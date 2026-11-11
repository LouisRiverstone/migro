package orm

import (
	"context"
	"reflect"
	"slices"
	"sync"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/events"
	gschema "gorm.io/gorm/schema"
)

// Replicate returns an unsaved copy of v (Eloquent's replicate): the primary key, creation and update
// times, the soft-delete mark, loaded relations and the columns named in except are cleared. The copy is
// bound to the same database, so Save inserts it. before:replicate listeners may abort it.
func Replicate[T any](ctx context.Context, v *T, except ...string) (*T, error) {
	e, ok := any(v).(entity)
	if !ok {
		return nil, migro.Newf(migro.UnboundModel, "%T does not embed orm.BaseModel", v)
	}
	m, err := e.base().bound()
	if err != nil {
		return nil, err
	}
	sch, err := parseModel(m.db, v)
	if err != nil {
		return nil, err
	}
	cp := new(T)
	*cp = *v
	rv := reflect.ValueOf(cp).Elem()
	for _, i := range relationFields(rv.Type()) {
		rv.Field(i).SetZero()
	}
	soft := softDeleteField(sch)
	for _, f := range sch.Fields {
		if f.DBName == "" {
			continue
		}
		if f.PrimaryKey || f.AutoCreateTime > 0 || f.AutoUpdateTime > 0 || f == soft || slices.Contains(except, f.DBName) {
			f.ReflectValueOf(ctx, rv).Set(reflect.Zero(f.FieldType))
		}
	}
	bind(m.db, cp, false)
	cm := &any(cp).(entity).base().m
	cm.extras, cm.changes, cm.created = nil, nil, false
	if err := events.EmitModel(ctx, m.db, events.BeforeReplicate, sch.Table, cp); err != nil {
		return nil, err
	}
	return cp, nil
}

// Is reports whether a and b are the same row: the same model type and the same non-zero primary key
// (Eloquent's is). IsNot is its negation.
func Is(a, b any) bool {
	ra, rb := reflect.Indirect(reflect.ValueOf(a)), reflect.Indirect(reflect.ValueOf(b))
	if !ra.IsValid() || !rb.IsValid() || ra.Type() != rb.Type() || ra.Kind() != reflect.Struct {
		return false
	}
	sch, err := gschema.Parse(a, &sync.Map{}, gschema.NamingStrategy{})
	if err != nil || sch.PrioritizedPrimaryField == nil {
		return false
	}
	ka, za := sch.PrioritizedPrimaryField.ValueOf(context.Background(), ra)
	kb, zb := sch.PrioritizedPrimaryField.ValueOf(context.Background(), rb)
	return !za && !zb && reflect.DeepEqual(ka, kb)
}

func IsNot(a, b any) bool { return !Is(a, b) }
