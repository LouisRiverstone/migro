package orm

import (
	"context"
	"reflect"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
	gschema "gorm.io/gorm/schema"
)

// meta is the per-instance state that makes a model "active record"-like: the database it came from,
// a pointer to the full struct (an embedded BaseModel cannot see its outer type), and a snapshot of
// the column values at load time for dirty tracking.
type meta struct {
	db     *migro.DB
	self   any
	orig   map[string]any
	exists bool
	extras map[string]any
}

type entity interface{ base() *BaseModel }

func (b *BaseModel) base() *BaseModel { return b }

// Bind attaches v (a struct pointer) to db so its instance methods and relations work. A model with
// a non-zero primary key is treated as already persisted.
func Bind[T any](db *migro.DB, v *T) *T {
	bind(db, v, !pkIsZero(db, v))
	return v
}

func bind(db *migro.DB, v any, exists bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return
	}
	if e, ok := v.(entity); ok {
		m := &e.base().m
		m.db, m.self, m.exists = db, v, exists
		m.orig = nil
		if exists {
			m.orig = snapshot(db, v)
		}
	}
	bindRelations(db, rv)
}

func bindAll[T any](db *migro.DB, rows []T, exists bool) {
	for i := range rows {
		bind(db, &rows[i], exists)
	}
}

func (b *BaseModel) bound() (*meta, error) {
	if b.m.db == nil || b.m.self == nil {
		return nil, migro.Newf(migro.UnboundModel, "model is not bound to a database: load it through orm.New[T](db) or call orm.Bind")
	}
	return &b.m, nil
}

func snapshot(db *migro.DB, v any) map[string]any {
	sch, err := parseModel(db, v)
	if err != nil {
		return nil
	}
	rv := reflect.ValueOf(v).Elem()
	out := make(map[string]any, len(sch.Fields))
	for _, f := range sch.Fields {
		if f.DBName == "" {
			continue
		}
		val, _ := f.ValueOf(context.Background(), rv)
		out[f.DBName] = detach(val)
	}
	return out
}

// detach copies pointer targets so a later in-place write through a *T field cannot alter the snapshot.
func detach(v any) any {
	rv := reflect.ValueOf(v)
	if v == nil || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return v
	}
	cp := reflect.New(rv.Elem().Type())
	cp.Elem().Set(rv.Elem())
	return cp.Interface()
}

func pkIsZero(db *migro.DB, v any) bool {
	sch, err := parseModel(db, v)
	if err != nil || sch.PrioritizedPrimaryField == nil {
		return true
	}
	_, zero := sch.PrioritizedPrimaryField.ValueOf(context.Background(), reflect.ValueOf(v).Elem())
	return zero
}

func softDeleteField(sch *gschema.Schema) *gschema.Field {
	for _, f := range sch.Fields {
		if f.FieldType == reflect.TypeFor[gorm.DeletedAt]() {
			return f
		}
	}
	return nil
}
