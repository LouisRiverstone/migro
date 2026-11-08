// Package seed provides Lucid-style factories and seeders.
package seed

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/LouisRiverstone/migro"
)

// RelationFn creates n related rows for a freshly created parent (Lucid's factory.with()).
type RelationFn[T any] func(ctx context.Context, db *migro.DB, parent *T, n int, f *Faker) error

type withSpec struct {
	name string
	n    int
}

// Factory builds and persists models. Configure it once with Define/State/Relation/After, then
// derive variants with Merge/Apply/With/Seed — each returns a copy, so a shared factory is never mutated.
type Factory[T any] struct {
	define    func(f *Faker) T
	states    map[string]func(*T)
	relations map[string]RelationFn[T]
	after     []func(ctx context.Context, db *migro.DB, m *T) error

	merges  []func(*T)
	applied []string
	with    []withSpec
	seed    int64
	hasSeed bool
}

// Define creates a factory from a function returning default attribute values.
func Define[T any](define func(f *Faker) T) *Factory[T] {
	return &Factory[T]{define: define, states: map[string]func(*T){}, relations: map[string]RelationFn[T]{}}
}

// State registers a named variant (Lucid's .state()). Call during setup.
func (f *Factory[T]) State(name string, fn func(*T)) *Factory[T] { f.states[name] = fn; return f }

// Relation registers a named relation builder. Call during setup.
func (f *Factory[T]) Relation(name string, fn RelationFn[T]) *Factory[T] {
	f.relations[name] = fn
	return f
}

// After registers a hook run after each Create (Lucid's .after('create')). Call during setup.
func (f *Factory[T]) After(fn func(ctx context.Context, db *migro.DB, m *T) error) *Factory[T] {
	f.after = append(f.after, fn)
	return f
}

func (f *Factory[T]) clone() *Factory[T] {
	c := *f
	c.merges = slices.Clone(f.merges)
	c.applied = slices.Clone(f.applied)
	c.with = slices.Clone(f.with)
	return &c
}

// Merge overrides attributes on every generated model (Lucid's .merge()).
func (f *Factory[T]) Merge(fn func(*T)) *Factory[T] {
	c := f.clone()
	c.merges = append(c.merges, fn)
	return c
}

// Apply activates named states in order (Lucid's .apply()).
func (f *Factory[T]) Apply(names ...string) *Factory[T] {
	c := f.clone()
	c.applied = append(c.applied, names...)
	return c
}

// With creates n related rows per created model (Lucid's .with()).
func (f *Factory[T]) With(relation string, n int) *Factory[T] {
	c := f.clone()
	c.with = append(c.with, withSpec{relation, n})
	return c
}

// Seed makes generated data reproducible.
func (f *Factory[T]) Seed(seed int64) *Factory[T] {
	c := f.clone()
	c.seed, c.hasSeed = seed, true
	return c
}

func (f *Factory[T]) faker() *Faker {
	if f.hasSeed {
		return NewFaker(f.seed)
	}
	return NewFaker(time.Now().UnixNano())
}

func (f *Factory[T]) build(fk *Faker) (T, error) {
	m := f.define(fk)
	for _, name := range f.applied {
		st, ok := f.states[name]
		if !ok {
			return m, migro.Newf(migro.SeedFailed, "unknown factory state %q", name)
		}
		st(&m)
	}
	for _, fn := range f.merges {
		fn(&m)
	}
	return m, nil
}

// Make builds one model without persisting it.
func (f *Factory[T]) Make() (T, error) { return f.build(f.faker()) }

// MakeMany builds n models without persisting them.
func (f *Factory[T]) MakeMany(n int) ([]T, error) {
	fk := f.faker()
	out := make([]T, 0, n)
	for range n {
		m, err := f.build(fk)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// Create builds and inserts one model, then runs With relations and After hooks.
func (f *Factory[T]) Create(ctx context.Context, db *migro.DB) (*T, error) {
	out, err := f.CreateMany(ctx, db, 1)
	if err != nil {
		return nil, err
	}
	return &out[0], nil
}

// CreateMany inserts n models in a single transaction: either all of them exist afterwards or none.
func (f *Factory[T]) CreateMany(ctx context.Context, db *migro.DB, n int) ([]T, error) {
	if n < 1 {
		return nil, migro.Newf(migro.SeedFailed, "CreateMany needs n >= 1, got %d", n)
	}
	fk := f.faker()
	models := make([]T, 0, n)
	err := db.Transaction(ctx, func(tx *migro.DB) error {
		for i := range n {
			m, err := f.build(fk)
			if err != nil {
				return err
			}
			if err := tx.Gorm.WithContext(ctx).Create(&m).Error; err != nil {
				return migro.Wrap(migro.SeedFailed, err, "create model %d of %d", i+1, n)
			}
			for _, w := range f.with {
				rel, ok := f.relations[w.name]
				if !ok {
					return migro.Newf(migro.SeedFailed, "unknown factory relation %q", w.name)
				}
				if err := rel(ctx, tx, &m, w.n, fk); err != nil {
					return migro.Wrap(migro.SeedFailed, err, "relation %q", w.name)
				}
			}
			for _, h := range f.after {
				if err := h(ctx, tx, &m); err != nil {
					return migro.Wrap(migro.SeedFailed, err, "after hook")
				}
			}
			models = append(models, m)
		}
		return nil
	})
	if err != nil {
		if migro.CodeOf(err) == migro.SeedFailed {
			return nil, err
		}
		return nil, migro.Wrap(migro.SeedFailed, err, "%s", fmt.Sprintf("create %d models", n))
	}
	return models, nil
}
