package schema

import (
	"context"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/dialect"
)

// Builder executes DDL against a migro.DB (or a transaction-bound one).
type Builder struct{ db *migro.DB }

func New(db *migro.DB) *Builder { return &Builder{db: db} }

// DB returns the underlying handle, e.g. for raw statements inside a migration.
func (b *Builder) DB() *migro.DB { return b.db }

// CreateSQL compiles CREATE TABLE (+ index statements) without executing.
func CreateSQL(d dialect.Dialect, name string, fn func(*Table)) ([]string, error) {
	t := &Table{name: name}
	fn(t)
	return t.compileCreate(d)
}

// AlterSQL compiles ALTER TABLE statements without executing.
func AlterSQL(d dialect.Dialect, name string, fn func(*Table)) ([]string, error) {
	t := &Table{name: name, alter: true}
	fn(t)
	return t.compileAlter(d)
}

func (b *Builder) run(ctx context.Context, stmts []string) error {
	for _, s := range stmts {
		if err := b.db.Gorm.WithContext(ctx).Exec(s).Error; err != nil {
			return migro.Wrap(migro.QueryFailed, err, "exec %q", s)
		}
	}
	return nil
}

func (b *Builder) CreateTable(ctx context.Context, name string, fn func(*Table)) error {
	stmts, err := CreateSQL(b.db.Dialect, name, fn)
	if err != nil {
		return err
	}
	return b.run(ctx, stmts)
}

// CreateTableIfNotExists skips creation when the table exists.
func (b *Builder) CreateTableIfNotExists(ctx context.Context, name string, fn func(*Table)) error {
	has, err := b.HasTable(ctx, name)
	if err != nil || has {
		return err
	}
	return b.CreateTable(ctx, name, fn)
}

func (b *Builder) AlterTable(ctx context.Context, name string, fn func(*Table)) error {
	stmts, err := AlterSQL(b.db.Dialect, name, fn)
	if err != nil {
		return err
	}
	return b.run(ctx, stmts)
}

func (b *Builder) DropTable(ctx context.Context, name string) error {
	return b.run(ctx, []string{"DROP TABLE " + b.db.Dialect.Quote(name)})
}

// DropTableIfExists checks first because Oracle and SQL Server lack portable IF EXISTS.
func (b *Builder) DropTableIfExists(ctx context.Context, name string) error {
	has, err := b.HasTable(ctx, name)
	if err != nil || !has {
		return err
	}
	return b.DropTable(ctx, name)
}

func (b *Builder) RenameTable(ctx context.Context, from, to string) error {
	return b.run(ctx, []string{b.db.Dialect.RenameTable(from, to)})
}

func (b *Builder) HasTable(ctx context.Context, name string) (bool, error) {
	return b.db.Gorm.WithContext(ctx).Migrator().HasTable(name), nil
}

func (b *Builder) HasColumn(ctx context.Context, table, column string) (bool, error) {
	return b.db.Gorm.WithContext(ctx).Migrator().HasColumn(table, column), nil
}

// Raw runs an arbitrary statement.
func (b *Builder) Raw(ctx context.Context, sql string, args ...any) error {
	if err := b.db.Gorm.WithContext(ctx).Exec(sql, args...).Error; err != nil {
		return migro.Wrap(migro.QueryFailed, err, "raw exec")
	}
	return nil
}
