package orm

import (
	"context"
	"regexp"
	"strings"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/dialect"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var comparison = map[string]bool{"=": true, "<>": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}

// jsonSegment keeps a JSON path from carrying SQL: the path is inlined because databases do not
// accept it as a bind variable.
var jsonSegment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$|^[0-9]+$`)

func (q *Query[T]) fail(code migro.Code, format string, args ...any) *Query[T] {
	_ = q.g.AddError(migro.Newf(code, format, args...))
	return q
}

func (q *Query[T]) features() dialect.Features { return dialect.FeaturesOf(q.db.Dialect) }

// WhereRaw adds a raw condition. Pass values as args; never concatenate input into sql.
func (q *Query[T]) WhereRaw(sql string, args ...any) *Query[T] { return q.Where(sql, args...) }

// OrWhereRaw is WhereRaw joined with OR.
func (q *Query[T]) OrWhereRaw(sql string, args ...any) *Query[T] { return q.OrWhere(sql, args...) }

// WhereColumn compares two columns: WhereColumn("updated_at", ">", "created_at").
func (q *Query[T]) WhereColumn(left, op, right string) *Query[T] {
	if !comparison[op] {
		return q.fail(migro.QueryFailed, "invalid comparison operator %q", op)
	}
	d := q.db.Dialect
	return q.Where(d.Quote(left) + " " + op + " " + d.Quote(right))
}

// WhereLike matches col against a LIKE pattern; WhereNotLike negates it.
func (q *Query[T]) WhereLike(col, pattern string) *Query[T] {
	return q.Where(q.db.Dialect.Quote(col)+" LIKE ?", pattern)
}
func (q *Query[T]) WhereNotLike(col, pattern string) *Query[T] {
	return q.Where(q.db.Dialect.Quote(col)+" NOT LIKE ?", pattern)
}

func (q *Query[T]) wherePart(col string, part dialect.DatePart, op string, value any) *Query[T] {
	if !comparison[op] {
		return q.fail(migro.QueryFailed, "invalid comparison operator %q", op)
	}
	expr := q.features().DatePart(q.db.Dialect.Quote(col), part)
	return q.Where(expr+" "+op+" ?", value)
}

// WhereDate compares the date part of a date/time column (value is "2026-01-31" or a time.Time).
func (q *Query[T]) WhereDate(col, op string, value any) *Query[T] {
	return q.wherePart(col, dialect.PartDate, op, value)
}

// WhereYear, WhereMonth and WhereDay compare one component of a date/time column.
func (q *Query[T]) WhereYear(col, op string, value int) *Query[T] {
	return q.wherePart(col, dialect.PartYear, op, value)
}
func (q *Query[T]) WhereMonth(col, op string, value int) *Query[T] {
	return q.wherePart(col, dialect.PartMonth, op, value)
}
func (q *Query[T]) WhereDay(col, op string, value int) *Query[T] {
	return q.wherePart(col, dialect.PartDay, op, value)
}

// WhereJSON compares the text at a JSON path inside col: WhereJSON("meta", "address.city", "=", "Rio").
// Path segments are names or array indexes separated by dots.
func (q *Query[T]) WhereJSON(col, path, op string, value any) *Query[T] {
	if !comparison[op] {
		return q.fail(migro.QueryFailed, "invalid comparison operator %q", op)
	}
	segments := strings.Split(path, ".")
	for _, s := range segments {
		if !jsonSegment.MatchString(s) {
			return q.fail(migro.QueryFailed, "invalid JSON path segment %q in %q", s, path)
		}
	}
	expr := q.features().JSONExtract(q.db.Dialect.Quote(col), segments)
	if expr == "" {
		return q.fail(migro.UnsupportedFeature, "%s cannot query inside JSON columns", q.db.Dialect.Name())
	}
	return q.Where(expr+" "+op+" ?", value)
}

// WhereFullText matches a search text against columns that have a full-text index
// (see schema Table.FullText). MySQL uses MATCH AGAINST and PostgreSQL to_tsvector.
func (q *Query[T]) WhereFullText(search string, cols ...string) *Query[T] {
	return q.fullText(search, cols, false)
}

// WhereFullTextBoolean is WhereFullText with operator syntax (+word -word "a phrase").
func (q *Query[T]) WhereFullTextBoolean(search string, cols ...string) *Query[T] {
	return q.fullText(search, cols, true)
}

func (q *Query[T]) fullText(search string, cols []string, boolean bool) *Query[T] {
	if len(cols) == 0 {
		return q.fail(migro.QueryFailed, "full-text search needs at least one column")
	}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = q.db.Dialect.Quote(c)
	}
	expr := q.features().FullTextMatch(quoted, boolean)
	if expr == "" {
		return q.fail(migro.UnsupportedFeature, "%s has no full-text search", q.db.Dialect.Name())
	}
	return q.Where(expr, search)
}

// SubQuery is any Query[S]; it lets WhereExists take a query over another model.
type SubQuery interface{ subquery() *gorm.DB }

func (q *Query[T]) subquery() *gorm.DB { return q.g }

// WhereExists adds EXISTS (sub). Correlate the subquery with WhereColumn:
//
//	users.Query().WhereExists(posts.Query().WhereColumn("e_posts.user_id", "=", "e_users.id"))
func (q *Query[T]) WhereExists(sub SubQuery) *Query[T] { return q.Where("EXISTS (?)", sub.subquery()) }

// WhereNotExists adds NOT EXISTS (sub).
func (q *Query[T]) WhereNotExists(sub SubQuery) *Query[T] {
	return q.Where("NOT EXISTS (?)", sub.subquery())
}

// WhereRelation is WhereHas with a plain condition: WhereRelation("Posts", "published = ?", true).
func (q *Query[T]) WhereRelation(relation string, query any, args ...any) *Query[T] {
	return q.WhereHas(relation, func(g *gorm.DB) *gorm.DB { return g.Where(query, args...) })
}

// FirstWhere is Where(col = value) followed by First.
func (q *Query[T]) FirstWhere(ctx context.Context, col string, value any) (*T, error) {
	return q.Where(q.db.Dialect.Quote(col)+" = ?", value).First(ctx)
}

// Sole returns the only match: NOT_FOUND for none, MULTIPLE_RECORDS_FOUND for several.
func (q *Query[T]) Sole(ctx context.Context) (*T, error) {
	rows, err := q.clone().Limit(2).Get(ctx)
	if err != nil {
		return nil, err
	}
	switch len(rows) {
	case 0:
		return nil, migro.Wrap(migro.NotFound, gorm.ErrRecordNotFound, "sole")
	case 1:
		return &rows[0], nil
	}
	return nil, migro.Newf(migro.MultipleRecords, "sole: the query matched more than one row")
}

// ---- grouping, distinct, union ----------------------------------------------------------------

// GroupBy groups by columns; combine it with Select and Having for aggregates.
func (q *Query[T]) GroupBy(cols ...string) *Query[T] {
	for _, c := range cols {
		q.g = q.g.Group(c) // GORM quotes the name itself
	}
	return q
}

// Having filters groups: Having("COUNT(*) > ?", 3). Never concatenate input into the condition.
func (q *Query[T]) Having(query any, args ...any) *Query[T] {
	q.g = q.g.Having(query, args...)
	return q
}

// Distinct drops duplicate rows, optionally by the given columns.
func (q *Query[T]) Distinct(cols ...string) *Query[T] {
	args := make([]any, len(cols))
	for i, c := range cols {
		args[i] = c
	}
	q.g = q.g.Distinct(args...)
	return q
}

// Union combines the receiver with others, removing duplicates; UnionAll keeps them. The result is a
// query over the combined rows: add OrderBy, Limit or Paginate to it afterwards. Each side keeps its
// own conditions and global scopes, and Select lists must line up.
func (q *Query[T]) Union(others ...*Query[T]) *Query[T] { return q.union("UNION", others) }
func (q *Query[T]) UnionAll(others ...*Query[T]) *Query[T] {
	return q.union("UNION ALL", others)
}

func (q *Query[T]) union(op string, others []*Query[T]) *Query[T] {
	if len(others) == 0 {
		return q
	}
	parts := []string{"?"}
	args := []any{q.clone().g}
	for _, o := range others {
		parts = append(parts, "?")
		args = append(args, o.clone().g)
	}
	out := &Query[T]{db: q.db, skip: &skipSet{}}
	out.g = q.db.Gorm.Model(new(T)).Table("("+strings.Join(parts, " "+op+" ")+") AS migro_union", args...).Unscoped()
	return out
}

// ---- locks -----------------------------------------------------------------------------------

// LockForUpdate adds FOR UPDATE; SharedLock adds a shared row lock. Use them inside a transaction.
// SQLite ignores both (it locks the whole database); SQL Server and Oracle's shared lock are not
// supported and fail with UNSUPPORTED_FEATURE.
func (q *Query[T]) LockForUpdate() *Query[T] { return q.lock(dialect.LockUpdate) }
func (q *Query[T]) SharedLock() *Query[T]    { return q.lock(dialect.LockShare) }

func (q *Query[T]) lock(mode dialect.LockMode) *Query[T] {
	if q.db.Dialect.Name() == "sqlite" {
		return q
	}
	sql := q.features().LockSQL(mode)
	if sql == "" {
		return q.fail(migro.UnsupportedFeature, "%s does not support the %s row lock", q.db.Dialect.Name(), mode)
	}
	q.g = q.g.Clauses(lockClause(sql))
	return q
}

// lockClause renders a whole lock clause ("FOR UPDATE", "LOCK IN SHARE MODE"). GORM's clause.Locking
// cannot: it always writes the word FOR itself and takes only the strength after it.
type lockClause string

func (lockClause) Name() string             { return "FOR" }
func (l lockClause) Build(b clause.Builder) { _, _ = b.WriteString(string(l)) }
func (l lockClause) MergeClause(c *clause.Clause) {
	c.Expression = l
	c.Builder = func(_ clause.Clause, b clause.Builder) { _, _ = b.WriteString(string(l)) }
}
