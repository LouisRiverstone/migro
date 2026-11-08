// Package migro is a Lucid/Knex-inspired toolkit on top of GORM:
// a schema builder, a query builder, a generic model layer and a migration runner.
//
// Register the dialects you need with a blank import:
//
//	import _ "github.com/LouisRiverstone/migro/dialect/postgres"
package migro

import (
	"context"
	"time"

	"github.com/LouisRiverstone/migro/dialect"
	"gorm.io/gorm"
)

// Config configures Open.
type Config struct {
	// Dialect name: postgres, cockroachdb, redshift, mysql, mariadb, sqlite, sqlserver, oracle.
	Dialect string
	DSN     string
	// Gorm is optional; nil uses GORM defaults with a silent logger.
	Gorm *gorm.Config

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DB bundles a *gorm.DB with the dialect it was opened for.
type DB struct {
	Gorm    *gorm.DB
	Dialect dialect.Dialect
}

// Open connects using a registered dialect.
func Open(cfg Config) (*DB, error) {
	d, err := dialect.Get(cfg.Dialect)
	if err != nil {
		return nil, Wrap(UnsupportedDialect, err, "open %q", cfg.Dialect)
	}
	gcfg := cfg.Gorm
	if gcfg == nil {
		gcfg = &gorm.Config{Logger: silentLogger()}
	}
	g, err := gorm.Open(d.Dialector(cfg.DSN), gcfg)
	if err != nil {
		return nil, Wrap(ConnectionFailed, err, "connect %s", d.Name())
	}
	sqlDB, err := g.DB()
	if err != nil {
		return nil, Wrap(ConnectionFailed, err, "get sql.DB")
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	return &DB{Gorm: g, Dialect: d}, nil
}

// Adopt adopts an existing *gorm.DB.
func Adopt(g *gorm.DB, d dialect.Dialect) *DB { return &DB{Gorm: g, Dialect: d} }

// Close releases the connection pool.
func (d *DB) Close() error {
	sqlDB, err := d.Gorm.DB()
	if err != nil {
		return Wrap(ConnectionFailed, err, "get sql.DB")
	}
	return sqlDB.Close()
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
