package orm

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"

	"github.com/LouisRiverstone/migro"
)

// Optional interfaces a model can implement to control mass assignment and serialization.
type (
	// Fillable lists the only columns Fill / CreateFrom may set (Eloquent's $fillable).
	Fillable interface{ Fillable() []string }
	// Guarded lists columns Fill may not set (Eloquent's $guarded). Ignored when Fillable exists.
	Guarded interface{ Guarded() []string }
	// Hidden lists columns left out of ToMap / JSON (Eloquent's $hidden).
	Hidden interface{ Hidden() []string }
	// Visible, when present, is the only set of columns ToMap / JSON includes ($visible).
	Visible interface{ Visible() []string }
	// Appends returns computed values added to ToMap / JSON (Eloquent's $appends).
	Appends interface{ Appends() map[string]any }
)

// Fill sets columns (by column or field name) from attrs. Fields outside Fillable, or inside Guarded,
// fail with MASS_ASSIGNMENT instead of being silently dropped. Models implementing neither are open.
func (b *BaseModel) Fill(attrs map[string]any) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	return fill(context.Background(), m.db, m.self, attrs)
}

func fill(ctx context.Context, db *migro.DB, self any, attrs map[string]any) error {
	sch, err := parseModel(db, self)
	if err != nil {
		return err
	}
	rv := reflect.ValueOf(self).Elem()
	for key, val := range attrs {
		f := sch.LookUpField(key)
		if f == nil || f.DBName == "" {
			return migro.Newf(migro.MassAssignment, "%s has no column %q", sch.Name, key)
		}
		if !fillAllowed(self, f.DBName) {
			return migro.Newf(migro.MassAssignment, "column %q of %s is not fillable", f.DBName, sch.Name)
		}
		if err := f.Set(ctx, rv, val); err != nil {
			return migro.Wrap(migro.MassAssignment, err, "set %s.%s", sch.Name, f.DBName)
		}
	}
	return nil
}

func fillAllowed(self any, col string) bool {
	if f, ok := self.(Fillable); ok {
		return slices.Contains(f.Fillable(), col)
	}
	if g, ok := self.(Guarded); ok {
		return !slices.Contains(g.Guarded(), col)
	}
	return true
}

// ToMap serializes a bound model the way Eloquent's toArray does: column names as keys, Hidden /
// Visible applied, Appends added, loaded relations nested, extras (counts, pivot) included.
func ToMap(v any) (map[string]any, error) {
	e, ok := v.(entity)
	if !ok {
		return nil, migro.Newf(migro.UnboundModel, "%T does not embed orm.BaseModel", v)
	}
	m, err := e.base().bound()
	if err != nil {
		return nil, err
	}
	sch, err := parseModel(m.db, m.self)
	if err != nil {
		return nil, err
	}
	rv := reflect.ValueOf(m.self).Elem()
	out := map[string]any{}
	for _, f := range sch.Fields {
		if f.DBName == "" || !visibleColumn(m.self, f.DBName) {
			continue
		}
		val, _ := f.ValueOf(context.Background(), rv)
		out[f.DBName] = val
	}
	for k, val := range m.extras {
		out[k] = val
	}
	if a, ok := m.self.(Appends); ok {
		for k, val := range a.Appends() {
			out[k] = val
		}
	}
	for _, i := range relationFields(rv.Type()) {
		rf := rv.Field(i).Addr().Interface().(relField)
		if !rf.rm().loaded {
			continue
		}
		key := snake(rv.Type().Field(i).Name)
		kids := make([]any, 0)
		for _, c := range rf.children() {
			sub, err := ToMap(c.Interface())
			if err != nil {
				return nil, err
			}
			kids = append(kids, sub)
		}
		if rf.kind().single() {
			if len(kids) == 0 {
				out[key] = nil
				continue
			}
			out[key] = kids[0]
			continue
		}
		out[key] = kids
	}
	return out, nil
}

func visibleColumn(self any, col string) bool {
	if vis, ok := self.(Visible); ok {
		return slices.Contains(vis.Visible(), col)
	}
	if h, ok := self.(Hidden); ok {
		return !slices.Contains(h.Hidden(), col)
	}
	return true
}

// JSON is json.Marshal over ToMap.
func JSON(v any) ([]byte, error) {
	m, err := ToMap(v)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, migro.Wrap(migro.QueryFailed, err, "marshal")
	}
	return b, nil
}
