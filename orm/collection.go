package orm

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/LouisRiverstone/migro"
)

// Collection is a list of models with Eloquent's collection helpers. It is a defined slice type,
// so it indexes, ranges and appends like []T, and converts to and from []T freely.
//
// Methods that read a column or the primary key need bound models (rows loaded through a query
// or a relation); on a collection of hand-built structs they return UNBOUND_MODEL.
type Collection[T any] []T

// Count is the number of models.
func (c Collection[T]) Count() int { return len(c) }

// IsEmpty reports whether there are no models.
func (c Collection[T]) IsEmpty() bool { return len(c) == 0 }

// First returns a pointer to the first model, or nil.
func (c Collection[T]) First() *T {
	if len(c) == 0 {
		return nil
	}
	return &c[0]
}

// Last returns a pointer to the last model, or nil.
func (c Collection[T]) Last() *T {
	if len(c) == 0 {
		return nil
	}
	return &c[len(c)-1]
}

// Each calls fn for every model; returning an error stops the walk.
func (c Collection[T]) Each(fn func(i int, m *T) error) error {
	for i := range c {
		if err := fn(i, &c[i]); err != nil {
			return err
		}
	}
	return nil
}

// appendBound appends a copy of src and re-binds the copy in its new slot, keeping its dirty
// state, so a model that moves between collections can still Save and lazy-load. out must have
// spare capacity (callers size it up front): growing the slice would move the slot.
func appendBound[T any](out Collection[T], src *T) Collection[T] {
	out = append(out, *src)
	dst := &out[len(out)-1]
	se, ok := any(src).(entity)
	if !ok {
		return out
	}
	sm := &se.base().m
	if sm.db == nil {
		return out
	}
	bind(sm.db, dst, sm.exists)
	dm := &any(dst).(entity).base().m
	dm.orig, dm.extras = sm.orig, sm.extras
	return out
}

// Filter keeps the models for which keep returns true. The result holds copies that stay bound
// to the database (Save and lazy relations work on them), but edits to a copy do not show in the
// original collection.
func (c Collection[T]) Filter(keep func(m *T) bool) Collection[T] {
	out := make(Collection[T], 0, len(c))
	for i := range c {
		if keep(&c[i]) {
			out = appendBound(out, &c[i])
		}
	}
	return out
}

// Reject is the inverse of Filter.
func (c Collection[T]) Reject(drop func(m *T) bool) Collection[T] {
	return c.Filter(func(m *T) bool { return !drop(m) })
}

// MapTo transforms every model into another type. It is a function because Go methods cannot
// introduce a type parameter.
func MapTo[T, R any](c Collection[T], fn func(m *T) R) []R {
	out := make([]R, len(c))
	for i := range c {
		out[i] = fn(&c[i])
	}
	return out
}

// Reduce folds the collection into one value.
func Reduce[T, A any](c Collection[T], initial A, fn func(acc A, m *T) A) A {
	acc := initial
	for i := range c {
		acc = fn(acc, &c[i])
	}
	return acc
}

// ---- column helpers -------------------------------------------------------------------------------

func (c Collection[T]) env() (*migro.DB, error) {
	if len(c) == 0 {
		return nil, nil
	}
	e, ok := any(&c[0]).(entity)
	if !ok {
		return nil, migro.Newf(migro.UnboundModel, "%T does not embed orm.BaseModel", c[0])
	}
	m, err := e.base().bound()
	if err != nil {
		return nil, err
	}
	return m.db, nil
}

