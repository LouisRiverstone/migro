package orm

import (
	"context"
	"reflect"
	"strings"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
)

// aggSpec is one relation aggregate: COUNT, SUM, AVG, MIN, MAX or EXISTS over rel (column col).
type aggSpec struct {
	rel, fn, col string
}

// extraKey is the Extras key the result lands under: posts_count, posts_sum_votes, posts_exists.
func (a aggSpec) extraKey() string {
	base := snake(a.rel)
	switch a.fn {
	case "COUNT":
		return base + "_count"
	case "EXISTS":
		return base + "_exists"
	}
	return base + "_" + strings.ToLower(a.fn) + "_" + a.col
}

// WithSum, WithAvg, WithMin and WithMax add an aggregate of a related column to each model's Extras
// as "<relation>_sum_<column>" (and so on), computed in one extra query. Sum is 0 when nothing is
// related; the others are nil. Supported on has-one/many, morph one/many and the many-to-many kinds.
func (q *Query[T]) WithSum(relation, column string) *Query[T] {
	return q.withAgg(relation, "SUM", column)
}
func (q *Query[T]) WithAvg(relation, column string) *Query[T] {
	return q.withAgg(relation, "AVG", column)
}
func (q *Query[T]) WithMin(relation, column string) *Query[T] {
	return q.withAgg(relation, "MIN", column)
}
func (q *Query[T]) WithMax(relation, column string) *Query[T] {
	return q.withAgg(relation, "MAX", column)
}

// WithExists adds "<relation>_exists" (a bool) to each model's Extras.
func (q *Query[T]) WithExists(relation string) *Query[T] { return q.withAgg(relation, "EXISTS", "") }

func (q *Query[T]) withAgg(rel, fn, col string) *Query[T] {
	q.aggs = append(q.aggs, aggSpec{rel: rel, fn: fn, col: col})
	return q
}

// LoadCount, LoadSum, LoadAvg, LoadMin, LoadMax and LoadExists compute the same aggregates for
// models already fetched (Eloquent's loadCount, loadSum, ...).
func LoadCount[T any](ctx context.Context, db *migro.DB, v *T, relations ...string) error {
	for _, r := range relations {
		if err := loadAgg(ctx, db, v, aggSpec{rel: r, fn: "COUNT"}); err != nil {
			return err
		}
	}
	return nil
}
func LoadSum[T any](ctx context.Context, db *migro.DB, v *T, relation, column string) error {
	return loadAgg(ctx, db, v, aggSpec{rel: relation, fn: "SUM", col: column})
}
func LoadAvg[T any](ctx context.Context, db *migro.DB, v *T, relation, column string) error {
	return loadAgg(ctx, db, v, aggSpec{rel: relation, fn: "AVG", col: column})
}
func LoadMin[T any](ctx context.Context, db *migro.DB, v *T, relation, column string) error {
	return loadAgg(ctx, db, v, aggSpec{rel: relation, fn: "MIN", col: column})
}
func LoadMax[T any](ctx context.Context, db *migro.DB, v *T, relation, column string) error {
	return loadAgg(ctx, db, v, aggSpec{rel: relation, fn: "MAX", col: column})
}
func LoadExists[T any](ctx context.Context, db *migro.DB, v *T, relation string) error {
	return loadAgg(ctx, db, v, aggSpec{rel: relation, fn: "EXISTS"})
}

func loadAgg[T any](ctx context.Context, db *migro.DB, v *T, a aggSpec) error {
	if v == nil {
		return nil
	}
	bindRelations(db, reflect.ValueOf(v))
	return aggregateOnto(ctx, db, []reflect.Value{reflect.ValueOf(v).Elem()}, a)
}

func aggregateOnto(ctx context.Context, db *migro.DB, owners []reflect.Value, a aggSpec) error {
	if len(owners) == 0 {
		return nil
	}
	d, _, err := resolveByName(db, owners[0].Type(), a.rel)
	if err != nil {
		return err
	}
	return d.aggregateInto(ctx, db, owners, a)
}

func (d *relDef) aggregateInto(ctx context.Context, db *migro.DB, owners []reflect.Value, a aggSpec) error {
	q := db.Dialect.Quote
	keys, err := keysOf(ctx, d, owners, d.lk)
	if err != nil {
		return err
	}
	if a.fn != "COUNT" && a.fn != "EXISTS" && !validColumn(a.col) {
		return migro.Newf(migro.QueryFailed, "invalid aggregate column %q", a.col)
	}
	fn, col := a.fn, "*"
	if fn == "EXISTS" {
		fn = "COUNT"
	} else if a.fn != "COUNT" {
		col = q(a.col)
	}

	var g *gorm.DB
	var ownerRaw string // GORM quotes Group names itself
	switch d.kind {
	case kHasOne, kHasMany, kMorphOne, kMorphMany:
		ownerRaw = d.fk
		g = relQuery(ctx, db, d.relT, nil)
		if d.typeCol != "" {
			g = g.Where(q(d.typeCol)+" = ?", d.typeVal)
		}
	case kBelongsToMany, kMorphToMany, kMorphedByMany:
		ownerRaw = d.pivot.table + "." + d.pivot.ownerCol
		if col == "*" { // counting needs no join: the pivot rows are the relations
			g = db.Gorm.WithContext(ctx).Table(d.pivot.table)
		} else {
			table := d.relSch.Table
			col = q(table) + "." + col
			g = relQuery(ctx, db, d.relT, nil).Joins("JOIN " + q(d.pivot.table) + " ON " + q(d.pivot.table) + "." + q(d.pivot.relatedCol) + " = " + q(table) + "." + q(d.rk))
		}
		if d.pivot.typeCol != "" {
			g = g.Where(q(d.pivot.table)+"."+q(d.pivot.typeCol)+" = ?", d.pivot.typeVal)
		}
		g = d.pivot.constrain(db, g)
	default:
		return migro.Newf(migro.UnsupportedFeature, "%s aggregates are not supported on %s", strings.ToLower(a.fn), d.name)
	}

	type bucket struct{ v any }
	found := map[string]bucket{}
	if len(keys) > 0 {
		var rows []map[string]any
		ownerCol := q(ownerRaw)
		err := g.Select(ownerCol+" AS k, "+fn+"("+col+") AS v").
			Where(ownerCol+" IN ?", keys).Group(ownerRaw).Find(&rows).Error
		if err != nil {
			return wrap(err, a.fn+" "+d.name)
		}
		for _, r := range rows {
			found[normKey(r["k"])] = bucket{v: r["v"]}
		}
	}
	for _, o := range owners {
		k, _ := fieldOf(ctx, d.ownerSch, o, d.lk)
		b, has := found[normKey(k)]
		var out any
		switch a.fn {
		case "COUNT":
			out = asInt(b.v)
		case "EXISTS":
			out = has && asInt(b.v) > 0
		case "SUM":
			out = asFloat(b.v)
		default:
			if has {
				out = scalarOf(b.v)
				if a.fn == "AVG" {
					out = asFloat(b.v)
				}
			}
		}
		setExtra(o.Addr().Interface(), a.extraKey(), out)
	}
	return nil
}

// scalarOf turns driver byte slices into strings so MIN/MAX of text or decimals read naturally.
func scalarOf(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

func validColumn(c string) bool { return c != "" && jsonSegment.MatchString(c) }
