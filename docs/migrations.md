---
title: Migrations
nav_order: 11
description: Laravel-style migration files with Up and Down.
---

# Migrations

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

Migrations are like version control for your database, letting your team define and share the application's schema. If you have ever told a teammate to add a column by hand after pulling your changes, migrations solve that problem. The tables themselves are described with the [schema builder](schema-builder.md).

## Generating migrations

```
migro make:migration create_users_table
migro make:migration add_nickname_to_users_table
```

The file lands in `database/migrations/`, named like Laravel's: `<yyyymmddhhmmss>_<snake_case_name>.go`. See [Command line](command-line.md) for every flag.

## Migration structure

Each file is a Lucid-style class with `Up` and `Down`, and registers itself when the package loads.

```go
// database/migrations/20260101120000_create_users_table.go
package migrations

import (
    "context"

    "github.com/LouisRiverstone/migro/migrate"
    "github.com/LouisRiverstone/migro/schema"
)

func init() { migrate.Register(&CreateUsersTable{}) }

type CreateUsersTable struct{}

func (CreateUsersTable) Up(ctx context.Context, s *schema.Builder) error {
    return s.CreateTable(ctx, "users", func(t *schema.Table) {
        t.BigIncrements("id")
        t.String("email", 120).NotNull().Unique()
        t.String("name")
        t.Timestamps()
        t.SoftDeletes()
    })
}

func (CreateUsersTable) Down(ctx context.Context, s *schema.Builder) error {
    return s.DropTableIfExists(ctx, "users")
}
```

```go
// database/migrations/20260102090000_create_posts_table.go  (indexes and relations)
func (CreatePostsTable) Up(ctx context.Context, s *schema.Builder) error {
    return s.CreateTable(ctx, "posts", func(t *schema.Table) {
        t.BigIncrements("id")
        t.ForeignID("user_id").Constrained().OnDelete("CASCADE")
        t.String("title").Index()
        t.Text("body")
        t.Morphs("subject")
        t.Unique("user_id", "title")
        t.Timestamps()
    })
}
```

The rules:

- **The name comes from the file name.** `migrate.Register` reads the calling file's name, so there is no string to keep in sync. A file that does not match `^\d{14}_[a-z0-9_]+\.go$`, or a duplicate, is reported when migrations load (`MIGRATION_FAILED`). Do not rename a file after it has run: it would count as a new migration.
- **The Go type name must be unique in the package** (`CreateUsersTable`). `make:migration` derives it from the file name and refuses duplicates.
- **Order is by name**, so the timestamp prefix decides it. `make:migration` always gives a new file a timestamp later than the newest existing one.
- **Optional behaviors** on the type: `Connection() string` pins it to a named connection (see [Several databases](command-line.md#several-databases)), and `DisableTransaction() bool` opts out of the wrapping transaction.
- The package must be linked into whatever runs the migrations. The `migro` command does that for you ([Command line](command-line.md)); in your own `main`, import it for its side effects: `import _ "example.com/app/database/migrations"`.

## Running migrations

From the shell, `migro migration:run` runs every pending migration and `migro migration:rollback` undoes the last batch ([Command line](command-line.md)). From Go code:

```go
m := migrate.New(db)
if err := m.AddRegistered(); err != nil { ... }      // everything registered via Register

res, err := m.Latest(ctx)                             // all pending, as one batch
res, err = m.Run(ctx, migrate.RunOptions{Step: true}) // every migration in its own batch
res, err = m.Run(ctx, migrate.RunOptions{Pretend: true}) // res.SQL[name] holds the SQL; nothing changes

res, err = m.Rollback(ctx)               // last batch
res, err = m.RollbackSteps(ctx, 2)       // the 2 most recent migrations
res, err = m.RollbackToBatch(ctx, 1)     // everything after batch 1 (0 = everything)
res, err = m.Reset(ctx)                  // everything
res, err = m.Refresh(ctx)                // Reset + Latest
res, err = m.Fresh(ctx)                  // DROP every table, then Latest (destructive)

status, err := m.Status(ctx)             // []Status{Name, Applied, Batch, AppliedAt}
err = m.Install(ctx)                     // create the tracking tables now (otherwise done on demand)
err = m.Unlock(ctx)                      // clear a stale lock
```

`migrate.New(db, migrate.WithObserver(func(e migrate.Event) { ... }))` reports `migrating`, `migrated`, `reverting`, `reverted` with durations.

Migrations written as plain values still work: `m.Add(migrate.Migration{Name: ..., Up: ..., Down: ...})`, and SQL files can be loaded with `migrate.FromFS(embeddedFS, "migrations")` (`<name>.up.sql` / `<name>.down.sql`).

### Rolling back migrations

`Rollback` undoes the last batch, `RollbackSteps(n)` the *n* most recent migrations, `RollbackToBatch(b)` everything after batch *b*, `Reset` everything. `Refresh` is `Reset` plus `Latest`, and `Fresh` drops every table first (destructive).

### Observing progress

`migrate.New(db, migrate.WithObserver(func(e migrate.Event) { ... }))` reports `migrating`, `migrated`, `reverting`, `reverted` with durations.

## Migration behavior

- Tracking lives in `migro_migrations` (override with `migrate.WithTable`) plus a lock table. A second runner gets `MIGRATION_LOCKED` instead of running concurrently.
- Each migration runs in a transaction when the database supports transactional DDL (PostgreSQL, SQLite, SQL Server, Redshift). **MySQL, MariaDB, Oracle and CockroachDB commit DDL implicitly**, so a failed migration can leave partial changes. Keep migrations small there.
- `--pretend` records every write statement a migration makes instead of running it, including statements issued through `s.DB()`. Reads still run so a migration can inspect the database. A statement that must return rows (for example `INSERT ... RETURNING`) cannot be emulated and fails the pretend run. Nothing is created or locked, not even the tracking tables.
- Migrations with a `Checksum` are checked against what ran (`MIGRATION_CHECKSUM_MISMATCH`). SQL-file migrations get one automatically.
