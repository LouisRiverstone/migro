package orm

import (
	"context"
	"reflect"
	"slices"
	"strconv"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
)

func (q *Query[T]) clone() *Query[T] {
	c := *q
	c.g = q.g.Session(&gorm.Session{})
	c.orders = slices.Clone(q.orders)
	return &c
}

func (q *Query[T]) WhereNotIn(col string, values any) *Query[T] {
	return q.Where(q.db.Dialect.Quote(col)+" NOT IN ?", values)
}
func (q *Query[T]) WhereNotNull(col string) *Query[T] {
	return q.Where(q.db.Dialect.Quote(col) + " IS NOT NULL")
}
func (q *Query[T]) WhereBetween(col string, from, to any) *Query[T] {
	return q.Where(q.db.Dialect.Quote(col)+" BETWEEN ? AND ?", from, to)
}
func (q *Query[T]) OrderByDesc(col string) *Query[T] { return q.OrderBy(col, "desc") }

// Latest orders newest first by created_at (or the given column); Oldest is the opposite.
func (q *Query[T]) Latest(col ...string) *Query[T] {
	return q.OrderBy(firstOr(col, "created_at"), "desc")
}
func (q *Query[T]) Oldest(col ...string) *Query[T] {
	return q.OrderBy(firstOr(col, "created_at"), "asc")
}

func firstOr(s []string, def string) string {
	if len(s) > 0 && s[0] != "" {
		return s[0]
	}
	return def
}

// When applies fn only if cond is true (Eloquent's when()).
func (q *Query[T]) When(cond bool, fn func(*Query[T]) *Query[T]) *Query[T] {
	if cond {
		return fn(q)
	}
	return q
}

// Tap runs fn for its side effects and returns the query.
func (q *Query[T]) Tap(fn func(*Query[T])) *Query[T] { fn(q); return q }

// FirstOrFail is First: a missing row is already a NOT_FOUND error.
func (q *Query[T]) FirstOrFail(ctx context.Context) (*T, error) { return q.First(ctx) }

// Sum and Avg return 0 when no row matches.
func (q *Query[T]) Sum(ctx context.Context, col string) (float64, error) {
	return q.aggregate(ctx, "COALESCE(SUM("+q.db.Dialect.Quote(col)+"), 0)")
}
func (q *Query[T]) Avg(ctx context.Context, col string) (float64, error) {
	return q.aggregate(ctx, "COALESCE(AVG("+q.db.Dialect.Quote(col)+"), 0)")
}

// Min and Max keep the column's own type (nil when no row matches).
func (q *Query[T]) Min(ctx context.Context, col string) (any, error) {
	return q.scalar(ctx, "MIN("+q.db.Dialect.Quote(col)+")")
}
func (q *Query[T]) Max(ctx context.Context, col string) (any, error) {
	return q.scalar(ctx, "MAX("+q.db.Dialect.Quote(col)+")")
}

func (q *Query[T]) aggregate(ctx context.Context, expr string) (float64, error) {
	v, err := q.scalar(ctx, expr)
	if err != nil {
		return 0, err
	}
	return asFloat(v), nil
}

// asFloat reads what drivers return for numeric aggregates: float64, int64, or decimal text.
func asFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	case []byte:
		f, _ := strconv.ParseFloat(string(x), 64)
		return f
	case nil:
		return 0
	}
	return float64(asInt(v))
}

func (q *Query[T]) scalar(ctx context.Context, expr string) (any, error) {
	var row map[string]any
	if err := q.clone().g.WithContext(ctx).Select(expr + " AS v").Take(&row).Error; err != nil {
		return nil, wrap(err, "aggregate")
	}
	if b, ok := row["v"].([]byte); ok { // some drivers return decimals as bytes
		return string(b), nil
	}
	return row["v"], nil
}

