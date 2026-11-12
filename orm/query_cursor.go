package orm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"math"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm/clause"
)

// SimplePage is a page without a total: one extra row tells whether another page follows, so it
// skips the COUNT query Paginate pays for.
type SimplePage[T any] struct {
	Data        Collection[T]
	PerPage     int
	CurrentPage int
	HasMore     bool
}

// SimplePaginate returns page (1-based) and whether a next page exists.
func (q *Query[T]) SimplePaginate(ctx context.Context, page, perPage int) (SimplePage[T], error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		return SimplePage[T]{}, migro.Newf(migro.QueryFailed, "perPage must be >= 1, got %d", perPage)
	}
	rows, err := q.clone().Limit(perPage + 1).Offset((page - 1) * perPage).Get(ctx)
	if err != nil {
		return SimplePage[T]{}, err
	}
	more := len(rows) > perPage
	if more {
		rows = rows[:perPage]
	}
	return SimplePage[T]{Data: rows, PerPage: perPage, CurrentPage: page, HasMore: more}, nil
}

// CursorPage is one keyset page. Pass NextCursor or PrevCursor back to CursorPaginate; an empty
// cursor means there is no page in that direction.
type CursorPage[T any] struct {
	Data       Collection[T]
	PerPage    int
	NextCursor string
	PrevCursor string
}

// CursorPaginate pages by the query's OrderBy columns instead of an offset, so deep pages stay fast and
// rows inserted meanwhile do not shift the page. The query needs at least one OrderBy; the primary key
// is appended as a tie-breaker. Order columns must not be NULL.
func (q *Query[T]) CursorPaginate(ctx context.Context, perPage int, cursor string) (CursorPage[T], error) {
	if perPage < 1 {
		return CursorPage[T]{}, migro.Newf(migro.QueryFailed, "perPage must be >= 1, got %d", perPage)
	}
	if len(q.orders) == 0 {
		return CursorPage[T]{}, migro.Newf(migro.QueryFailed, "cursor pagination needs an OrderBy")
	}
	sch, err := parseModel(q.db, new(T))
	if err != nil {
		return CursorPage[T]{}, err
	}
	pk, err := primaryKey(sch)
	if err != nil {
		return CursorPage[T]{}, err
	}
	orders := slices.Clone(q.orders)
	if !slices.ContainsFunc(orders, func(o orderSpec) bool { return o.col == pk }) {
		orders = append(orders, orderSpec{col: pk})
	}

	cur, err := decodeCursor(cursor, len(orders))
	if err != nil {
		return CursorPage[T]{}, err
	}
	prev := cur != nil && cur.Dir == "prev"
	d := q.db.Dialect

	query := q.clone()
	cols := make([]clause.OrderByColumn, len(orders))
	for i, o := range orders {
		cols[i] = clause.OrderByColumn{Column: clause.Column{Name: d.Quote(o.col), Raw: true}, Desc: o.desc != prev, Reorder: i == 0}
	}
	query.g = query.g.Clauses(clause.OrderBy{Columns: cols})
	if cur != nil {
		cond, args := keyset(d.Quote, orders, cur.Values, prev)
		query = query.Where(cond, args...)
	}
	rows, err := query.Limit(perPage + 1).Get(ctx)
	if err != nil {
		return CursorPage[T]{}, err
	}
	more := len(rows) > perPage
	if more {
		rows = rows[:perPage]
	}
	if prev {
		slices.Reverse(rows)
	}

	page := CursorPage[T]{Data: rows, PerPage: perPage}
	if len(rows) == 0 {
		return page, nil
	}
	token := func(row *T, dir string) (string, error) {
		vals := make([]cursorValue, len(orders))
		for i, o := range orders {
			v, err := fieldOf(ctx, sch, reflect.ValueOf(row).Elem(), o.col)
			if err != nil {
				return "", err
			}
			if vals[i], err = encodeValue(v); err != nil {
				return "", err
			}
		}
		return encodeCursor(cursorToken{Dir: dir, Values: vals})
	}
	first, last := &rows[0], &rows[len(rows)-1]
	hasBefore, hasAfter := cur != nil, more // a fresh first page has nothing before it
	if prev {
		hasBefore, hasAfter = more, true
	}
	if hasAfter {
		if page.NextCursor, err = token(last, "next"); err != nil {
			return CursorPage[T]{}, err
		}
	}
	if hasBefore {
		if page.PrevCursor, err = token(first, "prev"); err != nil {
			return CursorPage[T]{}, err
		}
	}
	return page, nil
}

