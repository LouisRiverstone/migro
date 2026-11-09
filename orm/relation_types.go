package orm

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/LouisRiverstone/migro"
	gschema "gorm.io/gorm/schema"
)

// Relations are declared as struct fields, like Lucid's decorators. The `gorm:"-"` tag is required
// (GORM must not treat the field as a column) and the optional `orm:"..."` tag overrides the
// Laravel-style conventions with column names:
//
//	type User struct {
//	    orm.SoftDeleteModel
//	    CountryID uint
//	    Profile   orm.HasOne[Profile]        `gorm:"-"`
//	    Posts     orm.HasMany[Post]          `gorm:"-"`
//	    Country   orm.BelongsTo[Country]     `gorm:"-"`
//	    Roles     orm.BelongsToMany[Role]    `gorm:"-" orm:"pivotTable:role_user"`
//	    Comments  orm.MorphMany[Comment]     `gorm:"-" orm:"morph:commentable"`
//	}
//
// Tag keys: foreignKey, localKey, relatedKey, pivotTable, pivotForeignKey, pivotRelatedForeignKey,
// pivotColumns (comma list), pivotTimestamps, morph, throughForeignKey, throughKey.

type kind int

const (
	kHasOne kind = iota + 1
	kHasMany
	kBelongsTo
	kBelongsToMany
	kThrough
	kMorphOne
	kMorphMany
	kMorphTo
	kMorphToMany
	kMorphedByMany
)

func (k kind) single() bool {
	return k == kHasOne || k == kBelongsTo || k == kMorphOne || k == kMorphTo
}

func (k kind) hasPivot() bool { return k == kBelongsToMany || k == kMorphToMany || k == kMorphedByMany }

type relField interface {
	rm() *relMeta
	kind() kind
	modelType() reflect.Type
	throughType() reflect.Type
	// store replaces the field's content with rows ([]T, or []any for MorphTo).
	store(rows reflect.Value)
	// children returns pointers to the loaded related structs.
	children() []reflect.Value
}

type relMeta struct {
	owner  any
	db     *migro.DB
	name   string
	loaded bool
}

func (m *relMeta) rm() *relMeta              { return m }
func (m *relMeta) throughType() reflect.Type { return nil }
func (m *relMeta) ownerValue() reflect.Value { return reflect.ValueOf(m.owner).Elem() }
func (m *relMeta) unbound() error {
	if m.owner == nil || m.db == nil {
		return migro.Newf(migro.UnboundModel, "relation %s: the model is not bound to a database", m.name)
	}
	return nil
}

// Loaded reports whether the relation has been loaded (eagerly or lazily).
func (m *relMeta) Loaded() bool { return m.loaded }

// Load fetches the relation if it was not loaded yet.
func (m *relMeta) Load(ctx context.Context) error {
	if err := m.unbound(); err != nil {
		return err
	}
	if m.loaded {
		return nil
	}
	return loadPath(ctx, m.db, []reflect.Value{m.ownerValue()}, m.name, nil)
}

type many[T any] struct {
	relMeta
	items []T
}

// Items returns the loaded rows (empty until loaded; see Loaded).
func (m *many[T]) Items() []T { return m.items }

func (m *many[T]) modelType() reflect.Type { return reflect.TypeFor[T]() }
func (m *many[T]) store(rows reflect.Value) {
	m.items, _ = rows.Interface().([]T)
	m.loaded = true
}
func (m *many[T]) children() []reflect.Value {
	out := make([]reflect.Value, len(m.items))
	for i := range m.items {
		out[i] = reflect.ValueOf(&m.items[i])
	}
	return out
}

// Get returns the related rows, loading them on first use.
func (m *many[T]) Get(ctx context.Context) ([]T, error) {
	if err := m.Load(ctx); err != nil {
		return nil, err
	}
	return m.items, nil
}

// Query returns a query over the related table already constrained to this owner.
func (m *many[T]) Query() (*Query[T], error) { return relatedQuery[T](&m.relMeta) }

type one[T any] struct {
	relMeta
	item *T
}

// Value returns the loaded row, or nil.
func (o *one[T]) Value() *T { return o.item }

