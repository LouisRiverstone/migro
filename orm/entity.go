package orm

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"time"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
)

// Save inserts a new model, or updates only the columns that changed since it was loaded
// (Eloquent's save). A model with no changes issues no query.
func (b *BaseModel) Save(ctx context.Context) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	g := m.db.Gorm.WithContext(ctx)
	if !m.exists {
		if err := wrap(g.Create(m.self).Error, "save"); err != nil {
			return err
		}
		m.exists, m.orig = true, snapshot(m.db, m.self)
		return nil
	}
	cols := m.dirtyColumns()
	if len(cols) == 0 {
		return nil
	}
	if sch, err := parseModel(m.db, m.self); err == nil {
		for _, f := range sch.Fields {
			if f.AutoUpdateTime > 0 && !slices.Contains(cols, f.DBName) {
				cols = append(cols, f.DBName)
			}
		}
	}
	if err := wrap(g.Model(m.self).Select(cols).Updates(m.self).Error, "save"); err != nil {
		return err
	}
	m.orig = snapshot(m.db, m.self)
	return nil
}

// Update fills attrs (honouring Fillable/Guarded) and saves.
func (b *BaseModel) Update(ctx context.Context, attrs map[string]any) error {
	if err := b.Fill(attrs); err != nil {
		return err
	}
	return b.Save(ctx)
}

// Delete removes the row, or marks it deleted when the model has a gorm.DeletedAt column.
func (b *BaseModel) Delete(ctx context.Context) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	if err := wrap(m.db.Gorm.WithContext(ctx).Delete(m.self).Error, "delete"); err != nil {
		return err
	}
	sch, err := parseModel(m.db, m.self)
	if err != nil {
		return err
	}
	f := softDeleteField(sch)
	if f == nil {
		m.exists = false
		return nil
	}
	_ = f.Set(ctx, reflect.ValueOf(m.self).Elem(), time.Now()) // mirror the mark on the struct
	return nil
}

// ForceDelete removes the row for good, soft deletes included.
func (b *BaseModel) ForceDelete(ctx context.Context) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	if err := wrap(m.db.Gorm.WithContext(ctx).Unscoped().Delete(m.self).Error, "force delete"); err != nil {
		return err
	}
	m.exists = false
	return nil
}

// Restore clears the soft-delete mark.
func (b *BaseModel) Restore(ctx context.Context) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	sch, err := parseModel(m.db, m.self)
	if err != nil {
		return err
	}
	f := softDeleteField(sch)
	if f == nil {
		return migro.Newf(migro.InvalidSchema, "%s has no soft-delete column", sch.Name)
	}
	if err := wrap(m.db.Gorm.WithContext(ctx).Unscoped().Model(m.self).Update(f.DBName, nil).Error, "restore"); err != nil {
		return err
	}
	f.ReflectValueOf(ctx, reflect.ValueOf(m.self).Elem()).Set(reflect.Zero(f.FieldType))
	m.orig = snapshot(m.db, m.self)
	return nil
}

// Trashed reports whether the model is soft deleted.
func (b *BaseModel) Trashed() bool {
	m, err := b.bound()
	if err != nil {
		return false
	}
	sch, err := parseModel(m.db, m.self)
	if err != nil {
		return false
	}
	f := softDeleteField(sch)
	if f == nil {
		return false
	}
	_, zero := f.ValueOf(context.Background(), reflect.ValueOf(m.self).Elem())
	return !zero
}

// Refresh reloads the model's columns from the database.
func (b *BaseModel) Refresh(ctx context.Context) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	if err := wrap(m.db.Gorm.WithContext(ctx).Unscoped().First(m.self).Error, "refresh"); err != nil {
		return err
	}
	m.exists, m.orig = true, snapshot(m.db, m.self)
	return nil
}

