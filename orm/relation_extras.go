package orm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
	gschema "gorm.io/gorm/schema"
)

// ---- ofMany -----------------------------------------------------------------------------------

// ofManyDef makes a HasOne/MorphOne pick the one related row with the greatest (or least) value of a
// column: orm:"ofMany:latest" (newest primary key), "ofMany:latest:created_at", "ofMany:oldest",
// "ofMany:max:price", "ofMany:min:price" (Eloquent's latestOfMany, oldestOfMany, ofMany).
type ofManyDef struct{ agg, col string }

func parseOfMany(spec string, rel *gschema.Schema) (*ofManyDef, error) {
	kind, col, _ := strings.Cut(spec, ":")
	var agg string
	switch kind {
	case "latest", "max":
		agg = "MAX"
	case "oldest", "min":
		agg = "MIN"
	default:
		return nil, fmt.Errorf("ofMany wants latest, oldest, max:<col> or min:<col>, got %q", spec)
	}
	if col == "" {
		if kind == "max" || kind == "min" {
			return nil, fmt.Errorf("ofMany:%s needs a column", kind)
		}
		pk, err := primaryKey(rel)
		if err != nil {
			return nil, err
		}
		col = pk
	}
	if _, ok := rel.FieldsByDBName[col]; !ok {
		return nil, fmt.Errorf("%s has no column %q", rel.Name, col)
	}
	return &ofManyDef{agg: agg, col: col}, nil
}

// cond restricts a query over the related table to each owner's chosen row.
func (o *ofManyDef) cond(db *migro.DB, d *relDef) (string, []any) {
	q := db.Dialect.Quote
	t := q(d.relSch.Table)
	sql := t + "." + q(o.col) + " = (SELECT " + o.agg + "(" + q("migro_m") + "." + q(o.col) + ") FROM " + t + " " + q("migro_m") +
		" WHERE " + q("migro_m") + "." + q(d.fk) + " = " + t + "." + q(d.fk)
	var args []any
	if d.typeCol != "" {
		sql += " AND " + q("migro_m") + "." + q(d.typeCol) + " = ?"
		args = append(args, d.typeVal)
	}
	if f := softDeleteField(d.relSch); f != nil {
		sql += " AND " + q("migro_m") + "." + q(f.DBName) + " IS NULL"
	}
	return sql + ")", args
}

// ---- pivot constraints --------------------------------------------------------------------------

func (p pivotDef) prefix() string {
	if p.as == "" {
		return "pivot"
	}
	return p.as
}

// constrain adds the pivotWhere conditions, qualified with the pivot table.
func (p pivotDef) constrain(db *migro.DB, g *gorm.DB) *gorm.DB {
	for _, c := range p.where {
		g = g.Where(db.Dialect.Quote(p.table)+"."+db.Dialect.Quote(c.col)+" = ?", c.val)
	}
	return g
}

// ---- withDefault ------------------------------------------------------------------------------

var relTagCache = newTypeCache[map[int]tagOpts]()

// relTags returns the parsed orm tag of each relation field of t, by field index.
func relTags(t reflect.Type) map[int]tagOpts {
	return relTagCache.get(t, func() map[int]tagOpts {
		out := map[int]tagOpts{}
		for _, i := range relationFields(t) {
			out[i] = parseTag(t.Field(i).Tag.Get("orm"))
		}
		return out
	})
}

// ---- touches ----------------------------------------------------------------------------------

// Touches lists belongs-to relations whose parent gets a fresh updated_at whenever this model is
// created, saved, deleted or restored through the repository or the instance methods (Eloquent's
// $touches). Bulk query updates do not touch, as in Eloquent.
type Touches interface{ Touches() []string }

func touchParents(ctx context.Context, db *migro.DB, self any) error {
	return touchChain(ctx, db, self, map[string]bool{})
}

