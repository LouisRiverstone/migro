package orm

import (
	"context"
	"fmt"
	"reflect"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
	gschema "gorm.io/gorm/schema"
)

// Relations here cover what GORM lacks (Lucid's hasManyThrough and the polymorphic family).
// They are batch loaders: one query per hop, never per parent, and they return maps keyed by the
// parent key you passed in, instead of mutating structs.
//
// Keys are compared by their printed value, so uint, int64 and string-encoded numbers match.
// Very large key lists are not chunked; split them when your database caps IN-list size.

func normKey(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return ""
		}
		rv = rv.Elem()
	}
	return fmt.Sprint(rv.Interface())
}

func uniqueKeys(keys []any) (vals []any, orig map[string]any) {
	orig = make(map[string]any, len(keys))
	for _, k := range keys {
		n := normKey(k)
		if _, dup := orig[n]; dup {
			continue
		}
		orig[n] = k
		vals = append(vals, k)
	}
	return vals, orig
}

func parseModel(db *migro.DB, model any) (*gschema.Schema, error) {
	stmt := &gorm.Statement{DB: db.Gorm}
	if err := stmt.Parse(model); err != nil {
		return nil, migro.Wrap(migro.QueryFailed, err, "parse model %T", model)
	}
	return stmt.Schema, nil
}

func fieldOf(ctx context.Context, sch *gschema.Schema, rv reflect.Value, column string) (any, error) {
	f, ok := sch.FieldsByDBName[column]
	if !ok {
		return nil, migro.Newf(migro.InvalidSchema, "model %s has no column %q", sch.Name, column)
	}
	v, _ := f.ValueOf(ctx, rv)
	return v, nil
}

func inClause(db *migro.DB, col string) string { return db.Dialect.Quote(col) + " IN ?" }

// ---- hasManyThrough ------------------------------------------------------------------

// HasManyThrough loads T rows reachable from a parent via a Through model, like Lucid's
// hasManyThrough: Country -> User (Through) -> Post (T).
//
//	ForeignKey         column on Through pointing at the parent   (users.country_id)
//	ThroughLocalKey    Through's key referenced by T              (users.id), default "id"
//	ThroughForeignKey  column on T pointing at Through            (posts.user_id)
type HasManyThrough[T any, Through any] struct {
	ForeignKey        string
	ThroughLocalKey   string
	ThroughForeignKey string
	// Scope optionally filters or orders the T query.
	Scope func(*gorm.DB) *gorm.DB
}

// Load returns, per parent key, the T rows reachable through Through.
func (r HasManyThrough[T, Through]) Load(ctx context.Context, db *migro.DB, parentKeys []any) (map[any][]T, error) {
	out := map[any][]T{}
	keys, origKeys := uniqueKeys(parentKeys)
	if len(keys) == 0 {
		return out, nil
	}
	if r.ForeignKey == "" || r.ThroughForeignKey == "" {
		return nil, migro.Newf(migro.InvalidSchema, "HasManyThrough needs ForeignKey and ThroughForeignKey")
	}
	localKey := r.ThroughLocalKey
	if localKey == "" {
		localKey = "id"
	}
	thSchema, err := parseModel(db, new(Through))
	if err != nil {
		return nil, err
	}
	tSchema, err := parseModel(db, new(T))
	if err != nil {
		return nil, err
	}

	var throughs []Through
	if err := db.Gorm.WithContext(ctx).Where(inClause(db, r.ForeignKey), keys).Find(&throughs).Error; err != nil {
		return nil, wrap(err, "load through rows")
	}
	throughToParent := map[string]string{}
	var throughKeys []any
	for i := range throughs {
		rv := reflect.ValueOf(&throughs[i]).Elem()
		parent, err := fieldOf(ctx, thSchema, rv, r.ForeignKey)
		if err != nil {
			return nil, err
		}
		local, err := fieldOf(ctx, thSchema, rv, localKey)
		if err != nil {
			return nil, err
		}
		throughToParent[normKey(local)] = normKey(parent)
		throughKeys = append(throughKeys, local)
	}
	if len(throughKeys) == 0 {
		return out, nil
	}

	var targets []T
	q := db.Gorm.WithContext(ctx).Where(inClause(db, r.ThroughForeignKey), throughKeys)
	if r.Scope != nil {
		q = r.Scope(q)
	}
	if err := q.Find(&targets).Error; err != nil {
		return nil, wrap(err, "load target rows")
	}
	for i := range targets {
		fk, err := fieldOf(ctx, tSchema, reflect.ValueOf(&targets[i]).Elem(), r.ThroughForeignKey)
		if err != nil {
			return nil, err
		}
		parent, ok := throughToParent[normKey(fk)]
		if !ok {
			continue
		}
		key := origKeys[parent]
		out[key] = append(out[key], targets[i])
	}
	return out, nil
}

// ---- morphTo -------------------------------------------------------------------------

// Ref points at a row of a registered morph type. ID must be comparable.
type Ref struct {
	Type string
	ID   any
}

// MorphRegistry maps type aliases (the value stored in a *_type column) to model loaders.
// It is an instance, not a global, so each application wires its own.
type MorphRegistry struct {
	loaders map[string]func(ctx context.Context, db *migro.DB, ids []any) (map[string]any, error)
}

