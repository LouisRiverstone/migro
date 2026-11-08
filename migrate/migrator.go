package migrate

import (
	"context"
	"sort"
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
}

// Option customises a Migrator.
type Option func(*Migrator)

// WithTable overrides the migrations table (default "migro_migrations").
func WithTable(name string) Option {
	return func(m *Migrator) { m.table, m.lockTable = name, name+"_lock" }
}

func New(db *migro.DB, opts ...Option) *Migrator {
	m := &Migrator{db: db, table: "migro_migrations", lockTable: "migro_migrations_lock"}
	for _, o := range opts {
		o(m)
	}
	return m
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

// Result lists the migrations a call applied or reverted.
type Result struct {
	Batch int
	Names []string
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

// Latest applies every pending migration as one batch.
func (m *Migrator) Latest(ctx context.Context) (Result, error) {
	return m.withLock(ctx, func() (Result, error) {
		done, err := m.applied(ctx)
		if err != nil {
			return Result{}, err
		}
		have := map[string]record{}
		batch := 0
		for _, r := range done {
			have[r.Name] = r
			batch = max(batch, r.Batch)
		}
		for _, mig := range m.migrations {
			r, ok := have[mig.Name]
			if ok && mig.Checksum != "" && r.Checksum != "" && r.Checksum != mig.Checksum {
				return Result{}, migro.Newf(migro.ChecksumMismatch, "applied migration %q was modified", mig.Name)
			}
		}
		res := Result{Batch: batch + 1}
		for _, mig := range m.migrations {
			if _, ok := have[mig.Name]; ok {
				continue
			}
			mig := mig
			err := m.run(ctx, mig, mig.Up, func(tx *migro.DB) error {
				return tx.Gorm.WithContext(ctx).Table(m.table).Create(&record{Name: mig.Name, Batch: res.Batch, Checksum: mig.Checksum, AppliedAt: time.Now()}).Error
			})
			if err != nil {
				return res, migro.Wrap(migro.MigrationFailed, err, "apply %s", mig.Name)
			}
			res.Names = append(res.Names, mig.Name)
		}
		if len(res.Names) == 0 {
			res.Batch = 0
		}
		return res, nil
	})
}

// Rollback reverts the most recent batch.
func (m *Migrator) Rollback(ctx context.Context) (Result, error) {
	return m.revert(ctx, false)
}

// Reset reverts every applied migration, newest first.
func (m *Migrator) Reset(ctx context.Context) (Result, error) {
	return m.revert(ctx, true)
}

func (m *Migrator) revert(ctx context.Context, all bool) (Result, error) {
	return m.withLock(ctx, func() (Result, error) {
		done, err := m.applied(ctx)
		if err != nil {
			return Result{}, err
		}
		if len(done) == 0 {
			return Result{}, nil
		}
		last := 0
		for _, r := range done {
			last = max(last, r.Batch)
		}
		byName := map[string]Migration{}
		for _, mig := range m.migrations {
			byName[mig.Name] = mig
		}
		res := Result{Batch: last}
		for i := len(done) - 1; i >= 0; i-- {
			r := done[i]
			if !all && r.Batch != last {
				continue
			}
			mig, ok := byName[r.Name]
			if !ok || mig.Down == nil {
				return res, migro.Newf(migro.MigrationFailed, "cannot roll back %q: migration or Down func not registered", r.Name)
			}
			err := m.run(ctx, mig, mig.Down, func(tx *migro.DB) error {
				return tx.Gorm.WithContext(ctx).Exec("DELETE FROM "+tx.Dialect.Quote(m.table)+" WHERE "+tx.Dialect.Quote("name")+" = ?", r.Name).Error
			})
			if err != nil {
				return res, migro.Wrap(migro.MigrationFailed, err, "revert %s", r.Name)
			}
			res.Names = append(res.Names, r.Name)
		}
		return res, nil
	})
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
	out := make([]Status, 0, len(m.migrations))
	for _, mig := range m.migrations {
		r, ok := have[mig.Name]
		out = append(out, Status{Name: mig.Name, Applied: ok, Batch: r.Batch, AppliedAt: r.AppliedAt})
	}
	return out, nil
}