// touchChain touches the parents of self, then the parents of those parents that declare Touches
// themselves (Eloquent's touchOwners cascades the same way). seen breaks cycles.
func touchChain(ctx context.Context, db *migro.DB, self any, seen map[string]bool) error {
	t, ok := self.(Touches)
	if !ok || db == nil {
		return nil
	}
	rv := reflect.ValueOf(self).Elem()
	for _, name := range t.Touches() {
		d, _, err := resolveByName(db, rv.Type(), name)
		if err != nil {
			return err
		}
		if d.kind != kBelongsTo {
			return migro.Newf(migro.InvalidSchema, "Touches: %s.%s is not a belongs-to relation", rv.Type().Name(), name)
		}
		key, err := fieldOf(ctx, d.ownerSch, rv, d.fk)
		if err != nil {
			return err
		}
		if blank(key) {
			continue
		}
		col := ""
		for _, f := range d.relSch.Fields {
			if f.AutoUpdateTime > 0 {
				col = f.DBName
				break
			}
		}
		if col == "" {
			continue
		}
		g := db.Gorm.WithContext(ctx).Model(reflect.New(d.relT).Interface()).Where(db.Dialect.Quote(d.lk)+" = ?", key)
		if err := wrap(g.UpdateColumn(col, time.Now()).Error, "touch "+name); err != nil {
			return err
		}
		parent := reflect.New(d.relT).Interface()
		if _, cascades := parent.(Touches); !cascades {
			continue
		}
		id := fmt.Sprintf("%s|%s|%v", d.relT, d.lk, key)
		if seen[id] {
			continue
		}
		seen[id] = true
		err = db.Gorm.WithContext(ctx).Where(db.Dialect.Quote(d.lk)+" = ?", key).Take(parent).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return wrap(err, "touch "+name)
		}
		if err := touchChain(ctx, db, parent, seen); err != nil {
			return err
		}
	}
	return nil
}

// ---- SaveMany / Push ----------------------------------------------------------------------------

// SaveMany links and persists each child (Eloquent's saveMany).
func (r *HasMany[T]) SaveMany(ctx context.Context, children []T) error {
	return saveMany(ctx, &r.relMeta, children)
}

// SaveMany links and persists each child.
func (r *MorphMany[T]) SaveMany(ctx context.Context, children []T) error {
	return saveMany(ctx, &r.relMeta, children)
}

func saveMany[T any](ctx context.Context, m *relMeta, children []T) error {
	return m.db.Transaction(ctx, func(tx *migro.DB) error {
		scoped := *m
		scoped.db = tx
		for i := range children {
			if err := saveChild(ctx, &scoped, &children[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

// Push saves v and every relation loaded on it, depth first (Eloquent's push): a loaded belongs-to
// parent is saved first and v's foreign key follows it; loaded children are linked to v and saved
// after it. Everything runs in one transaction.
func Push[T any](ctx context.Context, v *T) error {
	e, ok := any(v).(entity)
	if !ok {
		return migro.Newf(migro.UnboundModel, "%T does not embed orm.BaseModel", v)
	}
	m, err := e.base().bound()
	if err != nil {
		return err
	}
	return m.db.Transaction(ctx, func(tx *migro.DB) error {
		return push(ctx, tx, reflect.ValueOf(v), map[uintptr]bool{})
	})
}

func push(ctx context.Context, db *migro.DB, ptr reflect.Value, seen map[uintptr]bool) error {
	if seen[ptr.Pointer()] {
		return nil
	}
	seen[ptr.Pointer()] = true
	e, ok := ptr.Interface().(entity)
	if !ok {
		return migro.Newf(migro.UnboundModel, "%s does not embed orm.BaseModel", ptr.Type())
	}
	e.base().m.db = db // the whole push shares one transaction
	st := ptr.Elem()
	loaded := func(i int) (relField, bool) {
		rf := st.Field(i).Addr().Interface().(relField)
		return rf, rf.rm().loaded
	}

	for _, i := range relationFields(st.Type()) { // parents first, so their keys exist
		rf, ok := loaded(i)
		if !ok || rf.kind() != kBelongsTo {
			continue
		}
		for _, c := range rf.children() {
			if err := push(ctx, db, c, seen); err != nil {
				return err
			}
			if err := adoptParentKey(ctx, db, ptr, st.Type().Field(i).Name, c); err != nil {
				return err
			}
		}
	}
	if err := e.base().Save(ctx); err != nil {
		return err
	}
	for _, i := range relationFields(st.Type()) {
		rf, ok := loaded(i)
		if !ok {
			continue
		}
		switch rf.kind() {
		case kHasOne, kHasMany, kMorphOne, kMorphMany:
			meta := rf.rm()
			for _, c := range rf.children() {
				if err := meta.stamp(ctx, c.Interface()); err != nil {
					return err
				}
				if err := push(ctx, db, c, seen); err != nil {
					return err
				}
			}
		case kBelongsTo:
		default:
			for _, c := range rf.children() {
				if err := push(ctx, db, c, seen); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func adoptParentKey(ctx context.Context, db *migro.DB, owner reflect.Value, relation string, parent reflect.Value) error {
	d, _, err := resolveByName(db, owner.Elem().Type(), relation)
	if err != nil {
		return err
	}
	key, err := fieldOf(ctx, d.relSch, parent.Elem(), d.lk)
	if err != nil {
		return err
	}
	f, ok := d.ownerSch.FieldsByDBName[d.fk]
	if !ok {
		return migro.Newf(migro.InvalidSchema, "%s has no column %q", d.ownerSch.Name, d.fk)
	}
	return f.Set(ctx, owner.Elem(), key)
}
