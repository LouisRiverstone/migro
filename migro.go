// Package migro is a Lucid/Knex-inspired toolkit on top of GORM:
// a schema builder, a query builder, a generic model layer and a migration runner.
//
// Register the dialects you need with a blank import:
//
//	import _ "github.com/LouisRiverstone/migro/dialect/postgres"
package migro

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/LouisRiverstone/migro/dialect"
	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

// Config configures Open.
type Config struct {
	// Dialect name: postgres, cockroachdb, redshift, mysql, mariadb, sqlite, sqlserver, oracle.
	Dialect string
	DSN     string
	// Gorm is optional; nil uses GORM defaults with a silent logger.
	Gorm *gorm.Config

	// Pool configures the connection pool (Knex-style).
	Pool Pool

	// Replicas are read-only DSNs of the same dialect. Reads go to a replica, writes and
	// transactions to the primary. Replication lag can make a read right after a write stale;
	// use DB.WriteDB() to force the primary.
	Replicas []string
	// ReplicaPolicy is "random" (default) or "round-robin".
	ReplicaPolicy string

	// Deprecated: use Pool.Max.
	MaxOpenConns int
	// Deprecated: caps idle connections; use Pool.Min to keep connections warm.
	MaxIdleConns int
	// Deprecated: use Pool.MaxLifetime.
	ConnMaxLifetime time.Duration
}

// DB bundles a *gorm.DB with the dialect it was opened for.
type DB struct {
	Gorm    *gorm.DB
	Dialect dialect.Dialect

	closers []func() error
}

// Open connects using a registered dialect.
func Open(cfg Config) (*DB, error) { return OpenContext(context.Background(), cfg) }

// OpenContext is Open with a context bounding connection setup.
func OpenContext(ctx context.Context, cfg Config) (*DB, error) {
	d, err := dialect.Get(cfg.Dialect)
	if err != nil {
		return nil, Wrap(UnsupportedDialect, err, "open %q", cfg.Dialect)
	}
	pool := cfg.Pool
	if pool.Max == 0 {
		pool.Max = cfg.MaxOpenConns
	}
	pool.maxIdle = cfg.MaxIdleConns
	if pool.MaxLifetime == 0 {
		pool.MaxLifetime = cfg.ConnMaxLifetime
	}
	if err := pool.validate(); err != nil {
		return nil, err
	}
	policy, err := replicaPolicy(cfg.ReplicaPolicy)
	if err != nil {
		return nil, err
	}
	gcfg := cfg.Gorm
	if gcfg == nil {
		gcfg = &gorm.Config{Logger: silentLogger()}
	}

	db := &DB{Dialect: d}
	var dialector gorm.Dialector
	if pool.advanced() || len(cfg.Replicas) > 0 {
		h, err := openPool(ctx, d, cfg.DSN, pool)
		if err != nil {
			return nil, err
		}
		db.closers = append(db.closers, h.sqlDB.Close)
		dialector = d.FromConn(h.conn)
	} else {
		dialector = d.Dialector(cfg.DSN)
	}
	g, err := gorm.Open(dialector, gcfg)
	if err != nil {
		db.closeAll()
		return nil, Wrap(ConnectionFailed, err, "connect %s", d.Name())
	}
	db.Gorm = g
	if len(db.closers) == 0 {
		sqlDB, err := g.DB()
		if err != nil {
			return nil, Wrap(ConnectionFailed, err, "get sql.DB")
		}
		pool.apply(sqlDB)
		if err := pool.warm(ctx, sqlDB); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
		db.closers = append(db.closers, sqlDB.Close)
	}

	if len(cfg.Replicas) > 0 {
		dials := make([]gorm.Dialector, 0, len(cfg.Replicas))
		for _, dsn := range cfg.Replicas {
			h, err := openPool(ctx, d, dsn, pool)
			if err != nil {
				db.closeAll()
				return nil, err
			}
			db.closers = append(db.closers, h.sqlDB.Close)
			dials = append(dials, d.FromConn(h.conn))
		}
		if err := g.Use(dbresolver.Register(dbresolver.Config{Replicas: dials, Policy: policy})); err != nil {
			db.closeAll()
			return nil, Wrap(ConnectionFailed, err, "register replicas")
		}
	}
	return db, nil
}

func replicaPolicy(name string) (dbresolver.Policy, error) {
	switch name {
	case "", "random":
		return dbresolver.RandomPolicy{}, nil
	case "round-robin":
		return &roundRobin{}, nil
	}
	return nil, Newf(InvalidConfig, "unknown replica policy %q (use random or round-robin)", name)
}

type roundRobin struct{ n atomic.Uint64 }

func (r *roundRobin) Resolve(pools []gorm.ConnPool) gorm.ConnPool {
	return pools[int(r.n.Add(1)-1)%len(pools)]
}

func (d *DB) closeAll() {
	for _, c := range d.closers {
		_ = c()
	}
}

// WriteDB returns a handle whose queries all go to the primary (read-your-writes).
func (d *DB) WriteDB() *DB {
	return &DB{Gorm: d.Gorm.Clauses(dbresolver.Write), Dialect: d.Dialect}
}

// Adopt adopts an existing *gorm.DB.
func Adopt(g *gorm.DB, d dialect.Dialect) *DB {
	db := &DB{Gorm: g, Dialect: d}
	if sqlDB, err := g.DB(); err == nil {
		db.closers = append(db.closers, sqlDB.Close)
	}
	return db
}

// Close releases every connection pool (primary and replicas).
func (d *DB) Close() error {
	var errs []error
	for _, c := range d.closers {
		if err := c(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return Wrap(ConnectionFailed, errors.Join(errs...), "close")
	}
	return nil
}

// Ping verifies connectivity.
func (d *DB) Ping(ctx context.Context) error {
	sqlDB, err := d.Gorm.DB()
	if err != nil {
		return Wrap(ConnectionFailed, err, "get sql.DB")
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return Wrap(ConnectionFailed, err, "ping")
	}
	return nil
}

// Transaction runs fn in a transaction; a non-nil error rolls back.
// fn receives a *DB bound to the transaction, usable with schema, query, orm and migrate.
func (d *DB) Transaction(ctx context.Context, fn func(tx *DB) error) error {
	return d.Gorm.WithContext(ctx).Transaction(func(g *gorm.DB) error {
		return fn(&DB{Gorm: g, Dialect: d.Dialect})
	})
}
