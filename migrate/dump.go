package migrate

import (
	"bufio"
	"context"
	"strings"
	"time"

	"github.com/LouisRiverstone/migro"
)

// Dump is a squashed schema: the SQL every migration would run on an empty database, plus the names
// of those migrations. Loading it into a fresh database replaces running them one by one, so old
// migration files can be retired without losing the ability to build a new database.
//
// A dump is built by running each migration in pretend mode, so it needs no external tool (no
// pg_dump or mysqldump) and is portable only within the dialect it was generated for.
type Dump struct {
	Dialect    string
	Migrations []string
	Statements []string
}

const dumpHeader = "-- migro schema dump"

// Dump squashes the migrator's migrations. Nothing is written to the database. Run it against an
// empty database: a migration that inspects the existing schema (HasTable, ...) would otherwise record
// a different history.
func (m *Migrator) Dump(ctx context.Context) (Dump, error) {
	d := Dump{Dialect: m.db.Dialect.Name()}
	for _, mig := range m.active() {
		var res Result
		if err := m.pretend(ctx, mig.Name, mig.Up, &res); err != nil {
			return Dump{}, migro.Wrap(migro.MigrationFailed, err, "dump %s", mig.Name)
		}
		d.Migrations = append(d.Migrations, mig.Name)
		d.Statements = append(d.Statements, res.SQL[mig.Name]...)
	}
	return d, nil
}

// String renders the dump as a SQL file ParseDump reads back.
func (d Dump) String() string {
	var b strings.Builder
	b.WriteString(dumpHeader + "\n-- dialect: " + d.Dialect + "\n")
	for _, n := range d.Migrations {
		b.WriteString("-- migration: " + n + "\n")
	}
	for _, s := range d.Statements {
		b.WriteString("\n" + strings.TrimRight(strings.TrimSpace(s), ";") + ";\n")
	}
	return b.String()
}

// ParseDump reads what Dump.String wrote.
func ParseDump(src string) (Dump, error) {
	var d Dump
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false
			if line != dumpHeader {
				return Dump{}, migro.Newf(migro.InvalidSchema, "not a migro schema dump: the first line must be %q", dumpHeader)
			}
			continue
		}
		if rest, ok := strings.CutPrefix(line, "-- dialect:"); ok {
			d.Dialect = strings.TrimSpace(rest)
		} else if rest, ok := strings.CutPrefix(line, "-- migration:"); ok {
			d.Migrations = append(d.Migrations, strings.TrimSpace(rest))
		} else if line != "" && !strings.HasPrefix(line, "--") {
			break // the SQL starts here; SplitStatements handles the rest
		}
	}
	if first {
		return Dump{}, migro.Newf(migro.InvalidSchema, "the schema dump is empty")
	}
	if d.Dialect == "" {
		return Dump{}, migro.Newf(migro.InvalidSchema, "the schema dump names no dialect")
	}
	d.Statements = SplitStatements(src)
	return d, nil
}

// WithDump makes Run load the dump into a database that has no applied migrations, then run only the
// migrations the dump does not cover.
func WithDump(d Dump) Option { return func(m *Migrator) { m.dump = &d } }

// LoadDump executes the dump and records its migrations as applied in one batch.
func (m *Migrator) LoadDump(ctx context.Context, d Dump) error {
	if d.Dialect != m.db.Dialect.Name() {
		return migro.Newf(migro.InvalidSchema, "the schema dump is for %s, this database is %s", d.Dialect, m.db.Dialect.Name())
	}
	if err := m.ensureTables(ctx); err != nil {
		return err
	}
	checksums := map[string]string{}
	for _, mig := range m.migrations {
		checksums[mig.Name] = mig.Checksum
	}
	load := func(db *migro.DB) error {
		for _, s := range d.Statements {
			if err := db.Gorm.WithContext(ctx).Exec(s).Error; err != nil {
				return err
			}
		}
		for _, n := range d.Migrations {
			rec := &record{Name: n, Batch: 1, Checksum: checksums[n], AppliedAt: time.Now()}
			if err := db.Gorm.WithContext(ctx).Table(m.table).Create(rec).Error; err != nil {
				return err
			}
		}
		return nil
	}
	var err error
	if m.db.Dialect.TransactionalDDL() {
		err = m.db.Transaction(ctx, load)
	} else {
		err = load(m.db)
	}
	if err != nil {
		return migro.Wrap(migro.MigrationFailed, err, "load schema dump")
	}
	return nil
}

// DB returns the database the migrator works on.
func (m *Migrator) DB() *migro.DB { return m.db }
