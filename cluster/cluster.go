// Package cluster routes work across several independent databases (application-level sharding).
//
// Each shard is a full migro.DB, so replicas, pools and events work per shard. Cross-shard
// transactions are not supported: Each runs shards independently and reports per-shard failures,
// but shards that succeeded stay committed.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"sort"
	"sync"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/migrate"
)

// Router maps a shard key (tenant id, user id, ...) to a shard name.
type Router func(key any) (string, error)

// HashRouter spreads keys over shards by FNV hash of the key's printed value.
// Adding or removing a shard changes the mapping for most keys; migrate data before doing so.
func HashRouter(shardNames []string) Router {
	names := slices.Clone(shardNames)
	sort.Strings(names)
	return func(key any) (string, error) {
		if key == nil || len(names) == 0 {
			return "", migro.Newf(migro.ShardNotFound, "cannot route key %v", key)
		}
		h := fnv.New32a()
		_, _ = fmt.Fprint(h, key)
		return names[int(h.Sum32())%len(names)], nil
	}
}

type Cluster struct {
	shards map[string]*migro.DB
	names  []string
	route  Router
}

// New builds a cluster from already-open shards.
func New(shards map[string]*migro.DB, route Router) (*Cluster, error) {
	if len(shards) == 0 {
		return nil, migro.Newf(migro.InvalidConfig, "cluster needs at least one shard")
	}
	if route == nil {
		return nil, migro.Newf(migro.InvalidConfig, "cluster needs a router")
	}
	names := make([]string, 0, len(shards))
	for n := range shards {
		names = append(names, n)
	}
	sort.Strings(names)
	return &Cluster{shards: shards, names: names, route: route}, nil
}

// Open connects every shard; if one fails, the ones already opened are closed.
func Open(ctx context.Context, cfgs map[string]migro.Config, route Router) (*Cluster, error) {
	shards := make(map[string]*migro.DB, len(cfgs))
	for name, cfg := range cfgs {
		db, err := migro.OpenContext(ctx, cfg)
		if err != nil {
			for _, opened := range shards {
				_ = opened.Close()
			}
			return nil, migro.Wrap(migro.ConnectionFailed, err, "open shard %q", name)
		}
		shards[name] = db
	}
	return New(shards, route)
}

func (c *Cluster) Names() []string { return slices.Clone(c.names) }

func (c *Cluster) Shard(name string) (*migro.DB, error) {
	db, ok := c.shards[name]
	if !ok {
		return nil, migro.Newf(migro.ShardNotFound, "no shard named %q", name)
	}
	return db, nil
}

// For returns the shard owning key.
func (c *Cluster) For(key any) (*migro.DB, error) {
	name, err := c.route(key)
	if err != nil {
		return nil, err
	}
	return c.Shard(name)
}

// Each runs fn on every shard concurrently. Failures are collected as SHARD_FAILED, naming each shard.
func (c *Cluster) Each(ctx context.Context, fn func(ctx context.Context, name string, db *migro.DB) error) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, name := range c.names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(ctx, name, c.shards[name]); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("shard %s: %w", name, err))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(errs) == 0 {
		return nil
	}
	return migro.Wrap(migro.ShardFailed, errors.Join(errs...), "%d of %d shards failed", len(errs), len(c.names))
}

// Migrate applies the same migrations to every shard and returns each shard's result.
func (c *Cluster) Migrate(ctx context.Context, ms []migrate.Migration) (map[string]migrate.Result, error) {
	var mu sync.Mutex
	out := make(map[string]migrate.Result, len(c.names))
	err := c.Each(ctx, func(ctx context.Context, name string, db *migro.DB) error {
		m := migrate.New(db)
		if err := m.Add(ms...); err != nil {
			return err
		}
		res, err := m.Latest(ctx)
		mu.Lock()
		out[name] = res
		mu.Unlock()
		return err
	})
	return out, err
}

// Close closes every shard.
func (c *Cluster) Close() error {
	var errs []error
	for _, name := range c.names {
		if err := c.shards[name].Close(); err != nil {
			errs = append(errs, fmt.Errorf("shard %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}