// Fresh returns a newly loaded copy of v, leaving v untouched.
func Fresh[T any](ctx context.Context, v *T) (*T, error) {
	e, ok := any(v).(entity)
	if !ok {
		return nil, migro.Newf(migro.UnboundModel, "%T does not embed orm.BaseModel", v)
	}
	m, err := e.base().bound()
	if err != nil {
		return nil, err
	}
	out := new(T)
	sch, err := parseModel(m.db, v)
	if err != nil {
		return nil, err
	}
	pk, _ := sch.PrioritizedPrimaryField.ValueOf(ctx, reflect.ValueOf(v).Elem())
	if err := wrap(m.db.Gorm.WithContext(ctx).Unscoped().First(out, pk).Error, "fresh"); err != nil {
		return nil, err
	}
	bind(m.db, out, true)
	return out, nil
}

// Touch bumps updated_at without touching other columns.
func (b *BaseModel) Touch(ctx context.Context) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	b.UpdatedAt = time.Now()
	if err := wrap(m.db.Gorm.WithContext(ctx).Model(m.self).UpdateColumn("updated_at", b.UpdatedAt).Error, "touch"); err != nil {
		return err
	}
	m.orig = snapshot(m.db, m.self)
	return nil
}

// Increment adds n to a numeric column atomically in SQL, then reloads the model.
func (b *BaseModel) Increment(ctx context.Context, column string, n int) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	expr := gorm.Expr(m.db.Dialect.Quote(column)+" + ?", n)
	if err := wrap(m.db.Gorm.WithContext(ctx).Model(m.self).UpdateColumn(column, expr).Error, "increment"); err != nil {
		return err
	}
	return b.Refresh(ctx)
}

// Decrement is Increment with a negative amount.
func (b *BaseModel) Decrement(ctx context.Context, column string, n int) error {
	return b.Increment(ctx, column, -n)
}

// Exists reports whether the model has been persisted.
func (b *BaseModel) Exists() bool { return b.m.exists }

// ---- dirty tracking ---------------------------------------------------------------------

func (m *meta) dirtyColumns() []string {
	cur := snapshot(m.db, m.self)
	var out []string
	for col, v := range cur {
		if old, ok := m.orig[col]; !ok || !reflect.DeepEqual(old, v) {
			out = append(out, col)
		}
	}
	slices.Sort(out)
	return out
}

// IsDirty reports whether any (or, when given, any of the named) columns changed since load/save.
func (b *BaseModel) IsDirty(columns ...string) bool {
	m, err := b.bound()
	if err != nil {
		return false
	}
	dirty := m.dirtyColumns()
	if len(columns) == 0 {
		return len(dirty) > 0
	}
	for _, c := range columns {
		if slices.Contains(dirty, c) {
			return true
		}
	}
	return false
}

// IsClean is the inverse of IsDirty.
func (b *BaseModel) IsClean(columns ...string) bool { return !b.IsDirty(columns...) }

// Dirty returns the changed columns with their current values.
func (b *BaseModel) Dirty() map[string]any {
	m, err := b.bound()
	if err != nil {
		return nil
	}
	cur := snapshot(m.db, m.self)
	out := map[string]any{}
	for _, c := range m.dirtyColumns() {
		out[c] = cur[c]
	}
	return out
}

// GetOriginal returns the value a column had when the model was loaded or last saved.
func (b *BaseModel) GetOriginal(column string) any { return b.m.orig[column] }

// Extras holds computed values attached by queries: "<relation>_count" from WithCount and
// "pivot_<column>" on models loaded through a many-to-many relation (Lucid's $extras).
func (b *BaseModel) Extras() map[string]any { return maps.Clone(b.m.extras) }

// Extra returns one extra value.
func (b *BaseModel) Extra(key string) any { return b.m.extras[key] }

func setExtra(v any, key string, val any) {
	e, ok := v.(entity)
	if !ok {
		return
	}
	m := &e.base().m
	if m.extras == nil {
		m.extras = map[string]any{}
	}
	m.extras[key] = val
}
