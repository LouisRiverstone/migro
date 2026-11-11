package orm

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/events"
	"gorm.io/gorm"
)

// Save inserts a new model, or updates only the columns that changed since it was loaded
// (Eloquent's save). A model with no changes issues no query.
func (b *Record) Save(ctx context.Context) error {
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
		m.changes, m.created = maps.Clone(m.orig), true
		return touchParents(ctx, m.db, m.self)
	}
	cols := m.dirtyColumns()
	if len(cols) == 0 {
		m.changes, m.created = map[string]any{}, false
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
	m.changes, m.created = map[string]any{}, false
	for _, c := range cols {
		m.changes[c] = m.orig[c]
	}
	return touchParents(ctx, m.db, m.self)
}

// Update fills attrs (honouring Fillable/Guarded) and saves.
func (b *Record) Update(ctx context.Context, attrs map[string]any) error {
	if err := b.Fill(attrs); err != nil {
		return err
	}
	return b.Save(ctx)
}

// Delete removes the row, or marks it deleted when the model has a gorm.DeletedAt column.
func (b *Record) Delete(ctx context.Context) error {
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
		return touchParents(ctx, m.db, m.self)
	}
	_ = f.Set(ctx, reflect.ValueOf(m.self).Elem(), time.Now()) // mirror the mark on the struct
	return touchParents(ctx, m.db, m.self)
}

// ForceDelete removes the row for good, soft deletes included.
func (b *Record) ForceDelete(ctx context.Context) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	table := tableOf(m)
	if err := events.EmitModel(ctx, m.db, events.BeforeForceDelete, table, m.self); err != nil {
		return err
	}
	if err := wrap(m.db.Gorm.WithContext(ctx).Unscoped().Delete(m.self).Error, "force delete"); err != nil {
		return err
	}
	m.exists = false
	if err := events.EmitModel(ctx, m.db, events.AfterForceDelete, table, m.self); err != nil {
		return err
	}
	return touchParents(ctx, m.db, m.self)
}

// Restore clears the soft-delete mark.
func (b *Record) Restore(ctx context.Context) error {
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
	if err := events.EmitModel(ctx, m.db, events.BeforeRestore, sch.Table, m.self); err != nil {
		return err
	}
	if err := wrap(m.db.Gorm.WithContext(ctx).Unscoped().Model(m.self).Update(f.DBName, nil).Error, "restore"); err != nil {
		return err
	}
	f.ReflectValueOf(ctx, reflect.ValueOf(m.self).Elem()).Set(reflect.Zero(f.FieldType))
	m.orig = snapshot(m.db, m.self)
	if err := events.EmitModel(ctx, m.db, events.AfterRestore, sch.Table, m.self); err != nil {
		return err
	}
	return touchParents(ctx, m.db, m.self)
}