func NewMorphRegistry() *MorphRegistry {
	return &MorphRegistry{loaders: map[string]func(context.Context, *migro.DB, []any) (map[string]any, error){}}
}

// RegisterMorph registers model T under alias (e.g. "post"). T must have a single primary key.
func RegisterMorph[T any](r *MorphRegistry, db *migro.DB, alias string) error {
	sch, err := parseModel(db, new(T))
	if err != nil {
		return err
	}
	if len(sch.PrimaryFields) != 1 {
		return migro.Newf(migro.InvalidSchema, "morph model %s needs exactly one primary key", sch.Name)
	}
	pk := sch.PrimaryFields[0].DBName
	r.loaders[alias] = func(ctx context.Context, db *migro.DB, ids []any) (map[string]any, error) {
		var rows []T
		if err := db.Gorm.WithContext(ctx).Where(inClause(db, pk), ids).Find(&rows).Error; err != nil {
			return nil, wrap(err, "load morph "+alias)
		}
		out := make(map[string]any, len(rows))
		for i := range rows {
			v, err := fieldOf(ctx, sch, reflect.ValueOf(&rows[i]).Elem(), pk)
			if err != nil {
				return nil, err
			}
			out[normKey(v)] = &rows[i]
		}
		return out, nil
	}
	return nil
}

// Load resolves refs to model pointers (*T of the registered type) with one query per type.
// Refs whose row no longer exists are absent from the result.
func (r *MorphRegistry) Load(ctx context.Context, db *migro.DB, refs []Ref) (map[Ref]any, error) {
	byType := map[string][]any{}
	for _, ref := range refs {
		if _, ok := r.loaders[ref.Type]; !ok {
			return nil, migro.Newf(migro.UnknownMorphType, "morph type %q is not registered", ref.Type)
		}
		byType[ref.Type] = append(byType[ref.Type], ref.ID)
	}
	out := make(map[Ref]any, len(refs))
	for typ, ids := range byType {
		uniq, _ := uniqueKeys(ids)
		found, err := r.loaders[typ](ctx, db, uniq)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			if ref.Type != typ {
				continue
			}
			if m, ok := found[normKey(ref.ID)]; ok {
				out[ref] = m
			}
		}
	}
	return out, nil
}

// ---- morphToMany / morphedByMany ---------------------------------------------------------

// Pivot describes a polymorphic pivot table, e.g. taggables(tag_id, taggable_id, taggable_type).
type Pivot struct {
	Table         string
	MorphIDColumn string // taggable_id
	MorphTypeCol  string // taggable_type
	RelatedIDCol  string // tag_id
	// MorphType is the alias stored in MorphTypeCol for the morphable side (e.g. "post").
	MorphType string
}

func (p Pivot) validate() error {
	if p.Table == "" || p.MorphIDColumn == "" || p.MorphTypeCol == "" || p.RelatedIDCol == "" || p.MorphType == "" {
		return migro.Newf(migro.InvalidSchema, "pivot needs Table, MorphIDColumn, MorphTypeCol, RelatedIDCol and MorphType")
	}
	return nil
}

func (p Pivot) base(ctx context.Context, db *migro.DB) *gorm.DB {
	return db.Gorm.WithContext(ctx).Table(p.Table).Where(db.Dialect.Quote(p.MorphTypeCol)+" = ?", p.MorphType)
}

// MorphToMany loads T (the related side, e.g. Tag) for polymorphic parents (e.g. Post).
// RelatedKey is T's key referenced by the pivot, default "id".
type MorphToMany[T any] struct {
	Pivot      Pivot
	RelatedKey string
	Scope      func(*gorm.DB) *gorm.DB
}

// MorphedByMany is the inverse: given related keys (Tags), load the morphable T (Posts).
// MorphKey is T's key stored in the pivot, default "id".
type MorphedByMany[T any] struct {
	Pivot    Pivot
	MorphKey string
	Scope    func(*gorm.DB) *gorm.DB
}

func (r MorphToMany[T]) Load(ctx context.Context, db *migro.DB, parentKeys []any) (map[any][]T, error) {
	return loadViaPivot[T](ctx, db, r.Pivot, r.Pivot.MorphIDColumn, r.Pivot.RelatedIDCol, keyOr(r.RelatedKey), r.Scope, parentKeys)
}

func (r MorphedByMany[T]) Load(ctx context.Context, db *migro.DB, relatedKeys []any) (map[any][]T, error) {
	return loadViaPivot[T](ctx, db, r.Pivot, r.Pivot.RelatedIDCol, r.Pivot.MorphIDColumn, keyOr(r.MorphKey), r.Scope, relatedKeys)
}

func keyOr(k string) string {
	if k == "" {
		return "id"
	}
	return k
}

