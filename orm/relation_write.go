package orm

import (
	"context"
	"reflect"
	"time"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
)

// ---- has-one / has-many / morph-one / morph-many writes ---------------------------------------

func (m *relMeta) defOf() (*relDef, error) {
	if err := m.unbound(); err != nil {
		return nil, err
	}
	d, _, err := resolveByName(m.db, m.ownerValue().Type(), m.name)
	return d, err
}

// stamp sets the foreign key (and morph type) on a child so it points at this relation's owner.
func (m *relMeta) stamp(ctx context.Context, child any) error {
	d, err := m.defOf()
	if err != nil {
		return err
	}
	if d.kind != kHasOne && d.kind != kHasMany && d.kind != kMorphOne && d.kind != kMorphMany {
		return migro.Newf(migro.InvalidSchema, "relation %s does not own its children", m.name)
	}
	key, err := fieldOf(ctx, d.ownerSch, m.ownerValue(), d.lk)
	if err != nil {
		return err
	}
	cv := reflect.ValueOf(child).Elem()
	set := func(col string, val any) error {
		f, ok := d.relSch.FieldsByDBName[col]
		if !ok {
			return migro.Newf(migro.InvalidSchema, "%s has no column %q", d.relSch.Name, col)
		}
		return f.Set(ctx, cv, val)
	}
	if err := set(d.fk, key); err != nil {
		return err
	}
	if d.typeCol != "" {
		return set(d.typeCol, d.typeVal)
	}
	return nil
}

func saveChild[T any](ctx context.Context, m *relMeta, child *T) error {
	if err := m.stamp(ctx, child); err != nil {
		return err
	}
	bind(m.db, child, !pkIsZero(m.db, child))
	if e, ok := any(child).(entity); ok {
		return e.base().Save(ctx)
	}
	return wrap(m.db.Gorm.WithContext(ctx).Save(child).Error, "save related")
}

func createChild[T any](ctx context.Context, m *relMeta, attrs map[string]any) (*T, error) {
	child := new(T)
	bind(m.db, child, false)
	if err := fill(ctx, m.db, child, attrs); err != nil {
		return nil, err
	}
	if err := saveChild(ctx, m, child); err != nil {
		return nil, err
	}
	return child, nil
}

// Save sets the foreign key on child and persists it (Eloquent's $user->posts()->save($post)).
func (r *HasMany[T]) Save(ctx context.Context, child *T) error {
	return saveChild(ctx, &r.relMeta, child)
}

// Create builds a child from attrs (mass assignment rules apply), links and inserts it.
func (r *HasMany[T]) Create(ctx context.Context, attrs map[string]any) (*T, error) {
	return createChild[T](ctx, &r.relMeta, attrs)
}

// CreateMany creates one child per attrs map, atomically.
func (r *HasMany[T]) CreateMany(ctx context.Context, rows []map[string]any) ([]T, error) {
	return createManyChildren[T](ctx, &r.relMeta, rows)
}

func (r *HasOne[T]) Save(ctx context.Context, child *T) error {
	return saveChild(ctx, &r.relMeta, child)
}
func (r *HasOne[T]) Create(ctx context.Context, attrs map[string]any) (*T, error) {
	return createChild[T](ctx, &r.relMeta, attrs)
}

func (r *MorphMany[T]) Save(ctx context.Context, child *T) error {
	return saveChild(ctx, &r.relMeta, child)
}
func (r *MorphMany[T]) Create(ctx context.Context, attrs map[string]any) (*T, error) {
	return createChild[T](ctx, &r.relMeta, attrs)
}
func (r *MorphMany[T]) CreateMany(ctx context.Context, rows []map[string]any) ([]T, error) {
	return createManyChildren[T](ctx, &r.relMeta, rows)
}
func (r *MorphOne[T]) Save(ctx context.Context, child *T) error {
	return saveChild(ctx, &r.relMeta, child)
}
func (r *MorphOne[T]) Create(ctx context.Context, attrs map[string]any) (*T, error) {
	return createChild[T](ctx, &r.relMeta, attrs)
}