// Pluck returns one column of every matching row (Eloquent's pluck).
func Pluck[T, V any](ctx context.Context, q *Query[T], col string) ([]V, error) {
	var out []V
	if err := q.clone().g.WithContext(ctx).Pluck(col, &out).Error; err != nil {
		return nil, wrap(err, "pluck")
	}
	return out, nil
}

// Value returns one column of the first matching row, or NOT_FOUND.
func Value[T, V any](ctx context.Context, q *Query[T], col string) (V, error) {
	var zero V
	vs, err := Pluck[T, V](ctx, q.clone().Limit(1), col)
	if err != nil {
		return zero, err
	}
	if len(vs) == 0 {
		return zero, wrap(gorm.ErrRecordNotFound, "value")
	}
	return vs[0], nil
}

// Chunk walks the result in primary-key order, size rows at a time, stopping at the first error.
// Rows are hydrated like Get (bound, With / WithCount applied).
func (q *Query[T]) Chunk(ctx context.Context, size int, fn func(rows []T) error) error {
	return q.ChunkByID(ctx, size, "", fn)
}

// ChunkByID is Chunk keyed on column instead of the primary key (Eloquent's chunkById). The column
// must be unique and ascending-comparable; an empty column means the primary key.
func (q *Query[T]) ChunkByID(ctx context.Context, size int, column string, fn func(rows []T) error) error {
	if size < 1 {
		return migro.Newf(migro.QueryFailed, "chunk size must be >= 1, got %d", size)
	}
	sch, err := parseModel(q.db, new(T))
	if err != nil {
		return err
	}
	pk := column
	if pk == "" {
		if pk, err = primaryKey(sch); err != nil {
			return err
		}
	}
	var last any
	for {
		page := q.clone().OrderBy(pk, "asc").Limit(size)
		if last != nil {
			page = page.Where(q.db.Dialect.Quote(pk)+" > ?", last)
		}
		rows, err := page.Get(ctx)
		if err != nil || len(rows) == 0 {
			return err
		}
		if err := fn(rows); err != nil {
			return err
		}
		if len(rows) < size {
			return nil
		}
		last, _ = fieldOf(ctx, sch, reflect.ValueOf(&rows[len(rows)-1]).Elem(), pk)
	}
}

// Increment adds n to a column of every matching row, atomically.
func (q *Query[T]) Increment(ctx context.Context, col string, n int) (int64, error) {
	res := q.s(ctx).UpdateColumn(col, gorm.Expr(q.db.Dialect.Quote(col)+" + ?", n))
	return res.RowsAffected, wrap(res.Error, "increment")
}

func (q *Query[T]) Decrement(ctx context.Context, col string, n int) (int64, error) {
	return q.Increment(ctx, col, -n)
}

// Truncate empties the table (soft-deleted rows included). The statement is DELETE-based on
// SQLite, which has no TRUNCATE.
func (m *Model[T]) Truncate(ctx context.Context) error {
	sch, err := parseModel(m.db, new(T))
	if err != nil {
		return err
	}
	sql := "TRUNCATE TABLE " + m.db.Dialect.Quote(sch.Table)
	if m.db.Dialect.Name() == "sqlite" {
		sql = "DELETE FROM " + m.db.Dialect.Quote(sch.Table)
	}
	return wrap(m.db.Gorm.WithContext(ctx).Exec(sql).Error, "truncate")
}

// ToSQL renders the SELECT this query would run, with its bindings inlined, without running it
// (Eloquent's toRawSql). Use it for debugging; never feed the result back to the database.
func (q *Query[T]) ToSQL(ctx context.Context) (string, error) {
	var rows []T
	tx := q.clone().g.WithContext(ctx).Session(&gorm.Session{DryRun: true}).Find(&rows)
	if tx.Error != nil {
		return "", wrap(tx.Error, "to sql")
	}
	return q.db.Gorm.Dialector.Explain(tx.Statement.SQL.String(), tx.Statement.Vars...), nil
}

// DB returns the database the query runs on.
func (q *Query[T]) DB() *migro.DB { return q.db }
