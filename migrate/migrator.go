package migrate

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/schema"
)

// Migrator applies and rolls back migrations, recording them in a table.
type Migrator struct {
	db         *migro.DB
	table      string
	lockTable  string
	migrations []Migration
	observer   func(Event)
	conn       string
	defaultCon string
	filter     bool
	dump       *Dump
}

// Option customises a Migrator.
type Option func(*Migrator)

// WithTable overrides the migrations table (default "migro_migrations").
func WithTable(name string) Option {
	return func(m *Migrator) { m.table, m.lockTable = name, name+"_lock" }
}

// WithObserver receives an Event before and after every migration step.
func WithObserver(fn func(Event)) Option { return func(m *Migrator) { m.observer = fn } }

// WithConnection restricts the Migrator to migrations pinned to connection selected, plus
// unpinned ones when selected is the default connection.
func WithConnection(selected, defaultName string) Option {
	return func(m *Migrator) { m.conn, m.defaultCon, m.filter = selected, defaultName, true }
}

func New(db *migro.DB, opts ...Option) *Migrator {
	m := &Migrator{db: db, table: "migro_migrations", lockTable: "migro_migrations_lock"}
	for _, o := range opts {
		o(m)
	}
	return m
}

// EventKind identifies a progress event.
type EventKind string

const (
	Migrating EventKind = "migrating"
	Migrated  EventKind = "migrated"
	Reverting EventKind = "reverting"
	Reverted  EventKind = "reverted"
)

// Event reports progress; Duration is set on Migrated/Reverted.
type Event struct {
	Kind     EventKind
	Name     string
	Duration time.Duration
}

func (m *Migrator) emit(k EventKind, name string, d time.Duration) {
	if m.observer != nil {
		m.observer(Event{Kind: k, Name: name, Duration: d})
	}
}

// Add registers migrations; names must be unique.
func (m *Migrator) Add(ms ...Migration) error {
	all := append(append([]Migration{}, m.migrations...), ms...)
	if err := validate(all); err != nil {
		return err
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	m.migrations = all
	return nil
}

func (m *Migrator) active() []Migration {
	if !m.filter {
		return m.migrations
	}
	var out []Migration
	for _, mig := range m.migrations {
		if mig.Connection == m.conn || (mig.Connection == "" && m.conn == m.defaultCon) {
			out = append(out, mig)
		}
	}
	return out
}

// Result lists the migrations a call applied or reverted.
type Result struct {
	Batch int
	Names []string
	// SQL holds, per migration, the statements that would run. Only set by pretend runs.
	SQL map[string][]string
}

// Status describes one registered migration.
type Status struct {
	Name      string
	Applied   bool
	Batch     int
	AppliedAt time.Time
}

type record struct {
	ID        uint `gorm:"primaryKey"`
	Name      string
	Batch     int
	Checksum  string
	AppliedAt time.Time
}

type lockRow struct {
	ID       int `gorm:"primaryKey;autoIncrement:false"`
	LockedAt time.Time
}

// Install creates the migration bookkeeping tables. Every other method does this on demand;
// call Install to do it explicitly (migration:install).
func (m *Migrator) Install(ctx context.Context) error { return m.ensureTables(ctx) }

func (m *Migrator) ensureTables(ctx context.Context) error {
	s := schema.New(m.db)
	if err := s.CreateTableIfNotExists(ctx, m.table, func(t *schema.Table) {
		t.Increments("id")
		t.String("name", 255).NotNull().Unique()
		t.Integer("batch").NotNull()
		t.String("checksum", 64)
		t.Timestamp("applied_at")
	}); err != nil {
		return err
	}
	return s.CreateTableIfNotExists(ctx, m.lockTable, func(t *schema.Table) {
		t.Integer("id").Primary()
		t.Timestamp("locked_at")
	})
}

func (m *Migrator) applied(ctx context.Context) ([]record, error) {
	var rows []record
	if err := m.db.Gorm.WithContext(ctx).Table(m.table).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, migro.Wrap(migro.QueryFailed, err, "read %s", m.table)
	}
	return rows, nil
}

// appliedIfInstalled treats a missing migrations table as "nothing applied" without creating it.
func (m *Migrator) appliedIfInstalled(ctx context.Context) ([]record, error) {
	if !m.db.Gorm.WithContext(ctx).Migrator().HasTable(m.table) {
		return nil, nil
	}
	return m.applied(ctx)
}

