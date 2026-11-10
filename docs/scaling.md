---
title: Pool, replicas, sharding
nav_order: 16
description: Connection pool options, read replicas and key-based sharding.
---

# Pool, replicas, sharding

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

Beyond a single connection, migro can tune the connection pool, spread reads across replicas (Laravel's *read and write connections*) and route rows to several databases by key (sharding).

## Pool

```go
db, err := migro.Open(migro.Config{
    Dialect: "postgres", DSN: dsn,
    Pool: migro.Pool{
        Min: 2, Max: 10,
        AcquireTimeout: 3 * time.Second,
        IdleTimeout:    5 * time.Minute,
        MaxLifetime:    time.Hour,
        Validate:       true,
        AfterCreate: func(ctx context.Context, c migro.Conn) error {
            return c.Exec(ctx, "SET TIME ZONE 'UTC'")
        },
    },
})
```

| Field | Effect |
|---|---|
| `Max` | Maximum open connections (0 = unlimited). |
| `Min` | Connections opened at startup and kept idle. Best effort: `database/sql` can still close them after `IdleTimeout`/`MaxLifetime`. |
| `AcquireTimeout` | Longest wait for a free connection, then `POOL_ACQUIRE_TIMEOUT`. Never cancels a running query or transaction. |
| `IdleTimeout`, `MaxLifetime` | Recycle connections. |
| `Validate` | Ping a pooled connection on checkout and discard it if dead. Needs a driver implementing `driver.Pinger`. |
| `AfterCreate` | Runs once per new physical connection. If it fails, the connection is discarded. |

Invalid values (negative, or `Min > Max`) return `INVALID_CONFIG`.

## Read replicas

```go
db, err := migro.Open(migro.Config{
    Dialect: "postgres", DSN: primaryDSN,
    Replicas:      []string{replica1DSN, replica2DSN},
    ReplicaPolicy: "round-robin", // or "random" (default)
})

db.Gorm.Find(&users)                 // read  → a replica
db.Gorm.Create(&user)                // write → primary
db.WriteDB().Gorm.First(&user, id)   // force the primary (read your own write)
```

Transactions always use the primary. Replicas can lag, so a read right after a write may be stale; use `WriteDB()` when it matters.

## Sharding

`cluster` routes work across independent databases (for example one per tenant group):

```go
c, err := cluster.Open(ctx, map[string]migro.Config{
    "s1": {Dialect: "postgres", DSN: dsn1},
    "s2": {Dialect: "postgres", DSN: dsn2},
}, cluster.HashRouter([]string{"s1", "s2"}))
defer c.Close()

shard, err := c.For(tenantID)                      // *migro.DB that owns this key
results, err := c.Migrate(ctx, migrations)         // same migrations on every shard
err = c.Each(ctx, func(ctx context.Context, name string, db *migro.DB) error { return nil })
```

You can supply your own `cluster.Router` (`func(key any) (string, error)`) instead of `HashRouter`.

There are no cross-shard transactions. `Each` runs shards concurrently and independently: if one fails you get `SHARD_FAILED` naming it, and the shards that succeeded stay committed. Adding or removing a shard changes where most hash keys land, so move data first.
