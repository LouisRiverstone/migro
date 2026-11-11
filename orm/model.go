// Package orm is a Lucid-style model layer: a generic repository over GORM models.
//
// Relations, hooks (BeforeCreate, ...) and soft deletes use GORM's native
// struct tags, interfaces and gorm.DeletedAt; Query.Preload exposes eager loading.
package orm

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
)

// Record carries the active-record behaviour (Save, Delete, Refresh, Fill, IsDirty, relations, ...) without
// any column. BaseModel, UUIDModel and ULIDModel embed it and add a primary key; embed it yourself to
// bring your own key.
//
// Models returned by Model[T] / Query[T] are bound to their database, which enables the instance
// methods and lazy relations. A struct you build by hand is unbound until you create it through the
// repository or call Bind.
type Record struct {
	m meta // unexported: GORM ignores it
}

// BaseModel is an optional embeddable base with an auto-increment uint key, like Lucid's BaseModel.
type BaseModel struct {
	Record
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SoftDeleteModel adds soft deletes: Delete sets DeletedAt and queries hide those rows.
type SoftDeleteModel struct {
	BaseModel
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// UUIDModel is BaseModel with a time-ordered UUID (version 7) string key, filled on insert
// (Eloquent's HasUuids). Every instance method works, unlike the bare UUIDKey.
type UUIDModel struct {
	Record
	ID        string `gorm:"primaryKey;size:36"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (k *UUIDModel) BeforeCreate(*gorm.DB) error {
	if k.ID == "" {
		k.ID = NewUUID()
	}
	return nil
}

// SoftDeleteUUIDModel is UUIDModel with soft deletes.
type SoftDeleteUUIDModel struct {
	UUIDModel
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// ULIDModel is BaseModel with a 26-character ULID key (Eloquent's HasUlids).
type ULIDModel struct {
	Record
	ID        string `gorm:"primaryKey;size:26"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (k *ULIDModel) BeforeCreate(*gorm.DB) error {
	if k.ID == "" {
		k.ID = NewULID()
	}
	return nil
}

// SoftDeleteULIDModel is ULIDModel with soft deletes.
type SoftDeleteULIDModel struct {
	ULIDModel
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// Model is the repository for T.
type Model[T any] struct{ db *migro.DB }

// New binds a model type to a database (or transaction-bound *migro.DB).
func New[T any](db *migro.DB) *Model[T] { return &Model[T]{db: db} }

// Query starts a fluent query.
//
// Global scopes declared by the model (see GlobalScoped) are applied to it.
func (m *Model[T]) Query() *Query[T] {
	q := &Query[T]{db: m.db, g: m.db.Gorm.Model(new(T)), skip: &skipSet{}}
	if scopes := globalScopesOf(reflect.TypeFor[T]()); len(scopes) > 0 {
		q.g = q.g.Scopes(scopeFunc(scopes, q.skip))
	}
	return q
}

func wrap(err error, op string) error {
	if err == nil {
		return nil
	}
	var already *migro.Error
	if errors.As(err, &already) {
		return err // keep the original code (e.g. UNSUPPORTED_FEATURE raised inside a query)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return migro.Wrap(migro.NotFound, err, "%s", op)
	}
	return migro.Wrap(migro.QueryFailed, err, "%s", op)
}

// Find loads by primary key; returns migro.ErrNotFound-matching error when missing.
func (m *Model[T]) Find(ctx context.Context, id any) (*T, error) {
	var v T
	g, err := m.byKey(ctx, []any{id})
	if err != nil {
		return nil, err
	}
	if err := g.First(&v).Error; err != nil {
		return nil, wrap(err, "find")
	}
	bind(m.db, &v, true)
	return &v, nil
}

// FindBy loads the first row where column = value.
func (m *Model[T]) FindBy(ctx context.Context, column string, value any) (*T, error) {
	return m.Query().Where(m.db.Dialect.Quote(column)+" = ?", value).First(ctx)
}

func (m *Model[T]) All(ctx context.Context) (Collection[T], error) { return m.Query().Get(ctx) }

func (m *Model[T]) Create(ctx context.Context, v *T) error {
	if err := wrap(m.db.Gorm.WithContext(ctx).Create(v).Error, "create"); err != nil {
		return err
	}
	bind(m.db, v, true)
	markCreated(v)
	return touchParents(ctx, m.db, v)
}

// byKey scopes a query to the given primary keys. It names the key column itself: GORM would read a
// non-numeric string passed to First/Find/Delete as raw SQL, which breaks string and UUID keys.
func (m *Model[T]) byKey(ctx context.Context, ids []any) (*gorm.DB, error) {
	sch, err := parseModel(m.db, new(T))
	if err != nil {
		return nil, err
	}
	pk, err := primaryKey(sch)
	if err != nil {
		return nil, err
	}
	g := m.scoped(ctx)
	if len(ids) == 1 {
		return g.Where(m.db.Dialect.Quote(pk)+" = ?", ids[0]), nil
	}
	return g.Where(m.db.Dialect.Quote(pk)+" IN ?", ids), nil
}

// Make builds an unsaved model from attrs, honouring Fillable/Guarded (Eloquent's new Model($attrs)).
func (m *Model[T]) Make(attrs map[string]any) (*T, error) {
	v := new(T)
	bind(m.db, v, false)
	if d, ok := any(v).(Defaults); ok {
		if err := fillWith(context.Background(), m.db, v, d.Defaults(), false); err != nil {
			return nil, err
		}
	}
	if err := fill(context.Background(), m.db, v, attrs); err != nil {
		return nil, err
	}
	return v, nil
}

// CreateFrom builds a model from attrs (mass assignment rules apply) and inserts it (Eloquent's Model::create).
func (m *Model[T]) CreateFrom(ctx context.Context, attrs map[string]any) (*T, error) {
	v, err := m.Make(attrs)
	if err != nil {
		return nil, err
	}
	if err := m.Create(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

// Destroy deletes rows by primary key (soft when the model soft deletes) and returns how many.
func (m *Model[T]) Destroy(ctx context.Context, ids ...any) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	g, err := m.byKey(ctx, ids)
	if err != nil {
		return 0, err
	}
	res := g.Delete(new(T))
	return res.RowsAffected, wrap(res.Error, "destroy")
}

// FindMany loads several rows by primary key; missing ids are simply absent.
func (m *Model[T]) FindMany(ctx context.Context, ids ...any) (Collection[T], error) {
	if len(ids) == 0 {
		return Collection[T]{}, nil
	}
	var out Collection[T]
	g, err := m.byKey(ctx, ids)
	if err != nil {
		return nil, err
	}
	if err := g.Find(&out).Error; err != nil {
		return nil, wrap(err, "find many")
	}
	bindAll(m.db, out, true)
	return out, nil
}

// FindOrFail is Find: a missing row is already a NOT_FOUND error.
func (m *Model[T]) FindOrFail(ctx context.Context, id any) (*T, error) { return m.Find(ctx, id) }

func (m *Model[T]) CreateMany(ctx context.Context, vs []T) error {
	if len(vs) == 0 {
		return nil
	}
	if err := wrap(m.db.Gorm.WithContext(ctx).Create(&vs).Error, "create many"); err != nil {
		return err
	}
	bindAll(m.db, vs, true)
	for i := range vs {
		markCreated(&vs[i])
	}
	return nil
}

// UpdateOrCreateMany upserts each row by the given key columns, in one transaction
// (Lucid's updateOrCreateMany): an existing row gets the other fields of the row applied.
func (m *Model[T]) UpdateOrCreateMany(ctx context.Context, keys []string, rows []T) ([]T, error) {
	return m.upsertMany(ctx, keys, rows, true)
}

// FirstOrCreateMany is UpdateOrCreateMany that leaves existing rows untouched (Lucid's fetchOrCreateMany).
func (m *Model[T]) FirstOrCreateMany(ctx context.Context, keys []string, rows []T) ([]T, error) {
	return m.upsertMany(ctx, keys, rows, false)
}

func (m *Model[T]) upsertMany(ctx context.Context, keys []string, rows []T, update bool) ([]T, error) {
	if len(keys) == 0 {
		return nil, migro.Newf(migro.InvalidConfig, "upsert needs at least one key column")
	}
	sch, err := parseModel(m.db, new(T))
	if err != nil {
		return nil, err
	}
	// never rewrite the key, the creation time, or the soft-delete mark (that would resurrect rows)
	omit := []string{"created_at"}
	if sch.PrioritizedPrimaryField != nil {
		omit = append(omit, sch.PrioritizedPrimaryField.DBName)
	}
	if f := softDeleteField(sch); f != nil {
		omit = append(omit, f.DBName)
	}
	out := make([]T, 0, len(rows))
	err = m.db.Transaction(ctx, func(tx *migro.DB) error {
		for i := range rows {
			rv := reflect.ValueOf(&rows[i]).Elem()
			where := map[string]any{}
			for _, k := range keys {
				v, err := fieldOf(ctx, sch, rv, k)
				if err != nil {
					return err
				}
				where[k] = v
			}
			var found T
			res := tx.Gorm.WithContext(ctx).Where(where).Limit(1).Find(&found)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				if err := tx.Gorm.WithContext(ctx).Create(&rows[i]).Error; err != nil {
					return err
				}
				out = append(out, rows[i])
				continue
			}
			if update {
				if err := tx.Gorm.WithContext(ctx).Model(&found).Select("*").Omit(omit...).Updates(rows[i]).Error; err != nil {
					return err
				}
			}
			out = append(out, found)
		}
		return nil
	})
	if err != nil {
		return nil, wrap(err, "upsert many")
	}
	bindAll(m.db, out, true)
	return out, nil
}

// Save inserts or updates all fields by primary key.
func (m *Model[T]) Save(ctx context.Context, v *T) error {
	if err := wrap(m.db.Gorm.WithContext(ctx).Save(v).Error, "save"); err != nil {
		return err
	}
	return touchParents(ctx, m.db, v)
}

// Delete removes v (soft when T has gorm.DeletedAt).
func (m *Model[T]) Delete(ctx context.Context, v *T) error {
	if err := wrap(m.db.Gorm.WithContext(ctx).Delete(v).Error, "delete"); err != nil {
		return err
	}
	return touchParents(ctx, m.db, v)
}

// FirstOrCreate finds a row matching the non-zero fields of where, or creates it with attrs applied.
// Returns whether a row was created.
func (m *Model[T]) FirstOrCreate(ctx context.Context, where T, attrs T) (*T, bool, error) {
	out := where
	res := m.db.Gorm.WithContext(ctx).Where(&where).Attrs(&attrs).FirstOrCreate(&out)
	if res.Error != nil {
		return nil, false, wrap(res.Error, "first or create")
	}
	bind(m.db, &out, true)
	return &out, res.RowsAffected > 0, nil
}

// UpdateOrCreate finds by where and applies values, or creates the row with where+values.
func (m *Model[T]) UpdateOrCreate(ctx context.Context, where T, values T) (*T, error) {
	var out *T
	err := m.db.Transaction(ctx, func(tx *migro.DB) error {
		found := where
		res := tx.Gorm.Where(&where).Assign(&values).FirstOrCreate(&found)
		if res.Error != nil {
			return res.Error
		}
		out = &found
		return nil
	})
	if err != nil {
		return nil, wrap(err, "update or create")
	}
	bind(m.db, out, true)
	return out, nil
}
