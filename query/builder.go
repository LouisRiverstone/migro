// Package query is a Knex-style fluent query builder for tables without a model.
// Builders are mutable: every method modifies and returns the same *Builder.
package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
)

type Row = map[string]any

type Builder struct {
	db    *migro.DB
	table string
	g     *gorm.DB
}

// Table starts a query on a table.
func Table(db *migro.DB, table string) *Builder {
	return &Builder{db: db, table: table, g: db.Gorm.Table(table)}
}

func (b *Builder) Select(cols ...string) *Builder {
	if len(cols) > 0 {
		b.g = b.g.Select(cols)
	}
	return b
}

// Where accepts a SQL fragment with ? placeholders ("age > ?", 18) or a map[string]any.
// Values are always bound, never interpolated.
func (b *Builder) Where(query any, args ...any) *Builder { b.g = b.g.Where(query, args...); return b }
func (b *Builder) OrWhere(query any, args ...any) *Builder {
	b.g = b.g.Or(query, args...)
	return b
}
func (b *Builder) Not(query any, args ...any) *Builder { b.g = b.g.Not(query, args...); return b }

// WhereIn binds values; column is quoted by the dialect.
func (b *Builder) WhereIn(col string, values any) *Builder {
	return b.Where(b.db.Dialect.Quote(col)+" IN ?", values)
}

func (b *Builder) WhereNull(col string) *Builder {
	return b.Where(b.db.Dialect.Quote(col) + " IS NULL")
}

func (b *Builder) WhereNotNull(col string) *Builder {
	return b.Where(b.db.Dialect.Quote(col) + " IS NOT NULL")
}

func (b *Builder) WhereBetween(col string, lo, hi any) *Builder {
	return b.Where(b.db.Dialect.Quote(col)+" BETWEEN ? AND ?", lo, hi)
}

// Join takes a raw clause such as "JOIN posts ON posts.user_id = users.id".
func (b *Builder) Join(clause string, args ...any) *Builder {
	b.g = b.g.Joins(clause, args...)
	return b
}

// OrderBy sorts by col; dir is "asc" or "desc" (anything else is rejected at execution).
func (b *Builder) OrderBy(col, dir string) *Builder {
	d := strings.ToLower(dir)
	if d != "asc" && d != "desc" {
		_ = b.g.AddError(migro.Newf(migro.QueryFailed, "invalid order direction %q", dir))
		return b
	}
	b.g = b.g.Order(b.db.Dialect.Quote(col) + " " + strings.ToUpper(d))
	return b
}

func (b *Builder) GroupBy(cols ...string) *Builder {
	for _, c := range cols {
		b.g = b.g.Group(b.db.Dialect.Quote(c))
	}
	return b
}

func (b *Builder) Having(query any, args ...any) *Builder { b.g = b.g.Having(query, args...); return b }
func (b *Builder) Limit(n int) *Builder                   { b.g = b.g.Limit(n); return b }
func (b *Builder) Offset(n int) *Builder                  { b.g = b.g.Offset(n); return b }

// q returns a fresh statement copy so terminal calls never leak state into the builder.
func (b *Builder) q(ctx context.Context) *gorm.DB {
	return b.g.Session(&gorm.Session{NewDB: false}).WithContext(ctx)
}

func qf(err error, op string) error {
	if err == nil {
		return nil
	}
	return migro.Wrap(migro.QueryFailed, err, "%s", op)
}

// Get returns all matching rows.
func (b *Builder) Get(ctx context.Context) ([]Row, error) {
	rows := []Row{}
	if err := b.q(ctx).Find(&rows).Error; err != nil {
		return nil, qf(err, "get "+b.table)
	}
	return rows, nil
}

// First returns the first matching row or migro.ErrNotFound.
func (b *Builder) First(ctx context.Context) (Row, error) {
	rows := []Row{}
	if err := b.q(ctx).Limit(1).Find(&rows).Error; err != nil {
		return nil, qf(err, "first "+b.table)
	}
	if len(rows) == 0 {
		return nil, migro.Wrap(migro.NotFound, nil, "no row in %s", b.table)
	}
	return rows[0], nil
}

func (b *Builder) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := b.q(ctx).Count(&n).Error; err != nil {
		return 0, qf(err, "count "+b.table)
	}
	return n, nil
}

// Pluck returns a single column as []any.
func (b *Builder) Pluck(ctx context.Context, col string) ([]any, error) {
	var out []any
	if err := b.q(ctx).Pluck(col, &out).Error; err != nil {
		return nil, qf(err, "pluck "+b.table)
	}
	return out, nil
}

// Insert inserts one or more rows.
func (b *Builder) Insert(ctx context.Context, rows ...Row) error {
	if len(rows) == 0 {
		return nil
	}
	var v any = rows
	if len(rows) == 1 {
		v = rows[0]
	}
	return qf(b.db.Gorm.WithContext(ctx).Table(b.table).Create(v).Error, "insert "+b.table)
}

// Update sets columns on matching rows and returns the affected count.
// Without a Where, GORM refuses and an error is returned.
func (b *Builder) Update(ctx context.Context, values Row) (int64, error) {
	res := b.q(ctx).Updates(values)
	return res.RowsAffected, qf(res.Error, "update "+b.table)
}

// Delete removes matching rows. Without a Where, GORM refuses and an error is returned.
func (b *Builder) Delete(ctx context.Context) (int64, error) {
	res := b.q(ctx).Delete(&Row{})
	return res.RowsAffected, qf(res.Error, "delete "+b.table)
}

// Page is one page of results.
type Page struct {
	Data        []Row
	Total       int64
	PerPage     int
	CurrentPage int
	LastPage    int
}

// Paginate runs a count and a page query. page starts at 1.
func (b *Builder) Paginate(ctx context.Context, page, perPage int) (Page, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		return Page{}, migro.Newf(migro.QueryFailed, "perPage must be >= 1, got %d", perPage)
	}
	var n int64
	if err := b.q(ctx).Count(&n).Error; err != nil {
		return Page{}, qf(err, "paginate count "+b.table)
	}
	rows := []Row{}
	if err := b.q(ctx).Limit(perPage).Offset((page - 1) * perPage).Find(&rows).Error; err != nil {
		return Page{}, qf(err, "paginate "+b.table)
	}
	last := int((n + int64(perPage) - 1) / int64(perPage))
	return Page{Data: rows, Total: n, PerPage: perPage, CurrentPage: page, LastPage: last}, nil
}

func (b *Builder) String() string { return fmt.Sprintf("query.Builder(%s)", b.table) }
