package orm

import (
	"reflect"
	"slices"

	"github.com/LouisRiverstone/migro"
)

// ColumnInfo describes one column of a model.
type ColumnInfo struct {
	Name          string
	GoType        string
	DBType        string
	PrimaryKey    bool
	AutoIncrement bool
	NotNull       bool
	Unique        bool
	HasDefault    bool
}

// RelationInfo describes one relation of a model.
type RelationInfo struct {
	Name    string
	Kind    string // has_one, has_many, belongs_to, belongs_to_many, ...
	Related string // the related model, "(any)" for morph-to
	// Keys spells out how the relation joins, e.g. "posts.user_id -> users.id".
	Keys string
}

// ModelInfo is what Describe reports (the model:show command prints it).
type ModelInfo struct {
	Name        string
	Table       string
	SoftDeletes bool
	Columns     []ColumnInfo
	Relations   []RelationInfo
	// Traits lists the optional behaviours the model opts into: Fillable, Hidden, Touches, ...
	Traits []string
}

var kindNames = map[kind]string{
	kHasOne: "has_one", kHasMany: "has_many", kBelongsTo: "belongs_to", kBelongsToMany: "belongs_to_many",
	kThrough: "has_through", kThroughOne: "has_one_through", kMorphOne: "morph_one", kMorphMany: "morph_many",
	kMorphTo: "morph_to", kMorphToMany: "morph_to_many", kMorphedByMany: "morphed_by_many",
}

func (k kind) String() string { return kindNames[k] }

// Describe reports a model's table, columns, relations and optional behaviours.
func Describe(db *migro.DB, model any) (ModelInfo, error) {
	t := reflect.TypeOf(model)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	ptr := reflect.New(t).Interface()
	sch, err := parseModel(db, ptr)
	if err != nil {
		return ModelInfo{}, err
	}
	info := ModelInfo{Name: t.Name(), Table: sch.Table, SoftDeletes: softDeleteField(sch) != nil}
	for _, f := range sch.Fields {
		if f.DBName == "" {
			continue
		}
		info.Columns = append(info.Columns, ColumnInfo{
			Name: f.DBName, GoType: f.FieldType.String(), DBType: string(f.DataType), PrimaryKey: f.PrimaryKey,
			AutoIncrement: f.AutoIncrement, NotNull: f.NotNull, Unique: f.Unique, HasDefault: f.HasDefaultValue,
		})
	}
	for _, i := range relationFields(t) {
		sf := t.Field(i)
		d, _, err := resolveByName(db, t, sf.Name)
		if err != nil {
			return ModelInfo{}, err
		}
		info.Relations = append(info.Relations, d.info(sf.Name))
	}
	traits := []struct {
		name string
		has  bool
	}{
		{"Fillable", implements[Fillable](ptr)}, {"Guarded", implements[Guarded](ptr)}, {"Hidden", implements[Hidden](ptr)},
		{"Visible", implements[Visible](ptr)}, {"Appends", implements[Appends](ptr)}, {"Defaults", implements[Defaults](ptr)},
		{"SerializeAs", implements[SerializeAs](ptr)}, {"Touches", implements[Touches](ptr)}, {"Connected", implements[Connected](ptr)},
		{"GlobalScoped", implements[GlobalScoped](ptr)},
	}
	for _, tr := range traits {
		if tr.has {
			info.Traits = append(info.Traits, tr.name)
		}
	}
	slices.Sort(info.Traits)
	return info, nil
}

func implements[I any](v any) bool { _, ok := v.(I); return ok }

func (d *relDef) info(name string) RelationInfo {
	r := RelationInfo{Name: name, Kind: d.kind.String(), Related: "(any)"}
	if d.relT != nil {
		r.Related = d.relT.Name()
	}
	owner, rel := d.ownerSch.Table, ""
	if d.relSch != nil {
		rel = d.relSch.Table
	}
	switch d.kind {
	case kHasOne, kHasMany, kMorphOne, kMorphMany:
		r.Keys = rel + "." + d.fk + " -> " + owner + "." + d.lk
	case kBelongsTo:
		r.Keys = owner + "." + d.fk + " -> " + rel + "." + d.lk
	case kBelongsToMany, kMorphToMany, kMorphedByMany:
		r.Keys = owner + "." + d.lk + " <- " + d.pivot.table + "(" + d.pivot.ownerCol + ", " + d.pivot.relatedCol + ") -> " + rel + "." + d.rk
	case kThrough:
		r.Keys = rel + "." + d.relFK + " -> " + d.thSch.Table + "." + d.thKey + "; " + d.thSch.Table + "." + d.thFK + " -> " + owner + "." + d.lk
	case kMorphTo:
		r.Keys = owner + "." + d.typeCol + ", " + owner + "." + d.fk
	}
	return r
}