func (o *one[T]) modelType() reflect.Type { return reflect.TypeFor[T]() }
func (o *one[T]) store(rows reflect.Value) {
	o.item = nil
	if rows.Len() > 0 {
		o.item, _ = rows.Index(0).Addr().Interface().(*T)
	}
	o.loaded = true
}
func (o *one[T]) children() []reflect.Value {
	if o.item == nil {
		return nil
	}
	return []reflect.Value{reflect.ValueOf(o.item)}
}

// Get returns the related row (nil when there is none), loading it on first use.
func (o *one[T]) Get(ctx context.Context) (*T, error) {
	if err := o.Load(ctx); err != nil {
		return nil, err
	}
	return o.item, nil
}

// Query returns a query over the related table already constrained to this owner.
func (o *one[T]) Query() (*Query[T], error) { return relatedQuery[T](&o.relMeta) }

type (
	HasMany[T any]                 struct{ many[T] }
	HasOne[T any]                  struct{ one[T] }
	BelongsTo[T any]               struct{ one[T] }
	BelongsToMany[T any]           struct{ many[T] }
	MorphMany[T any]               struct{ many[T] }
	MorphOne[T any]                struct{ one[T] }
	MorphToMany[T any]             struct{ many[T] }
	MorphedByMany[T any]           struct{ many[T] }
	HasManyThrough[T, Through any] struct{ many[T] }
)

func (*HasMany[T]) kind() kind                                { return kHasMany }
func (*HasOne[T]) kind() kind                                 { return kHasOne }
func (*BelongsTo[T]) kind() kind                              { return kBelongsTo }
func (*BelongsToMany[T]) kind() kind                          { return kBelongsToMany }
func (*MorphMany[T]) kind() kind                              { return kMorphMany }
func (*MorphOne[T]) kind() kind                               { return kMorphOne }
func (*MorphToMany[T]) kind() kind                            { return kMorphToMany }
func (*MorphedByMany[T]) kind() kind                          { return kMorphedByMany }
func (*HasManyThrough[T, Through]) kind() kind                { return kThrough }
func (*HasManyThrough[T, Through]) throughType() reflect.Type { return reflect.TypeFor[Through]() }

// MorphTo is the inverse of MorphOne/MorphMany: the owner holds <name>_type and <name>_id, and the
// target is any model registered with MorphMap. Value is a pointer to that model.
type MorphTo struct {
	relMeta
	item any
}

func (*MorphTo) kind() kind              { return kMorphTo }
func (*MorphTo) modelType() reflect.Type { return nil }
func (m *MorphTo) Value() any            { return m.item }
func (m *MorphTo) store(rows reflect.Value) {
	m.item = nil
	if rows.Len() > 0 {
		m.item = rows.Index(0).Interface()
	}
	m.loaded = true
}
func (m *MorphTo) children() []reflect.Value {
	if m.item == nil {
		return nil
	}
	return []reflect.Value{reflect.ValueOf(m.item)}
}

// Get returns the target (nil when it no longer exists), loading it on first use.
func (m *MorphTo) Get(ctx context.Context) (any, error) {
	if err := m.Load(ctx); err != nil {
		return nil, err
	}
	return m.item, nil
}

// ---- binding ----------------------------------------------------------------------------------

var relFieldType = reflect.TypeFor[relField]()

var relIndexCache sync.Map // reflect.Type -> []int

// relationFields returns the indexes of a struct's relation fields.
func relationFields(t reflect.Type) []int {
	if v, ok := relIndexCache.Load(t); ok {
		return v.([]int)
	}
	var idx []int
	for i := range t.NumField() {
		sf := t.Field(i)
		if sf.IsExported() && reflect.PointerTo(sf.Type).Implements(relFieldType) {
			idx = append(idx, i)
		}
	}
	relIndexCache.Store(t, idx)
	return idx
}

func bindRelations(db *migro.DB, ptr reflect.Value) {
	st := ptr.Elem()
	for _, i := range relationFields(st.Type()) {
		m := st.Field(i).Addr().Interface().(relField).rm()
		m.owner, m.db, m.name = ptr.Interface(), db, st.Type().Field(i).Name
	}
}

// ---- conventions --------------------------------------------------------------------------------

var naming = gschema.NamingStrategy{}

func snake(s string) string { return naming.ColumnName("", s) }

type tagOpts map[string]string

