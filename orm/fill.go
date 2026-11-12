package orm

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"time"

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
	// Defaults returns the values a model starts with when built through Model.Make or CreateFrom
	// (Eloquent's $attributes). A struct literal is never touched: Go cannot tell a field left at its zero
	// value from one set to it.
	Defaults interface{ Defaults() map[string]any }
	// SerializeAs renames columns and relations in ToMap / JSON: {"created_at": "createdAt"} (Lucid's serializeAs).
	SerializeAs interface{ SerializeAs() map[string]string }
	// Appends returns computed values added to ToMap / JSON (Eloquent's $appends).
	Appends interface{ Appends() map[string]any }
)

// Fill sets columns (by column or field name) from attrs. Fields outside Fillable, or inside Guarded,
// fail with MASS_ASSIGNMENT instead of being silently dropped. Models implementing neither are open.
func (b *Record) Fill(attrs map[string]any) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	return fill(context.Background(), m.db, m.self, attrs)
}

// SetAttribute sets one column through its cast (a mutator), ignoring Fillable / Guarded: it is the
// programmatic counterpart of `$model->column = value`.
func (b *Record) SetAttribute(column string, v any) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	return fillWith(context.Background(), m.db, m.self, map[string]any{column: v}, false)
}

// GetAttribute returns one column through its cast (an accessor).
func (b *Record) GetAttribute(column string) (any, error) {
	m, err := b.bound()
	if err != nil {
		return nil, err
	}
	sch, err := parseModel(m.db, m.self)
	if err != nil {
		return nil, err
	}
	f := sch.LookUpField(column)
	if f == nil || f.DBName == "" {
		return nil, migro.Newf(migro.InvalidSchema, "%s has no column %q", sch.Name, column)
	}
	val, _ := f.ValueOf(context.Background(), reflect.ValueOf(m.self).Elem())
	if c := castFor(m.self, f.DBName); c != nil {
		out, err := c.Get(val)
		return out, castErr(f.DBName, err)
	}
	return val, nil
}

func fill(ctx context.Context, db *migro.DB, self any, attrs map[string]any) error {
	return fillWith(ctx, db, self, attrs, true)
}

// fillWith applies casts; guard=false skips the Fillable / Guarded check.
func fillWith(ctx context.Context, db *migro.DB, self any, attrs map[string]any, guard bool) error {
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
		if guard && !fillAllowed(self, f.DBName) {
			return migro.Newf(migro.MassAssignment, "column %q of %s is not fillable", f.DBName, sch.Name)
		}
		if c := castFor(self, f.DBName); c != nil {
			cv, err := c.Set(val)
			if err != nil {
				return castErr(f.DBName, err)
			}
			val = cv
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

// SerializeOpt adjusts one ToMapWith / JSONWith call.
type SerializeOpt func(*serializeCfg)

type serializeCfg struct {
	visible, hidden []string
	dateLayout      string
}

// MakeVisible shows columns the model hides, for this call only (Eloquent's makeVisible).
func MakeVisible(cols ...string) SerializeOpt {
	return func(c *serializeCfg) { c.visible = append(c.visible, cols...) }
}

// MakeHidden hides extra columns for this call only (Eloquent's makeHidden). It wins over MakeVisible.
func MakeHidden(cols ...string) SerializeOpt {
	return func(c *serializeCfg) { c.hidden = append(c.hidden, cols...) }
}

// DateFormat renders time.Time values with a Go layout, nested relations included.
func DateFormat(layout string) SerializeOpt {
	return func(c *serializeCfg) { c.dateLayout = layout }
}

// ToMap serializes a bound model the way Eloquent's toArray does: column names as keys, casts'
// Get applied, Hidden / Visible applied, Appends added, loaded relations nested, extras (counts,
// pivot) included.
func ToMap(v any) (map[string]any, error) { return ToMapWith(v) }

// ToMapWith is ToMap with per-call options.
func ToMapWith(v any, opts ...SerializeOpt) (map[string]any, error) {
	cfg := &serializeCfg{}
	for _, o := range opts {
		o(cfg)
	}
	return toMap(v, cfg)
}

func toMap(v any, cfg *serializeCfg) (map[string]any, error) {
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
		if f.DBName == "" || !visibleColumn(m.self, f.DBName, cfg) {
			continue
		}
		val, _ := f.ValueOf(context.Background(), rv)
		if c := castFor(m.self, f.DBName); c != nil {
			if val, err = c.Get(val); err != nil {
				return nil, castErr(f.DBName, err)
			}
		}
		out[f.DBName] = serializeValue(val, cfg)
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
			sub, err := toMap(c.Interface(), &serializeCfg{dateLayout: cfg.dateLayout})
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
	if r, ok := m.self.(SerializeAs); ok {
		for from, to := range r.SerializeAs() {
			if val, has := out[from]; has && from != to {
				delete(out, from)
				out[to] = val
			}
		}
	}
	return out, nil
}

// jsonData lets JSONColumn[T] columns serialize as their nested value instead of a wrapper struct.
type jsonData interface{ jsonData() any }

func (j JSONColumn[T]) jsonData() any { return j.Data }

func serializeValue(val any, cfg *serializeCfg) any {
	if d, ok := val.(jsonData); ok {
		return d.jsonData()
	}
	if cfg.dateLayout == "" {
		return val
	}
	switch t := val.(type) {
	case time.Time:
		return t.Format(cfg.dateLayout)
	case *time.Time:
		if t == nil {
			return nil
		}
		return t.Format(cfg.dateLayout)
	}
	return val
}

func visibleColumn(self any, col string, cfg *serializeCfg) bool {
	if slices.Contains(cfg.hidden, col) {
		return false
	}
	if slices.Contains(cfg.visible, col) {
		return true
	}
	if vis, ok := self.(Visible); ok {
		return slices.Contains(vis.Visible(), col)
	}
	if h, ok := self.(Hidden); ok {
		return !slices.Contains(h.Hidden(), col)
	}
	return true
}

// JSON is json.Marshal over ToMap.
func JSON(v any) ([]byte, error) { return JSONWith(v) }

// JSONWith is JSON with per-call options.
func JSONWith(v any, opts ...SerializeOpt) ([]byte, error) {
	m, err := ToMapWith(v, opts...)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, migro.Wrap(migro.QueryFailed, err, "marshal")
	}
	return b, nil
}
