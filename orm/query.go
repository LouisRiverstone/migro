package orm

import (
	"context"
	"strings"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
)

// Query is a mutable, chainable query for model T.
type Query[T any] struct {
	db     *migro.DB
	g      *gorm.DB
	with   []withSpec
	counts []string
	skip   *skipSet // global scopes switched off for this query
}

func (q *Query[T]) s(ctx context.Context) *gorm.DB {
	return q.g.Session(&gorm.Session{NewDB: false}).WithContext(ctx)
}

func (q *Query[T]) Where(query any, args ...any) *Query[T] { q.g = q.g.Where(query, args...); return q }
func (q *Query[T]) OrWhere(query any, args ...any) *Query[T] {
	q.g = q.g.Or(query, args...)
	return q
}
func (q *Query[T]) Not(query any, args ...any) *Query[T] { q.g = q.g.Not(query, args...); return q }

func (q *Query[T]) WhereIn(col string, values any) *Query[T] {
	return q.Where(q.db.Dialect.Quote(col)+" IN ?", values)
}

func (q *Query[T]) WhereNull(col string) *Query[T] {
	return q.Where(q.db.Dialect.Quote(col) + " IS NULL")
}

func (q *Query[T]) Select(cols ...string) *Query[T] {
	if len(cols) > 0 {
		q.g = q.g.Select(cols)
	}
	return q
}

// OrderBy: dir is "asc" or "desc".
func (q *Query[T]) OrderBy(col, dir string) *Query[T] {
	d := strings.ToLower(dir)
	if d != "asc" && d != "desc" {
		q.g.AddError(migro.Newf(migro.QueryFailed, "invalid order direction %q", dir))
		return q
	}
	q.g = q.g.Order(q.db.Dialect.Quote(col) + " " + strings.ToUpper(d))
	return q
}

func (q *Query[T]) Limit(n int) *Query[T]  { q.g = q.g.Limit(n); return q }
func (q *Query[T]) Offset(n int) *Query[T] { q.g = q.g.Offset(n); return q }

// Preload eager-loads a GORM relation by field name (avoids N+1).
func (q *Query[T]) Preload(relation string, args ...any) *Query[T] {
	q.g = q.g.Preload(relation, args...)
	return q
}

func (q *Query[T]) Joins(clause string, args ...any) *Query[T] {
	q.g = q.g.Joins(clause, args...)
	return q
}

// Scope applies a reusable query fragment.
func (q *Query[T]) Scope(fns ...func(*gorm.DB) *gorm.DB) *Query[T] {
	q.g = q.g.Scopes(fns...)
	return q
}

// WithTrashed includes soft-deleted rows.
func (q *Query[T]) WithTrashed() *Query[T] { q.g = q.g.Unscoped(); return q }

// OnlyTrashed returns only soft-deleted rows.
func (q *Query[T]) OnlyTrashed() *Query[T] {
	q.g = q.g.Unscoped().Where(q.db.Dialect.Quote("deleted_at") + " IS NOT NULL")
	return q
}

func (q *Query[T]) Get(ctx context.Context) (Collection[T], error) {
	out := Collection[T]{}
	if err := q.s(ctx).Find(&out).Error; err != nil {
		return nil, wrap(err, "get")
	}
	if err := q.hydrate(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// First returns the first match or a NOT_FOUND error.
func (q *Query[T]) First(ctx context.Context) (*T, error) {
	var v T
	if err := q.s(ctx).Take(&v).Error; err != nil {
		return nil, wrap(err, "first")
	}
	rows := []T{v}
	if err := q.hydrate(ctx, rows); err != nil {
		return nil, err
	}
	return &rows[0], nil
}

func (q *Query[T]) Count(ctx context.Context) (int64, error) {
	var n int64
	if err := q.s(ctx).Count(&n).Error; err != nil {
		return 0, wrap(err, "count")
	}
	return n, nil
}

func (q *Query[T]) Exists(ctx context.Context) (bool, error) {
	n, err := q.Limit(1).Count(ctx)
	return n > 0, err
}

// Update sets columns on matching rows; GORM refuses without a Where.
func (q *Query[T]) Update(ctx context.Context, values map[string]any) (int64, error) {
	res := q.s(ctx).Updates(values)
	return res.RowsAffected, wrap(res.Error, "update")
}

// Delete removes matching rows (soft when T has gorm.DeletedAt); GORM refuses without a Where.
func (q *Query[T]) Delete(ctx context.Context) (int64, error) {
	res := q.s(ctx).Delete(new(T))
	return res.RowsAffected, wrap(res.Error, "delete")
}

// Page is one page of models.
type Page[T any] struct {
	Data        Collection[T]
	Total       int64
	PerPage     int
	CurrentPage int
	LastPage    int
}

// Paginate returns page (1-based) with totals.
func (q *Query[T]) Paginate(ctx context.Context, page, perPage int) (Page[T], error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		return Page[T]{}, migro.Newf(migro.QueryFailed, "perPage must be >= 1, got %d", perPage)
	}
	var n int64
	if err := q.s(ctx).Count(&n).Error; err != nil {
		return Page[T]{}, wrap(err, "paginate count")
	}
	rows := Collection[T]{}
	if err := q.s(ctx).Limit(perPage).Offset((page - 1) * perPage).Find(&rows).Error; err != nil {
		return Page[T]{}, wrap(err, "paginate")
	}
	if err := q.hydrate(ctx, rows); err != nil {
		return Page[T]{}, err
	}
	last := int((n + int64(perPage) - 1) / int64(perPage))
	return Page[T]{Data: rows, Total: n, PerPage: perPage, CurrentPage: page, LastPage: last}, nil
}
