---
title: How it works
nav_order: 4
description: The mental model behind migro: layers, dialects, migrations and what the command line does.
---

# How it works

## The big picture

```
 your application
   │
   ▼
 migro   schema · query · orm · migrate · seed · events · cluster · console
   │
   ▼
 GORM    (a *gorm.DB sits inside every migro.DB, always reachable as db.Gorm)
   │
   ▼
 database/sql driver ──► PostgreSQL · MySQL · SQLite · SQL Server · Oracle · …
```

migro does not replace GORM. It adds the pieces GORM leaves out (a schema builder, versioned migrations, seeders, a CLI) and a Lucid-style layer for everyday queries. Anything GORM can do is still available through `db.Gorm`, and your GORM models, hooks and relations work unchanged.

## The packages

| Package | Role |
|---|---|
| `migro` | `Open`, the `DB` type (a `*gorm.DB` plus its dialect), pool and replica options, the `*Error` type |
| `dialect/*` | One adapter per database: type mapping, identifier quoting, driver wiring. You import the ones you need |
| `schema` | Describe tables once; compiled to the right SQL for the database in use |
| `query` | Fluent queries for tables without a model |
| `orm` | Generic models (`orm.New[User](db)`), plus the relations GORM lacks |
| `migrate` | Registering, ordering and running migrations; `migrate/diff` generates them from models |
| `seed` | Factories, a fake-data generator and seeders |
| `events` | An event bus and model lifecycle events |
| `cluster` | Route keys to separate databases |
| `console` | The command implementations behind `migro` |
| `cmd/migro` | The `migro` program |

## One definition, many databases

A table described with the schema builder is only a description. The dialect decides the SQL:

```go
t.BigIncrements("id")
```

becomes `bigserial primary key` on PostgreSQL, `bigint unsigned not null auto_increment primary key` on MySQL, `integer primary key autoincrement` on SQLite and an identity column on SQL Server and Oracle. Where a database cannot do something (adding a foreign key to an existing SQLite table, `CREATE INDEX` on Redshift) you get an `UNSUPPORTED_FEATURE` error instead of silently different behavior. See [Database notes](databases.md).

## Errors

Every failure is a `*migro.Error` carrying a stable `Code` such as `NOT_FOUND` or `MIGRATION_LOCKED`. Match with `errors.Is(err, migro.ErrNotFound)` or `migro.CodeOf(err)`. The underlying driver error is kept and reachable with `errors.Unwrap`. See [Errors](errors.md).

## Migrations

A migration is a Go type with `Up` and `Down`, in a file named `<14-digit timestamp>_<snake_name>.go`. Its `init()` calls `migrate.Register`, which records it in a package-level registry and takes the migration's name from the file name. Nothing runs at that point.

When migrations run:

1. The registered migrations are sorted by name, so the timestamp decides the order.
2. migro reads the `migro_migrations` table (created on demand) to learn which names have already run.
3. It takes a lock (a row in a lock table) so two runners cannot overlap. A second runner gets `MIGRATION_LOCKED`.
4. Each pending migration runs. On databases with transactional DDL (PostgreSQL, SQLite, SQL Server, Redshift) it runs in a transaction together with the bookkeeping row, so a failure leaves nothing half-done. MySQL, MariaDB, Oracle and CockroachDB commit DDL implicitly, so a failed migration there can leave partial changes.
5. The pending migrations from one run share a **batch** number. `migration:rollback` undoes the last batch, `--step=N` undoes the last N migrations, `--batch=N` undoes everything after batch N.
6. The lock is released, even on failure.

Rolling back calls `Down` on the registered type, so the code for a migration must still be in your project to undo it. Do not rename or delete a migration file after it has run: its name is its identity.

`--pretend` runs `Up` or `Down` against a recording connection that captures write statements instead of executing them, so you see the exact SQL without changing anything. See [Migrations](migrations.md).

## What migro does when you run a command

File generators (`make:*`) just write files. Everything else needs your project's migrations and seeders, and they are Go code, so `migro` has to compile them:

1. It finds your `go.mod`, loads `.env`, and works out which database drivers your configuration needs.
2. It writes a small `main` into a temporary `_migro_run_*` folder inside your module. That program imports your `database/migrations` and `database/seeders` packages (so their `init()` functions register everything), the driver package, and `migro/console`.
3. It builds that program and runs it from your project root, passing your arguments and the environment. The exit code and output come back unchanged.
4. It deletes the temporary folder.

That is why a command takes as long as a `go build`, and why you must run `migro` inside the module. For `migration:diff`, the generated program also imports your models package and passes the models to the diff engine.

You do not have to use the `migro` program at all. `console.App` is a library: put it behind a subcommand of your own binary and the migrations are linked in the normal way, with no build step at runtime. See [Command line](command-line.md#how-it-runs).

## Models, queries and transactions

`orm.New[User](db)` returns a thin repository over `db.Gorm`. Its queries are GORM queries; `Query().Where(...)` adds to a GORM statement, so soft deletes, hooks, `Preload` and scopes behave as in GORM. `db.Transaction(ctx, fn)` hands `fn` a `*migro.DB` bound to the transaction, and every layer (schema, query, orm, migrate, seed) accepts it.

## Events

The event bus attaches to GORM through a plugin that registers create, update, delete and query callbacks. A `before:*` handler that returns an error aborts the operation and rolls back its transaction. Like GORM hooks, events do not fire for `UpdateColumn`, raw SQL or the query builder. See [Events](events.md).

## Replicas and sharding

Read replicas use GORM's `dbresolver` plugin: reads go to a replica, writes and transactions to the primary, and `db.WriteDB()` forces the primary. Sharding (`cluster`) is application-level: a router maps a key to one of several independent databases. There are no cross-shard transactions. See [Pool, replicas, sharding](scaling.md).