func (c Collection[T]) values(col string) ([]any, error) {
	db, err := c.env()
	if err != nil || db == nil {
		return nil, err
	}
	sch, err := parseModel(db, new(T))
	if err != nil {
		return nil, err
	}
	out := make([]any, len(c))
	for i := range c {
		v, err := fieldOf(context.Background(), sch, reflect.ValueOf(&c[i]).Elem(), col)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (c Collection[T]) pk() (string, error) {
	db, err := c.env()
	if err != nil || db == nil {
		return "", err
	}
	sch, err := parseModel(db, new(T))
	if err != nil {
		return "", err
	}
	return primaryKey(sch)
}

// groupKey dereferences pointers so *uint columns key like uint ones; nil stays nil.
func groupKey(v any) any {
	rv := reflect.ValueOf(v)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}
	return rv.Interface()
}

// Pluck returns one column of every model (Eloquent's pluck).
func (c Collection[T]) Pluck(col string) ([]any, error) { return c.values(col) }

// PluckAs is Pluck with the values asserted to V; a value of another type is a CAST_FAILED error.
func PluckAs[T, V any](c Collection[T], col string) ([]V, error) {
	vals, err := c.values(col)
	if err != nil {
		return nil, err
	}
	out := make([]V, len(vals))
	for i, v := range vals {
		x, ok := v.(V)
		if !ok {
			return nil, migro.Newf(migro.CastFailed, "column %q holds %T, not %T", col, v, *new(V))
		}
		out[i] = x
	}
	return out, nil
}

// ModelKeys returns the primary key of every model.
func (c Collection[T]) ModelKeys() ([]any, error) {
	pk, err := c.pk()
	if err != nil || pk == "" {
		return nil, err
	}
	return c.values(pk)
}

// KeyBy indexes the models by a column. The map holds plain copies of the structs. The key keeps the field's Go type (uint for an ID),
// pointer columns are dereferenced and NULLs are skipped. Later duplicates win.
func (c Collection[T]) KeyBy(col string) (map[any]T, error) {
	vals, err := c.values(col)
	if err != nil {
		return nil, err
	}
	out := make(map[any]T, len(c))
	for i, v := range vals {
		if k := groupKey(v); k != nil {
			out[k] = c[i]
		}
	}
	return out, nil
}

// GroupBy buckets the models by a column; NULLs are skipped.
func (c Collection[T]) GroupBy(col string) (map[any]Collection[T], error) {
	vals, err := c.values(col)
	if err != nil {
		return nil, err
	}
	idx := map[any][]int{}
	for i, v := range vals {
		if k := groupKey(v); k != nil {
			idx[k] = append(idx[k], i)
		}
	}
	out := make(map[any]Collection[T], len(idx))
	for k, is := range idx {
		bucket := make(Collection[T], 0, len(is))
		for _, i := range is {
			bucket = appendBound(bucket, &c[i])
		}
		out[k] = bucket
	}
	return out, nil
}

// Unique keeps the first model of every distinct column value.
func (c Collection[T]) Unique(col string) (Collection[T], error) {
	vals, err := c.values(col)
	if err != nil {
		return nil, err
	}
	seen := map[any]bool{}
	out := make(Collection[T], 0, len(c))
	for i, v := range vals {
		k := groupKey(v)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = appendBound(out, &c[i])
	}
	return out, nil
}

// Find returns the model with the given primary key, or nil. Keys compare by printed value, so
// 1, uint(1) and "1" match.
func (c Collection[T]) Find(id any) (*T, error) {
	keys, err := c.ModelKeys()
	if err != nil {
		return nil, err
	}
	want := normKey(id)
	for i, k := range keys {
		if normKey(k) == want {
			return &c[i], nil
		}
	}
	return nil, nil
}

// Contains reports whether a model with that primary key is in the collection.
func (c Collection[T]) Contains(id any) (bool, error) {
	m, err := c.Find(id)
	return m != nil, err
}

// Only keeps the models whose primary key is in ids.
func (c Collection[T]) Only(ids ...any) (Collection[T], error) { return c.byKeys(ids, true) }

// Except drops the models whose primary key is in ids.
func (c Collection[T]) Except(ids ...any) (Collection[T], error) { return c.byKeys(ids, false) }

func (c Collection[T]) byKeys(ids []any, keep bool) (Collection[T], error) {
	keys, err := c.ModelKeys()
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[normKey(id)] = true
	}
	out := make(Collection[T], 0, len(c))
	for i, k := range keys {
		if set[normKey(k)] == keep {
			out = appendBound(out, &c[i])
		}
	}
	return out, nil
}

// ---- database helpers -----------------------------------------------------------------------------

// Load eager-loads relations onto every model with one query per relation (Eloquent's load()).
func (c Collection[T]) Load(ctx context.Context, relations ...string) error {
	db, err := c.env()
	if err != nil || db == nil {
		return err
	}
	done := map[string]bool{}
	for _, r := range relations {
		if err := loadPathOnce(ctx, db, c.owners(nil), r, nil, "", done); err != nil {
			return err
		}
	}
	return nil
}

// LoadMissing loads a relation only onto the models that do not have it loaded yet.
func (c Collection[T]) LoadMissing(ctx context.Context, relations ...string) error {
	db, err := c.env()
	if err != nil || db == nil {
		return err
	}
	done := map[string]bool{}
	for _, r := range relations {
		head, _, _ := strings.Cut(r, ".")
		missing := c.owners(func(v reflect.Value) bool {
			f := v.FieldByName(head)
			rf, ok := f.Addr().Interface().(relField)
			return f.IsValid() && ok && !rf.rm().loaded
		})
		if len(missing) == 0 {
			continue
		}
		if err := loadPathOnce(ctx, db, missing, r, nil, "", done); err != nil {
			return err
		}
	}
	return nil
}

func (c Collection[T]) owners(keep func(reflect.Value) bool) []reflect.Value {
	out := make([]reflect.Value, 0, len(c))
	for i := range c {
		v := reflect.ValueOf(&c[i]).Elem()
		if keep == nil || keep(v) {
			out = append(out, v)
		}
	}
	return out
}

// Fresh reloads the models from the database, keeping their order. Rows that no longer exist
// are dropped.
func (c Collection[T]) Fresh(ctx context.Context) (Collection[T], error) {
	db, err := c.env()
	if err != nil || db == nil {
		return Collection[T]{}, err
	}
	keys, err := c.ModelKeys()
	if err != nil {
		return nil, err
	}
	rows, err := New[T](db).FindMany(ctx, keys...)
	if err != nil {
		return nil, err
	}
	byKey, err := rows.KeyBy(mustPK(rows))
	if err != nil {
		return nil, err
	}
	norm := make(map[string]T, len(byKey))
	for k, v := range byKey {
		norm[normKey(k)] = v
	}
	out := Collection[T]{}
	for _, k := range keys {
		if row, ok := norm[normKey(k)]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func mustPK[T any](c Collection[T]) string {
	pk, _ := c.pk()
	return pk
}

// ---- serialization --------------------------------------------------------------------------------

// ToMaps serializes every model with ToMapWith.
func (c Collection[T]) ToMaps(opts ...SerializeOpt) ([]map[string]any, error) {
	out := make([]map[string]any, len(c))
	for i := range c {
		m, err := ToMapWith(&c[i], opts...)
		if err != nil {
			return nil, err
		}
		out[i] = m
	}
	return out, nil
}

// JSON encodes the collection as a JSON array of serialized models.
func (c Collection[T]) JSON(opts ...SerializeOpt) ([]byte, error) {
	maps, err := c.ToMaps(opts...)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(maps)
	if err != nil {
		return nil, migro.Wrap(migro.QueryFailed, err, "marshal")
	}
	return b, nil
}
