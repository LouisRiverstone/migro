// Package orm is a Lucid-style model layer: a generic repository over GORM models.
//
// Relations, hooks (BeforeCreate, ...) and soft deletes use GORM's native
// struct tags, interfaces and gorm.DeletedAt; Query.Preload exposes eager loading.
package orm

import (
	"context"
	"errors"
	"time"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
)

// BaseModel is an optional embeddable base, like Lucid's BaseModel.
type BaseModel struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SoftDeleteModel adds soft deletes: Delete sets DeletedAt and queries hide those rows.
type SoftDeleteModel struct {
	BaseModel
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// Model is the repository for T.
type Model[T any] struct{ db *migro.DB }

// New binds a model type to a database (or transaction-bound *migro.DB).
func New[T any](db *migro.DB) *Model[T] { return &Model[T]{db: db} }

// Query starts a fluent query.
func (m *Model[T]) Query() *Query[T] {
	return &Query[T]{db: m.db, g: m.db.Gorm.Model(new(T))}
}

func wrap(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return migro.Wrap(migro.NotFound, err, "%s", op)
	}
	return migro.Wrap(migro.QueryFailed, err, "%s", op)
}

// Find loads by primary key; returns migro.ErrNotFound-matching error when missing.
func (m *Model[T]) Find(ctx context.Context, id any) (*T, error) {
	var v T
	if err := m.db.Gorm.WithContext(ctx).First(&v, id).Error; err != nil {
		return nil, wrap(err, "find")
	}
	return &v, nil
}

// FindBy loads the first row where column = value.
func (m *Model[T]) FindBy(ctx context.Context, column string, value any) (*T, error) {
	return m.Query().Where(m.db.Dialect.Quote(column)+" = ?", value).First(ctx)
}

func (m *Model[T]) All(ctx context.Context) ([]T, error) { return m.Query().Get(ctx) }

func (m *Model[T]) Create(ctx context.Context, v *T) error {
	return wrap(m.db.Gorm.WithContext(ctx).Create(v).Error, "create")
}

func (m *Model[T]) CreateMany(ctx context.Context, vs []T) error {
	if len(vs) == 0 {
		return nil
	}
	return wrap(m.db.Gorm.WithContext(ctx).Create(&vs).Error, "create many")
}

// Save inserts or updates all fields by primary key.
func (m *Model[T]) Save(ctx context.Context, v *T) error {
	return wrap(m.db.Gorm.WithContext(ctx).Save(v).Error, "save")
}

// Delete removes v (soft when T has gorm.DeletedAt).
func (m *Model[T]) Delete(ctx context.Context, v *T) error {
	return wrap(m.db.Gorm.WithContext(ctx).Delete(v).Error, "delete")
}

// FirstOrCreate finds a row matching the non-zero fields of where, or creates it with attrs applied.
// Returns whether a row was created.
func (m *Model[T]) FirstOrCreate(ctx context.Context, where T, attrs T) (*T, bool, error) {
	out := where
	res := m.db.Gorm.WithContext(ctx).Where(&where).Attrs(&attrs).FirstOrCreate(&out)
	if res.Error != nil {
		return nil, false, wrap(res.Error, "first or create")
	}
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
	return out, nil
}