// lock takes a cross-process lock via a primary-key insert; a duplicate means another runner holds it.
func (m *Migrator) lock(ctx context.Context) error {
	err := m.db.Gorm.WithContext(ctx).Table(m.lockTable).Create(&lockRow{ID: 1, LockedAt: time.Now()}).Error
	if err == nil {
		return nil
	}
	var n int64
	if cerr := m.db.Gorm.WithContext(ctx).Table(m.lockTable).Count(&n).Error; cerr == nil && n > 0 {
		return migro.Wrap(migro.MigrationLocked, err, "another migration run holds the lock (use Unlock if it is stale)")
	}
	return migro.Wrap(migro.QueryFailed, err, "acquire lock")
}

// Unlock force-releases a stale lock.
func (m *Migrator) Unlock(ctx context.Context) error {
	if err := m.ensureTables(ctx); err != nil {
		return err
	}
	return m.unlock(ctx)
}

func (m *Migrator) unlock(ctx context.Context) error {
	if err := m.db.Gorm.WithContext(ctx).Exec("DELETE FROM " + m.db.Dialect.Quote(m.lockTable)).Error; err != nil {
		return migro.Wrap(migro.QueryFailed, err, "release lock")
	}
	return nil
}

// withLock prepares tables, takes the lock and always releases it.
func (m *Migrator) withLock(ctx context.Context, fn func() (Result, error)) (res Result, err error) {
	if err = m.ensureTables(ctx); err != nil {
		return Result{}, err
	}
	if err = m.lock(ctx); err != nil {
		return Result{}, err
	}
	defer func() {
		if uerr := m.unlock(context.WithoutCancel(ctx)); uerr != nil && err == nil {
			err = uerr
		}
	}()
	return fn()
}

func (m *Migrator) run(ctx context.Context, mig Migration, fn Func, record func(tx *migro.DB) error) error {
	exec := func(db *migro.DB) error {
		if err := fn(ctx, schema.New(db)); err != nil {
			return err
		}
		return record(db)
	}
	if m.db.Dialect.TransactionalDDL() && !mig.DisableTransaction {
		return m.db.Transaction(ctx, exec)
	}
	return exec(m.db)
}

// pretend runs fn against a recording DB, so nothing is written, and stores the SQL under name.
func (m *Migrator) pretend(ctx context.Context, name string, fn Func, res *Result) error {
	db, rec := pretendDB(ctx, m.db)
	if err := fn(ctx, schema.New(db)); err != nil {
		return err
	}
	if res.SQL == nil {
		res.SQL = map[string][]string{}
	}
	res.SQL[name] = rec.take()
	return nil
}

// RunOptions tune Run.
type RunOptions struct {
	// Step puts every migration in its own batch so each can be rolled back on its own.
	Step bool
	// Pretend returns the SQL each migration would run and changes nothing.
	Pretend bool
}

// Latest applies every pending migration as one batch.
func (m *Migrator) Latest(ctx context.Context) (Result, error) { return m.Run(ctx, RunOptions{}) }

// Run applies every pending migration.
func (m *Migrator) Run(ctx context.Context, opts RunOptions) (Result, error) {
	if opts.Pretend {
		return m.runPending(ctx, opts)
	}
	return m.withLock(ctx, func() (Result, error) { return m.runPending(ctx, opts) })
}