// Trashed reports whether the model is soft deleted.
func (b *Record) Trashed() bool {
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
func (b *Record) Refresh(ctx context.Context) error {
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
func (b *Record) Touch(ctx context.Context) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	now := time.Now()
	if err := wrap(m.db.Gorm.WithContext(ctx).Model(m.self).UpdateColumn("updated_at", now).Error, "touch"); err != nil {
		return err
	}
	if sch, err := parseModel(m.db, m.self); err == nil {
		if f := sch.LookUpField("updated_at"); f != nil {
			_ = f.Set(ctx, reflect.ValueOf(m.self).Elem(), now) // mirror on the struct
		}
	}
	m.orig = snapshot(m.db, m.self)
	return nil
}

// Increment adds n to a numeric column atomically in SQL, then reloads the model.
func (b *Record) Increment(ctx context.Context, column string, n int) error {
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
func (b *Record) Decrement(ctx context.Context, column string, n int) error {
	return b.Increment(ctx, column, -n)
}

// Exists reports whether the model has been persisted.
func (b *Record) Exists() bool { return b.m.exists }

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
func (b *Record) IsDirty(columns ...string) bool {
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
func (b *Record) IsClean(columns ...string) bool { return !b.IsDirty(columns...) }

// Dirty returns the changed columns with their current values.
func (b *Record) Dirty() map[string]any {
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
func (b *Record) GetOriginal(column string) any { return b.m.orig[column] }

// Extras holds computed values attached by queries: "<relation>_count" from WithCount and
// "pivot_<column>" on models loaded through a many-to-many relation (Lucid's $extras).
func (b *Record) Extras() map[string]any { return maps.Clone(b.m.extras) }

// Extra returns one extra value.
func (b *Record) Extra(key string) any { return b.m.extras[key] }

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

// tableOf is the model's table name, or "" when its schema cannot be parsed.
func tableOf(m *meta) string {
	sch, err := parseModel(m.db, m.self)
	if err != nil {
		return ""
	}
	return sch.Table
}

// WasChanged reports whether the last Save wrote any (or, when given, any of the named) columns. A
// Save that inserted the row counts every column as changed.
func (b *Record) WasChanged(columns ...string) bool {
	if len(columns) == 0 {
		return len(b.m.changes) > 0
	}
	for _, c := range columns {
		if _, ok := b.m.changes[c]; ok {
			return true
		}
	}
	return false
}

// GetChanges returns the columns the last Save wrote, with their new values.
func (b *Record) GetChanges() map[string]any { return maps.Clone(b.m.changes) }

// WasRecentlyCreated reports whether the last Save inserted the row.
func (b *Record) WasRecentlyCreated() bool { return b.m.created }

// SaveQuietly is Save without model events (Eloquent's saveQuietly).
func (b *Record) SaveQuietly(ctx context.Context) error { return b.Save(events.WithoutEvents(ctx)) }

// DeleteQuietly is Delete without model events.
func (b *Record) DeleteQuietly(ctx context.Context) error {
	return b.Delete(events.WithoutEvents(ctx))
}

// IncrementEach adds a different amount to several columns in one atomic UPDATE, then reloads the model.
func (b *Record) IncrementEach(ctx context.Context, amounts map[string]int) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	if len(amounts) == 0 {
		return nil
	}
	set := make(map[string]any, len(amounts))
	for col, n := range amounts {
		set[col] = gorm.Expr(m.db.Dialect.Quote(col)+" + ?", n)
	}
	if err := wrap(m.db.Gorm.WithContext(ctx).Model(m.self).UpdateColumns(set).Error, "increment each"); err != nil {
		return err
	}
	return b.Refresh(ctx)
}

// SaveWithoutTimestamps saves without touching updated_at, and without events or hooks
// (Eloquent's withoutTimestamps). A new row is inserted with the timestamps it already carries.
func (b *Record) SaveWithoutTimestamps(ctx context.Context) error {
	m, err := b.bound()
	if err != nil {
		return err
	}
	g := m.db.Gorm.WithContext(ctx).Session(&gorm.Session{SkipHooks: true})
	if !m.exists {
		if err := wrap(g.Create(m.self).Error, "save without timestamps"); err != nil {
			return err
		}
		m.exists, m.orig = true, snapshot(m.db, m.self)
		return nil
	}
	dirty := m.dirtyColumns()
	if len(dirty) == 0 {
		return nil
	}
	set := make(map[string]any, len(dirty))
	cur := snapshot(m.db, m.self)
	for _, c := range dirty {
		set[c] = cur[c]
	}
	if err := wrap(g.Model(m.self).UpdateColumns(set).Error, "save without timestamps"); err != nil {
		return err
	}
	m.orig = snapshot(m.db, m.self)
	return nil
}

// markCreated records an insert made outside Save, so WasRecentlyCreated and WasChanged see it.
func markCreated(v any) {
	if e, ok := v.(entity); ok {
		m := &e.base().m
		m.changes, m.created = maps.Clone(m.orig), true
	}
}