func loadViaPivot[T any](ctx context.Context, db *migro.DB, p Pivot, fromCol, toCol, targetKey string, scope func(*gorm.DB) *gorm.DB, keys []any) (map[any][]T, error) {
	out := map[any][]T{}
	if err := p.validate(); err != nil {
		return nil, err
	}
	uniq, orig := uniqueKeys(keys)
	if len(uniq) == 0 {
		return out, nil
	}
	var rows []map[string]any
	if err := p.base(ctx, db).Select([]string{fromCol, toCol}).Where(inClause(db, fromCol), uniq).Find(&rows).Error; err != nil {
		return nil, wrap(err, "load pivot "+p.Table)
	}
	type link struct{ from, to string }
	var links []link
	var toVals []any
	for _, row := range rows {
		links = append(links, link{normKey(row[fromCol]), normKey(row[toCol])})
		toVals = append(toVals, row[toCol])
	}
	if len(toVals) == 0 {
		return out, nil
	}
	tSchema, err := parseModel(db, new(T))
	if err != nil {
		return nil, err
	}
	uniqTo, _ := uniqueKeys(toVals)
	var targets []T
	q := db.Gorm.WithContext(ctx).Where(inClause(db, targetKey), uniqTo)
	if scope != nil {
		q = scope(q)
	}
	if err := q.Find(&targets).Error; err != nil {
		return nil, wrap(err, "load related rows")
	}
	byKey := make(map[string]T, len(targets))
	for i := range targets {
		v, err := fieldOf(ctx, tSchema, reflect.ValueOf(&targets[i]).Elem(), targetKey)
		if err != nil {
			return nil, err
		}
		byKey[normKey(v)] = targets[i]
	}
	for _, l := range links {
		if t, ok := byKey[l.to]; ok {
			k := orig[l.from]
			out[k] = append(out[k], t)
		}
	}
	return out, nil
}

// Attach links related keys to the parent, skipping links that already exist.
func (r MorphToMany[T]) Attach(ctx context.Context, db *migro.DB, parentKey any, relatedKeys ...any) error {
	if err := r.Pivot.validate(); err != nil {
		return err
	}
	return db.Transaction(ctx, func(tx *migro.DB) error { return r.attach(ctx, tx, parentKey, relatedKeys) })
}

func (r MorphToMany[T]) existing(ctx context.Context, db *migro.DB, parentKey any) (map[string]any, error) {
	var rows []map[string]any
	q := r.Pivot.base(ctx, db).Select([]string{r.Pivot.RelatedIDCol}).Where(db.Dialect.Quote(r.Pivot.MorphIDColumn)+" = ?", parentKey)
	if err := q.Find(&rows).Error; err != nil {
		return nil, wrap(err, "read pivot "+r.Pivot.Table)
	}
	have := make(map[string]any, len(rows))
	for _, row := range rows {
		have[normKey(row[r.Pivot.RelatedIDCol])] = row[r.Pivot.RelatedIDCol]
	}
	return have, nil
}

func (r MorphToMany[T]) attach(ctx context.Context, db *migro.DB, parentKey any, relatedKeys []any) error {
	have, err := r.existing(ctx, db, parentKey)
	if err != nil {
		return err
	}
	var rows []map[string]any
	seen := map[string]bool{}
	for _, k := range relatedKeys {
		n := normKey(k)
		if _, ok := have[n]; ok || seen[n] {
			continue
		}
		seen[n] = true
		rows = append(rows, map[string]any{r.Pivot.MorphIDColumn: parentKey, r.Pivot.MorphTypeCol: r.Pivot.MorphType, r.Pivot.RelatedIDCol: k})
	}
	if len(rows) == 0 {
		return nil
	}
	return wrap(db.Gorm.WithContext(ctx).Table(r.Pivot.Table).Create(rows).Error, "attach "+r.Pivot.Table)
}

// Detach removes the given links, or every link of the parent when no keys are passed.
func (r MorphToMany[T]) Detach(ctx context.Context, db *migro.DB, parentKey any, relatedKeys ...any) error {
	if err := r.Pivot.validate(); err != nil {
		return err
	}
	q := r.Pivot.base(ctx, db).Where(db.Dialect.Quote(r.Pivot.MorphIDColumn)+" = ?", parentKey)
	if len(relatedKeys) > 0 {
		q = q.Where(inClause(db, r.Pivot.RelatedIDCol), relatedKeys)
	}
	return wrap(q.Delete(&map[string]any{}).Error, "detach "+r.Pivot.Table)
}

// Sync makes the parent's links exactly relatedKeys, atomically.
func (r MorphToMany[T]) Sync(ctx context.Context, db *migro.DB, parentKey any, relatedKeys ...any) error {
	if err := r.Pivot.validate(); err != nil {
		return err
	}
	return db.Transaction(ctx, func(tx *migro.DB) error {
		have, err := r.existing(ctx, tx, parentKey)
		if err != nil {
			return err
		}
		want := make(map[string]bool, len(relatedKeys))
		for _, k := range relatedKeys {
			want[normKey(k)] = true
		}
		var stale []any
		for n, raw := range have {
			if !want[n] {
				stale = append(stale, raw)
			}
		}
		if len(stale) > 0 {
			if err := r.Detach(ctx, tx, parentKey, stale...); err != nil {
				return err
			}
		}
		return r.attach(ctx, tx, parentKey, relatedKeys)
	})
}