func createManyChildren[T any](ctx context.Context, m *relMeta, rows []map[string]any) ([]T, error) {
	if err := m.unbound(); err != nil {
		return nil, err
	}
	out := make([]T, 0, len(rows))
	err := m.db.Transaction(ctx, func(tx *migro.DB) error {
		scoped := *m
		scoped.db = tx
		for _, attrs := range rows {
			c, err := createChild[T](ctx, &scoped, attrs)
			if err != nil {
				return err
			}
			out = append(out, *c)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	bindAll(m.db, out, true)
	return out, nil
}

// ---- belongs-to ---------------------------------------------------------------------------------

// Associate points the owner's foreign key at parent (it does not save the owner).
func (r *BelongsTo[T]) Associate(parent *T) error {
	d, err := r.defOf()
	if err != nil {
		return err
	}
	key, err := fieldOf(context.Background(), d.relSch, reflect.ValueOf(parent).Elem(), d.lk)
	if err != nil {
		return err
	}
	f := d.ownerSch.FieldsByDBName[d.fk]
	if err := f.Set(context.Background(), r.ownerValue(), key); err != nil {
		return migro.Wrap(migro.InvalidSchema, err, "associate %s", r.name)
	}
	r.item, r.loaded = parent, true
	return nil
}

// Dissociate clears the owner's foreign key (the column must be nullable; it does not save the owner).
func (r *BelongsTo[T]) Dissociate() error {
	d, err := r.defOf()
	if err != nil {
		return err
	}
	f := d.ownerSch.FieldsByDBName[d.fk]
	r.ownerValue().FieldByIndex(f.StructField.Index).Set(reflect.Zero(f.FieldType))
	r.item, r.loaded = nil, true
	return nil
}

// ---- many-to-many pivot writes -------------------------------------------------------------------

type pivotOps struct{ m *relMeta }

func (p pivotOps) def() (*relDef, any, error) {
	d, err := p.m.defOf()
	if err != nil {
		return nil, nil, err
	}
	key, err := fieldOf(context.Background(), d.ownerSch, p.m.ownerValue(), d.lk)
	return d, key, err
}

// keyOf accepts either a raw key or a model whose key the relation points at.
func keyOf(ctx context.Context, d *relDef, v any) (any, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() && rv.Elem().Kind() == reflect.Struct {
		return fieldOf(ctx, d.relSch, rv.Elem(), d.rk)
	}
	return v, nil
}

func (d *relDef) pivotBase(ctx context.Context, db *migro.DB, ownerKey any) *gorm.DB {
	p := d.pivot
	g := db.Gorm.WithContext(ctx).Table(p.table).Where(db.Dialect.Quote(p.ownerCol)+" = ?", ownerKey)
	if p.typeCol != "" {
		g = g.Where(db.Dialect.Quote(p.typeCol)+" = ?", p.typeVal)
	}
	return g
}

func (d *relDef) existing(ctx context.Context, db *migro.DB, ownerKey any) (map[string]any, error) {
	var rows []map[string]any
	if err := d.pivotBase(ctx, db, ownerKey).Select([]string{d.pivot.relatedCol}).Find(&rows).Error; err != nil {
		return nil, wrap(err, "read pivot "+d.pivot.table)
	}
	have := make(map[string]any, len(rows))
	for _, r := range rows {
		have[normKey(r[d.pivot.relatedCol])] = r[d.pivot.relatedCol]
	}
	return have, nil
}

func (d *relDef) attach(ctx context.Context, db *migro.DB, ownerKey any, ids []any, attrs map[string]any) ([]any, error) {
	have, err := d.existing(ctx, db, ownerKey)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	var added []any
	seen := map[string]bool{}
	for _, id := range ids {
		n := normKey(id)
		if _, ok := have[n]; ok || seen[n] {
			continue
		}
		seen[n] = true
		row := map[string]any{d.pivot.ownerCol: ownerKey, d.pivot.relatedCol: id}
		if d.pivot.typeCol != "" {
			row[d.pivot.typeCol] = d.pivot.typeVal
		}
		if d.pivot.timestamps {
			now := time.Now()
			row["created_at"], row["updated_at"] = now, now
		}
		for k, v := range attrs {
			row[k] = v
		}
		rows, added = append(rows, row), append(added, id)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return added, wrap(db.Gorm.WithContext(ctx).Table(d.pivot.table).Create(rows).Error, "attach "+d.pivot.table)
}

func (d *relDef) detach(ctx context.Context, db *migro.DB, ownerKey any, ids []any) error {
	g := d.pivotBase(ctx, db, ownerKey)
	if len(ids) > 0 {
		g = g.Where(inClause(db, d.pivot.relatedCol), ids)
	}
	return wrap(g.Delete(&map[string]any{}).Error, "detach "+d.pivot.table)
}

// SyncResult reports what Sync / Toggle changed.
type SyncResult struct{ Attached, Detached []any }

func (p pivotOps) ids(ctx context.Context, d *relDef, raw []any) ([]any, error) {
	out := make([]any, 0, len(raw))
	for _, v := range raw {
		k, err := keyOf(ctx, d, v)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

func (p pivotOps) attach(ctx context.Context, attrs map[string]any, raw []any) error {
	d, key, err := p.def()
	if err != nil {
		return err
	}
	ids, err := p.ids(ctx, d, raw)
	if err != nil {
		return err
	}
	_, err = d.attach(ctx, p.m.db, key, ids, attrs)
	p.m.loaded = false
	return err
}

func (p pivotOps) detach(ctx context.Context, raw []any) error {
	d, key, err := p.def()
	if err != nil {
		return err
	}
	ids, err := p.ids(ctx, d, raw)
	if err != nil {
		return err
	}
	p.m.loaded = false
	return d.detach(ctx, p.m.db, key, ids)
}

func (p pivotOps) sync(ctx context.Context, detaching bool, raw []any) (SyncResult, error) {
	var res SyncResult
	d, key, err := p.def()
	if err != nil {
		return res, err
	}
	ids, err := p.ids(ctx, d, raw)
	if err != nil {
		return res, err
	}
	err = p.m.db.Transaction(ctx, func(tx *migro.DB) error {
		have, err := d.existing(ctx, tx, key)
		if err != nil {
			return err
		}
		want := map[string]bool{}
		for _, id := range ids {
			want[normKey(id)] = true
		}
		if detaching {
			for n, id := range have {
				if !want[n] {
					res.Detached = append(res.Detached, id)
				}
			}
			if len(res.Detached) > 0 {
				if err := d.detach(ctx, tx, key, res.Detached); err != nil {
					return err
				}
			}
		}
		res.Attached, err = d.attach(ctx, tx, key, ids, nil)
		return err
	})
	p.m.loaded = false
	return res, err
}

func (p pivotOps) toggle(ctx context.Context, raw []any) (SyncResult, error) {
	var res SyncResult
	d, key, err := p.def()
	if err != nil {
		return res, err
	}
	ids, err := p.ids(ctx, d, raw)
	if err != nil {
		return res, err
	}
	err = p.m.db.Transaction(ctx, func(tx *migro.DB) error {
		have, err := d.existing(ctx, tx, key)
		if err != nil {
			return err
		}
		var add []any
		for _, id := range ids {
			if _, ok := have[normKey(id)]; ok {
				res.Detached = append(res.Detached, id)
				continue
			}
			add = append(add, id)
		}
		if len(res.Detached) > 0 {
			if err := d.detach(ctx, tx, key, res.Detached); err != nil {
				return err
			}
		}
		res.Attached, err = d.attach(ctx, tx, key, add, nil)
		return err
	})
	p.m.loaded = false
	return res, err
}

func (p pivotOps) updateExisting(ctx context.Context, raw any, attrs map[string]any) error {
	d, key, err := p.def()
	if err != nil {
		return err
	}
	id, err := keyOf(ctx, d, raw)
	if err != nil {
		return err
	}
	if d.pivot.timestamps {
		attrs = mergeMap(attrs, map[string]any{"updated_at": time.Now()})
	}
	g := d.pivotBase(ctx, p.m.db, key).Where(p.m.db.Dialect.Quote(d.pivot.relatedCol)+" = ?", id)
	p.m.loaded = false
	return wrap(g.Updates(attrs).Error, "update pivot "+d.pivot.table)
}

func mergeMap(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// pivotWriter is embedded by every many-to-many relation so they share Attach/Detach/Sync/Toggle.
// ids may be raw keys or model pointers.
func (r *BelongsToMany[T]) ops() pivotOps { return pivotOps{&r.relMeta} }
func (r *MorphToMany[T]) ops() pivotOps   { return pivotOps{&r.relMeta} }
func (r *MorphedByMany[T]) ops() pivotOps { return pivotOps{&r.relMeta} }

// Attach links the given keys (or models), skipping links that already exist.
func (r *BelongsToMany[T]) Attach(ctx context.Context, ids ...any) error {
	return r.ops().attach(ctx, nil, ids)
}

// AttachWith links keys and stores extra pivot columns on the new rows.
func (r *BelongsToMany[T]) AttachWith(ctx context.Context, attrs map[string]any, ids ...any) error {
	return r.ops().attach(ctx, attrs, ids)
}
func (r *BelongsToMany[T]) Detach(ctx context.Context, ids ...any) error {
	return r.ops().detach(ctx, ids)
}

// Sync makes the links exactly ids; SyncWithoutDetaching only adds.
func (r *BelongsToMany[T]) Sync(ctx context.Context, ids ...any) (SyncResult, error) {
	return r.ops().sync(ctx, true, ids)
}
func (r *BelongsToMany[T]) SyncWithoutDetaching(ctx context.Context, ids ...any) (SyncResult, error) {
	return r.ops().sync(ctx, false, ids)
}
func (r *BelongsToMany[T]) Toggle(ctx context.Context, ids ...any) (SyncResult, error) {
	return r.ops().toggle(ctx, ids)
}
func (r *BelongsToMany[T]) UpdateExistingPivot(ctx context.Context, id any, attrs map[string]any) error {
	return r.ops().updateExisting(ctx, id, attrs)
}

func (r *MorphToMany[T]) Attach(ctx context.Context, ids ...any) error {
	return r.ops().attach(ctx, nil, ids)
}
func (r *MorphToMany[T]) AttachWith(ctx context.Context, attrs map[string]any, ids ...any) error {
	return r.ops().attach(ctx, attrs, ids)
}
func (r *MorphToMany[T]) Detach(ctx context.Context, ids ...any) error {
	return r.ops().detach(ctx, ids)
}
func (r *MorphToMany[T]) Sync(ctx context.Context, ids ...any) (SyncResult, error) {
	return r.ops().sync(ctx, true, ids)
}
func (r *MorphToMany[T]) SyncWithoutDetaching(ctx context.Context, ids ...any) (SyncResult, error) {
	return r.ops().sync(ctx, false, ids)
}
func (r *MorphToMany[T]) Toggle(ctx context.Context, ids ...any) (SyncResult, error) {
	return r.ops().toggle(ctx, ids)
}

func (r *MorphedByMany[T]) Attach(ctx context.Context, ids ...any) error {
	return r.ops().attach(ctx, nil, ids)
}
func (r *MorphedByMany[T]) Detach(ctx context.Context, ids ...any) error {
	return r.ops().detach(ctx, ids)
}
func (r *MorphedByMany[T]) Sync(ctx context.Context, ids ...any) (SyncResult, error) {
	return r.ops().sync(ctx, true, ids)
}