func parseTag(tag string) tagOpts {
	out := tagOpts{}
	for _, part := range strings.Split(tag, ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), ":")
		if k != "" {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

func (o tagOpts) get(key, def string) string {
	if v := o[key]; v != "" {
		return v
	}
	return def
}

type pivotDef struct {
	table, ownerCol, relatedCol string
	typeCol, typeVal            string
	extra                       []string
	timestamps                  bool
}

// relDef is a relation resolved against the database schema.
type relDef struct {
	kind     kind
	name     string
	ownerT   reflect.Type
	relT     reflect.Type
	ownerSch *gschema.Schema
	relSch   *gschema.Schema

	// has*/morph*: fk is on the related table. belongsTo: fk is on the owner. lk is the key it points at.
	fk, lk string
	// many-to-many: rk is the related key the pivot points at.
	rk string

	typeCol, typeVal string // morph one/many/to: discriminator column (and alias the owner stores)
	pivot            pivotDef

	thSch       *gschema.Schema // through
	thFK, thKey string          // through.thFK -> owner.lk ; thKey is through's key
	relFK       string          // related.relFK -> through.thKey
}

func primaryKey(sch *gschema.Schema) (string, error) {
	if sch.PrioritizedPrimaryField == nil {
		return "", migro.Newf(migro.InvalidSchema, "model %s needs a primary key to be used in a relation", sch.Name)
	}
	return sch.PrioritizedPrimaryField.DBName, nil
}

func resolveByName(db *migro.DB, ownerT reflect.Type, name string) (*relDef, relField, error) {
	sf, ok := ownerT.FieldByName(name)
	if !ok {
		return nil, nil, migro.Newf(migro.InvalidSchema, "%s has no relation %q", ownerT.Name(), name)
	}
	rf, ok := reflect.New(sf.Type).Interface().(relField)
	if !ok {
		return nil, nil, migro.Newf(migro.InvalidSchema, "%s.%s is not a relation field", ownerT.Name(), name)
	}
	if !strings.Contains(sf.Tag.Get("gorm"), "-") {
		return nil, nil, migro.Newf(migro.InvalidSchema, "%s.%s needs the `gorm:\"-\"` tag", ownerT.Name(), name)
	}
	d, err := resolve(db, ownerT, sf, rf)
	return d, rf, err
}

func resolve(db *migro.DB, ownerT reflect.Type, sf reflect.StructField, rf relField) (*relDef, error) {
	opts := parseTag(sf.Tag.Get("orm"))
	d := &relDef{kind: rf.kind(), name: sf.Name, ownerT: ownerT, relT: rf.modelType()}
	var err error
	if d.ownerSch, err = parseModel(db, reflect.New(ownerT).Interface()); err != nil {
		return nil, err
	}
	// A composite-key model has no single primary key: that is fine until a relation needs one.
	ownerPK, _ := primaryKey(d.ownerSch)
	ownerSnake := snake(ownerT.Name())
	var relSnake, relPK string
	if d.relT != nil {
		if d.relSch, err = parseModel(db, reflect.New(d.relT).Interface()); err != nil {
			return nil, err
		}
		relPK, _ = primaryKey(d.relSch)
		relSnake = snake(d.relT.Name())
	}
	morph := func(def string) (string, error) {
		name := opts.get("morph", def)
		if name == "" {
			return "", migro.Newf(migro.InvalidSchema, "%s.%s needs orm:\"morph:<name>\"", ownerT.Name(), sf.Name)
		}
		return name, nil
	}

	switch d.kind {
	case kHasOne, kHasMany:
		d.lk, d.fk = opts.get("localKey", ownerPK), opts.get("foreignKey", ownerSnake+"_id")
	case kMorphOne, kMorphMany:
		name, err := morph("")
		if err != nil {
			return nil, err
		}
		d.lk, d.fk, d.typeCol, d.typeVal = opts.get("localKey", ownerPK), name+"_id", name+"_type", morphAlias(ownerT)
	case kBelongsTo:
		d.fk, d.lk = opts.get("foreignKey", snake(sf.Name)+"_id"), opts.get("relatedKey", relPK)
	case kBelongsToMany:
		names := []string{ownerSnake, relSnake}
		sort.Strings(names)
		d.lk, d.rk = opts.get("localKey", ownerPK), opts.get("relatedKey", relPK)
		d.pivot = pivotDef{
			table:      opts.get("pivotTable", strings.Join(names, "_")),
			ownerCol:   opts.get("pivotForeignKey", ownerSnake+"_id"),
			relatedCol: opts.get("pivotRelatedForeignKey", relSnake+"_id"),
		}
	case kMorphToMany:
		name, err := morph("")
		if err != nil {
			return nil, err
		}
		d.lk, d.rk = opts.get("localKey", ownerPK), opts.get("relatedKey", relPK)
		d.pivot = pivotDef{
			table: opts.get("pivotTable", name+"s"), ownerCol: opts.get("pivotForeignKey", name+"_id"),
			relatedCol: opts.get("pivotRelatedForeignKey", relSnake+"_id"),
			typeCol:    name + "_type", typeVal: morphAlias(ownerT),
		}
	case kMorphedByMany:
		name, err := morph("")
		if err != nil {
			return nil, err
		}
		d.lk, d.rk = opts.get("localKey", ownerPK), opts.get("relatedKey", relPK)
		d.pivot = pivotDef{
			table: opts.get("pivotTable", name+"s"), ownerCol: opts.get("pivotForeignKey", ownerSnake+"_id"),
			relatedCol: opts.get("pivotRelatedForeignKey", name+"_id"),
			typeCol:    name + "_type", typeVal: morphAlias(d.relT),
		}
	case kThrough:
		thT := rf.throughType()
		if d.thSch, err = parseModel(db, reflect.New(thT).Interface()); err != nil {
			return nil, err
		}
		thPK, _ := primaryKey(d.thSch)
		d.lk, d.thFK = opts.get("localKey", ownerPK), opts.get("foreignKey", ownerSnake+"_id")
		d.thKey, d.relFK = opts.get("throughKey", thPK), opts.get("throughForeignKey", snake(thT.Name())+"_id")
	case kMorphTo:
		name, err := morph(snake(sf.Name))
		if err != nil {
			return nil, err
		}
		d.fk, d.typeCol = name+"_id", name+"_type"
	default:
		return nil, migro.Newf(migro.InvalidSchema, "unknown relation kind on %s.%s", ownerT.Name(), sf.Name)
	}
	for _, req := range d.required() {
		if req.col == "" {
			return nil, migro.Newf(migro.InvalidSchema, "relation %s.%s: %s needs a single primary key (or an explicit orm:\"%s:...\" tag)", ownerT.Name(), sf.Name, req.owner, req.tag)
		}
	}
	if d.kind.hasPivot() {
		if cols := opts["pivotColumns"]; cols != "" {
			d.pivot.extra = strings.Split(cols, ",")
		}
		d.pivot.timestamps = opts["pivotTimestamps"] == "true"
	}
	return d, nil
}

type requirement struct{ col, owner, tag string }

// required lists the key columns a kind cannot work without.
func (d *relDef) required() []requirement {
	own, rel := d.ownerT.Name(), ""
	if d.relT != nil {
		rel = d.relT.Name()
	}
	switch d.kind {
	case kHasOne, kHasMany, kMorphOne, kMorphMany, kThrough:
		return []requirement{{d.lk, own, "localKey"}}
	case kBelongsTo:
		return []requirement{{d.lk, rel, "relatedKey"}}
	case kBelongsToMany, kMorphToMany, kMorphedByMany:
		return []requirement{{d.lk, own, "localKey"}, {d.rk, rel, "relatedKey"}}
	}
	return nil
}

// ---- morph map ----------------------------------------------------------------------------------

var morphMap = struct {
	sync.RWMutex
	byAlias map[string]reflect.Type
	byType  map[reflect.Type]string
}{byAlias: map[string]reflect.Type{}, byType: map[reflect.Type]string{}}

// MorphMap registers the aliases stored in *_type columns (Laravel's Relation::morphMap):
//
//	orm.MorphMap(map[string]any{"post": Post{}, "video": Video{}})
//
// Models missing from the map use their snake_case type name as alias.
func MorphMap(m map[string]any) {
	morphMap.Lock()
	defer morphMap.Unlock()
	for alias, model := range m {
		t := reflect.TypeOf(model)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		morphMap.byAlias[alias], morphMap.byType[t] = t, alias
	}
}

func morphAlias(t reflect.Type) string {
	morphMap.RLock()
	defer morphMap.RUnlock()
	if a, ok := morphMap.byType[t]; ok {
		return a
	}
	return snake(t.Name())
}

func morphModel(alias string) (reflect.Type, bool) {
	morphMap.RLock()
	defer morphMap.RUnlock()
	t, ok := morphMap.byAlias[alias]
	return t, ok
}