func (m *Migrator) runPending(ctx context.Context, opts RunOptions) (Result, error) {
	var (
		done []record
		err  error
	)
	if opts.Pretend {
		done, err = m.appliedIfInstalled(ctx)
	} else {
		done, err = m.applied(ctx)
	}
	if err != nil {
		return Result{}, err
	}
	res := Result{}
	if m.dump != nil && len(done) == 0 {
		if done, err = m.applyDump(ctx, opts, &res); err != nil {
			return res, err
		}
	}
	have := map[string]record{}
	batch := 0
	for _, r := range done {
		have[r.Name] = r
		batch = max(batch, r.Batch)
	}
	list := m.active()
	for _, mig := range list {
		r, ok := have[mig.Name]
		if ok && mig.Checksum != "" && r.Checksum != "" && r.Checksum != mig.Checksum {
			return Result{}, migro.Newf(migro.ChecksumMismatch, "applied migration %q was modified", mig.Name)
		}
	}
	res.Batch = batch + 1
	for _, mig := range list {
		if _, ok := have[mig.Name]; ok {
			continue
		}
		if opts.Step && len(res.Names) > 0 {
			res.Batch++
		}
		mig, thisBatch := mig, res.Batch
		m.emit(Migrating, mig.Name, 0)
		start := time.Now()
		if opts.Pretend {
			err = m.pretend(ctx, mig.Name, mig.Up, &res)
		} else {
			err = m.run(ctx, mig, mig.Up, func(tx *migro.DB) error {
				return tx.Gorm.WithContext(ctx).Table(m.table).Create(&record{Name: mig.Name, Batch: thisBatch, Checksum: mig.Checksum, AppliedAt: time.Now()}).Error
			})
		}
		if err != nil {
			return res, migro.Wrap(migro.MigrationFailed, err, "apply %s", mig.Name)
		}
		m.emit(Migrated, mig.Name, time.Since(start))
		res.Names = append(res.Names, mig.Name)
	}
	if len(res.Names) == 0 {
		res.Batch = 0
	}
	return res, nil
}

// applyDump loads the dump into a database with no migration history and returns the records that
// stand for it. Under Pretend it only reports the SQL.
func (m *Migrator) applyDump(ctx context.Context, opts RunOptions, res *Result) ([]record, error) {
	const name = "schema dump"
	m.emit(Migrating, name, 0)
	start := time.Now()
	if opts.Pretend {
		if m.dump.Dialect != m.db.Dialect.Name() {
			return nil, migro.Newf(migro.InvalidSchema, "the schema dump is for %s, this database is %s", m.dump.Dialect, m.db.Dialect.Name())
		}
		res.SQL = map[string][]string{name: m.dump.Statements}
	} else if err := m.LoadDump(ctx, *m.dump); err != nil {
		return nil, err
	}
	m.emit(Migrated, name, time.Since(start))
	res.Names = append(res.Names, name)
	recs := make([]record, len(m.dump.Migrations))
	for i, n := range m.dump.Migrations {
		recs[i] = record{Name: n, Batch: 1}
	}
	return recs, nil
}

// RevertOptions select what to roll back. Set at most one of Steps, ToBatch, All;
// with none set the most recent batch is reverted.
type RevertOptions struct {
	// Steps reverts the N most recently applied migrations (Laravel's --step).
	Steps int
	// ToBatch reverts every migration applied after the given batch; 0 reverts everything (Adonis' --batch).
	ToBatch *int
	All     bool
	Pretend bool
}

// Rollback reverts the most recent batch.
func (m *Migrator) Rollback(ctx context.Context) (Result, error) {
	return m.Revert(ctx, RevertOptions{})
}

// RollbackSteps reverts the n most recently applied migrations.
func (m *Migrator) RollbackSteps(ctx context.Context, n int) (Result, error) {
	return m.Revert(ctx, RevertOptions{Steps: n})
}

// RollbackToBatch reverts every migration applied after batch.
func (m *Migrator) RollbackToBatch(ctx context.Context, batch int) (Result, error) {
	return m.Revert(ctx, RevertOptions{ToBatch: &batch})
}

// Reset reverts every applied migration, newest first.
func (m *Migrator) Reset(ctx context.Context) (Result, error) {
	return m.Revert(ctx, RevertOptions{All: true})
}

// Revert rolls migrations back according to opts.
func (m *Migrator) Revert(ctx context.Context, opts RevertOptions) (Result, error) {
	selectors := 0
	if opts.Steps != 0 {
		selectors++
	}
	if opts.ToBatch != nil {
		selectors++
	}
	if opts.All {
		selectors++
	}
	if selectors > 1 || opts.Steps < 0 || (opts.ToBatch != nil && *opts.ToBatch < 0) {
		return Result{}, migro.Newf(migro.InvalidConfig, "use at most one of Steps (>0), ToBatch (>=0) and All")
	}
	if opts.Pretend {
		return m.revertSelected(ctx, opts)
	}
	return m.withLock(ctx, func() (Result, error) { return m.revertSelected(ctx, opts) })
}

