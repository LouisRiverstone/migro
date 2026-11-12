package orm

import (
	"context"
	"sort"
	"sync"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/events"
)

// Prunable selects the stale rows of T that Prune deletes one by one, firing model events and
// ForceDelete semantics (Eloquent's Prunable):
//
//	func (Session) Prunable(q *orm.Query[Session]) *orm.Query[Session] { return q.Where("expires_at < ?", time.Now()) }
type Prunable[T any] interface{ Prunable(q *Query[T]) *Query[T] }

// MassPrunable is Prunable deleted with one statement and no events (Eloquent's MassPrunable). When a
// model has both, MassPrunable wins.
type MassPrunable[T any] interface{ MassPrunable(q *Query[T]) *Query[T] }

// Pruning is an optional hook run on each row before a Prunable delete, e.g. to remove its files.
type Pruning interface {
	Pruning(ctx context.Context) error
}

// Prune deletes the model's stale rows for good, soft-deleted ones included, and returns how many.
func (m *Model[T]) Prune(ctx context.Context) (int64, error) {
	if mp, ok := any(new(T)).(MassPrunable[T]); ok {
		n, err := mp.MassPrunable(m.Query()).WithTrashed().Delete(events.WithoutEvents(ctx))
		return n, pruneErr(err)
	}
	p, ok := any(new(T)).(Prunable[T])
	if !ok {
		return 0, migro.Newf(migro.InvalidConfig, "%T implements neither Prunable nor MassPrunable", *new(T))
	}
	var n int64
	err := p.Prunable(m.Query()).WithTrashed().Chunk(ctx, 200, func(rows []T) error {
		for i := range rows {
			if h, ok := any(&rows[i]).(Pruning); ok {
				if err := h.Pruning(ctx); err != nil {
					return err
				}
			}
			if e, ok := any(&rows[i]).(entity); ok {
				if err := e.base().ForceDelete(ctx); err != nil {
					return err
				}
			} else if err := wrap(m.db.Gorm.WithContext(ctx).Unscoped().Delete(&rows[i]).Error, "prune"); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, pruneErr(err)
}

func pruneErr(err error) error {
	if err == nil {
		return nil
	}
	return migro.Wrap(migro.PruneFailed, err, "prune")
}

var prunables = struct {
	sync.Mutex
	byName map[string]func(context.Context, *migro.DB) (int64, error)
}{byName: map[string]func(context.Context, *migro.DB) (int64, error){}}

// RegisterPrunable lets PruneAll (and the model:prune command) find T.
func RegisterPrunable[T any](name string) {
	prunables.Lock()
	defer prunables.Unlock()
	prunables.byName[name] = func(ctx context.Context, db *migro.DB) (int64, error) { return New[T](db).Prune(ctx) }
}

// PruneAll prunes every registered model, stopping at the first failure. It returns the count per name.
func PruneAll(ctx context.Context, db *migro.DB) (map[string]int64, error) {
	prunables.Lock()
	names := make([]string, 0, len(prunables.byName))
	fns := make(map[string]func(context.Context, *migro.DB) (int64, error), len(prunables.byName))
	for n, fn := range prunables.byName {
		names = append(names, n)
		fns[n] = fn
	}
	prunables.Unlock()
	sort.Strings(names)
	out := map[string]int64{}
	for _, n := range names {
		c, err := fns[n](ctx, db)
		out[n] = c
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
