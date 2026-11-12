package orm

import (
	"context"
	"reflect"
	"strings"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
)

// WhereBelongsTo keeps the rows that belong to parent through a belongs-to relation
// (Eloquent's whereBelongsTo): posts.Query().WhereBelongsTo(user). With several relations to the
// same model, name the relation: WhereBelongsTo(user, "Editor").
func (q *Query[T]) WhereBelongsTo(parent any, relation ...string) *Query[T] {
	pv := reflect.Indirect(reflect.ValueOf(parent))
	if !pv.IsValid() || pv.Kind() != reflect.Struct {
		_ = q.g.AddError(migro.Newf(migro.QueryFailed, "WhereBelongsTo needs a model, got %T", parent))
		return q
	}
	owner := reflect.TypeFor[T]()
	var found []*relDef
	for _, i := range relationFields(owner) {
		name := owner.Field(i).Name
		if len(relation) > 0 && relation[0] != name {
			continue
		}
		d, _, err := resolveByName(q.db, owner, name)
		if err != nil {
			_ = q.g.AddError(err)
			return q
		}
		if d.kind == kBelongsTo && d.relT == pv.Type() {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		_ = q.g.AddError(migro.Newf(migro.InvalidSchema, "%s has no belongs-to relation to %s", owner.Name(), pv.Type().Name()))
		return q
	case 1:
	default:
		_ = q.g.AddError(migro.Newf(migro.QueryFailed, "%s has several belongs-to relations to %s: name one", owner.Name(), pv.Type().Name()))
		return q
	}
	d := found[0]
	key, err := fieldOf(context.Background(), d.relSch, pv, d.lk)
	if err != nil {
		_ = q.g.AddError(err)
		return q
	}
	return q.Where(q.db.Dialect.Quote(d.fk)+" = ?", key)
}

// WhereHasMorph keeps the rows whose morph-to relation points at one of the given morph aliases, and
// whose target satisfies scope (nil for any target): WhereHasMorph("Commentable", []string{"post"}, nil).
func (q *Query[T]) WhereHasMorph(relation string, aliases []string, scope scopeFn) *Query[T] {
	d, _, err := resolveByName(q.db, reflect.TypeFor[T](), relation)
	if err != nil {
		_ = q.g.AddError(err)
		return q
	}
	if d.kind != kMorphTo || len(aliases) == 0 {
		_ = q.g.AddError(migro.Newf(migro.QueryFailed, "WhereHasMorph needs a morph-to relation and at least one alias"))
		return q
	}
	quote := q.db.Dialect.Quote
	var conds []string
	var args []any
	for _, alias := range aliases {
		t, ok := morphModel(alias)
		if !ok {
			_ = q.g.AddError(migro.Newf(migro.UnknownMorphType, "morph type %q is not registered: call orm.MorphMap", alias))
			return q
		}
		sch, err := parseModel(q.db, reflect.New(t).Interface())
		if err != nil {
			_ = q.g.AddError(err)
			return q
		}
		pk, err := primaryKey(sch)
		if err != nil {
			_ = q.g.AddError(err)
			return q
		}
		sub := q.db.Gorm.Session(&gorm.Session{NewDB: true}).Model(reflect.New(t).Interface()).Select(pk)
		if scope != nil {
			sub = scope(sub)
		}
		conds = append(conds, quote(d.typeCol)+" = ? AND "+quote(d.fk)+" IN (?)")
		args = append(args, alias, sub)
	}
	return q.Where("("+strings.Join(conds, ") OR (")+")", args...)
}