func selectToRevert(done []record, opts RevertOptions) []record {
	last := 0
	for _, r := range done {
		last = max(last, r.Batch)
	}
	var picked []record
	for i := len(done) - 1; i >= 0; i-- { // newest first
		r := done[i]
		switch {
		case opts.All:
		case opts.Steps > 0:
			if len(picked) >= opts.Steps {
				return picked
			}
		case opts.ToBatch != nil:
			if r.Batch <= *opts.ToBatch {
				continue
			}
		default:
			if r.Batch != last {
				continue
			}
		}
		picked = append(picked, r)
	}
	return picked
}

func (m *Migrator) revertSelected(ctx context.Context, opts RevertOptions) (Result, error) {
	var (
		done []record
		err  error
	)
	if opts.Pretend {
		done, err = m.appliedIfInstalled(ctx)
	} else {
		done, err = m.applied(ctx)
	}
	if err != nil {
		return Result{}, err
	}
	picked := selectToRevert(done, opts)
	if len(picked) == 0 {
		return Result{}, nil
	}
	byName := map[string]Migration{}
	for _, mig := range m.active() {
		byName[mig.Name] = mig
	}
	res := Result{Batch: picked[0].Batch}
	for _, r := range picked {
		mig, ok := byName[r.Name]
		if !ok || mig.Down == nil {
			return res, migro.Newf(migro.MigrationFailed, "cannot roll back %q: migration or Down func not registered", r.Name)
		}
		m.emit(Reverting, r.Name, 0)
		start := time.Now()
		if opts.Pretend {
			err = m.pretend(ctx, r.Name, mig.Down, &res)
		} else {
			err = m.run(ctx, mig, mig.Down, func(tx *migro.DB) error {
				return tx.Gorm.WithContext(ctx).Exec("DELETE FROM "+tx.Dialect.Quote(m.table)+" WHERE "+tx.Dialect.Quote("name")+" = ?", r.Name).Error
			})
		}
		if err != nil {
			return res, migro.Wrap(migro.MigrationFailed, err, "revert %s", r.Name)
		}
		m.emit(Reverted, r.Name, time.Since(start))
		res.Names = append(res.Names, r.Name)
	}
	return res, nil
}

// Refresh reverts everything, then applies everything again.
func (m *Migrator) Refresh(ctx context.Context) (Result, error) {
	if _, err := m.Reset(ctx); err != nil {
		return Result{}, err
	}
	return m.Latest(ctx)
}

// Fresh drops every table in the database (not just migrated ones), then applies all migrations.
// It is destructive: callers decide whether it is allowed.
func (m *Migrator) Fresh(ctx context.Context) (Result, error) {
	if err := DropAllTables(ctx, m.db); err != nil {
		return Result{}, err
	}
	return m.Latest(ctx)
}

// DropAllTables removes every table, retrying so foreign-key order does not matter.
func DropAllTables(ctx context.Context, db *migro.DB) error {
	mg := db.Gorm.WithContext(ctx).Migrator()
	for range 10 {
		tables, err := mg.GetTables()
		if err != nil {
			return migro.Wrap(migro.QueryFailed, err, "list tables")
		}
		tables = slices.DeleteFunc(tables, func(t string) bool { return strings.HasPrefix(t, "sqlite_") })
		if len(tables) == 0 {
			return nil
		}
		for _, t := range tables {
			if !strings.HasPrefix(t, "sqlite_") { // SQLite's internal tables cannot be dropped
				_ = mg.DropTable(t)
			}
		}
	}
	left, _ := mg.GetTables()
	left = slices.DeleteFunc(left, func(t string) bool { return strings.HasPrefix(t, "sqlite_") })
	if len(left) > 0 {
		return migro.Newf(migro.QueryFailed, "could not drop tables: %v", left)
	}
	return nil
}

// Status reports every registered migration and whether it is applied.
func (m *Migrator) Status(ctx context.Context) ([]Status, error) {
	if err := m.ensureTables(ctx); err != nil {
		return nil, err
	}
	done, err := m.applied(ctx)
	if err != nil {
		return nil, err
	}
	have := map[string]record{}
	for _, r := range done {
		have[r.Name] = r
	}
	list := m.active()
	out := make([]Status, 0, len(list))
	for _, mig := range list {
		r, ok := have[mig.Name]
		out = append(out, Status{Name: mig.Name, Applied: ok, Batch: r.Batch, AppliedAt: r.AppliedAt})
	}
	return out, nil
}