// keyset builds "rows strictly after (or before) the cursor row" for a mixed-direction ordering:
// (c0 > v0) OR (c0 = v0 AND c1 > v1) OR ...
func keyset(quote func(string) string, orders []orderSpec, vals []any, prev bool) (string, []any) {
	var sql string
	var args []any
	for i, o := range orders {
		part := ""
		for j := 0; j < i; j++ {
			part += quote(orders[j].col) + " = ? AND "
			args = append(args, vals[j])
		}
		op := ">"
		if o.desc != prev {
			op = "<"
		}
		part += quote(o.col) + " " + op + " ?"
		args = append(args, vals[i])
		if sql != "" {
			sql += " OR "
		}
		sql += "(" + part + ")"
	}
	return "(" + sql + ")", args
}

type cursorValue struct {
	T string `json:"t"`
	V string `json:"v"`
}

type cursorToken struct {
	Dir    string        `json:"d"`
	Values []cursorValue `json:"v"`
}

type decodedCursor struct {
	Dir    string
	Values []any
}

func encodeValue(v any) (cursorValue, error) {
	switch x := v.(type) {
	case time.Time:
		return cursorValue{"t", x.UTC().Format(time.RFC3339Nano)}, nil
	case string:
		return cursorValue{"s", x}, nil
	case []byte:
		return cursorValue{"s", string(x)}, nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		return encodeValue(rv.Elem().Interface())
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return cursorValue{"i", strconv.FormatInt(rv.Int(), 10)}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return cursorValue{"u", strconv.FormatUint(rv.Uint(), 10)}, nil
	case reflect.Float32, reflect.Float64:
		return cursorValue{"f", strconv.FormatFloat(rv.Float(), 'g', -1, 64)}, nil
	case reflect.String:
		return cursorValue{"s", rv.String()}, nil
	}
	return cursorValue{}, migro.Newf(migro.QueryFailed, "cursor pagination cannot order by a %T column", v)
}

func decodeValue(c cursorValue) (any, error) {
	switch c.T {
	case "t":
		return time.Parse(time.RFC3339Nano, c.V)
	case "s":
		return c.V, nil
	case "i":
		return strconv.ParseInt(c.V, 10, 64)
	case "u":
		u, err := strconv.ParseUint(c.V, 10, 64)
		if err == nil && u <= math.MaxInt64 {
			return int64(u), nil // drivers take int64 for every integer column
		}
		return u, err
	case "f":
		return strconv.ParseFloat(c.V, 64)
	}
	return nil, fmt.Errorf("unknown value type %q", c.T)
}

func encodeCursor(t cursorToken) (string, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return "", migro.Wrap(migro.QueryFailed, err, "encode cursor")
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func decodeCursor(s string, columns int) (*decodedCursor, error) {
	if s == "" {
		return nil, nil
	}
	bad := func(err error) error { return migro.Wrap(migro.QueryFailed, err, "invalid cursor") }
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, bad(err)
	}
	var t cursorToken
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, bad(err)
	}
	if (t.Dir != "next" && t.Dir != "prev") || len(t.Values) != columns {
		return nil, bad(errors.New("it does not match this query's ordering"))
	}
	out := &decodedCursor{Dir: t.Dir}
	for _, c := range t.Values {
		v, err := decodeValue(c)
		if err != nil {
			return nil, bad(err)
		}
		out.Values = append(out.Values, v)
	}
	return out, nil
}

// ---- streaming ------------------------------------------------------------------------------

// Cursor streams the rows one at a time through a single open statement (Eloquent's cursor): memory
// stays flat for huge results. Relations are not eager loaded; use Lazy when you need With. Breaking
// out of the loop closes the statement.
func (q *Query[T]) Cursor(ctx context.Context) iter.Seq2[*T, error] {
	return func(yield func(*T, error) bool) {
		rows, err := q.s(ctx).Rows()
		if err != nil {
			yield(nil, wrap(err, "cursor"))
			return
		}
		defer rows.Close()
		for rows.Next() {
			v := new(T)
			if err := q.db.Gorm.ScanRows(rows, v); err != nil {
				yield(nil, wrap(err, "cursor scan"))
				return
			}
			bind(q.db, v, true)
			if !yield(v, nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(nil, wrap(err, "cursor"))
		}
	}
}

var errStop = errors.New("stop")

// Lazy streams the rows chunk by chunk (Eloquent's lazyById): each chunk is hydrated like Get, so With
// and WithCount work, and memory holds one chunk at a time.
func (q *Query[T]) Lazy(ctx context.Context, size int) iter.Seq2[*T, error] {
	return func(yield func(*T, error) bool) {
		err := q.Chunk(ctx, size, func(rows []T) error {
			for i := range rows {
				if !yield(&rows[i], nil) {
					return errStop
				}
			}
			return nil
		})
		if err != nil && !errors.Is(err, errStop) {
			yield(nil, err)
		}
	}
}

// Each runs fn for every row, size rows at a time, stopping at the first error.
func (q *Query[T]) Each(ctx context.Context, size int, fn func(*T) error) error {
	return q.Chunk(ctx, size, func(rows []T) error {
		for i := range rows {
			if err := fn(&rows[i]); err != nil {
				return err
			}
		}
		return nil
	})
}
