---
title: Migrations
nav_order: 11
description: "Generate, structure, run, roll back, refresh and squash migrations; connections, transactions, locking, production safety and events."
---

# Migrations

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

Migrations are like version control for your database: your team defines the schema in code and shares it. If you have ever told a teammate to add a column by hand after pulling your changes, migrations solve that problem. The tables themselves are described with the [schema builder](schema-builder.md), which writes the right SQL for each database.

## Generating migrations

```
migro make:migration create_flights_table
```

The file lands in `database/migrations/`, named like Laravel's: `<yyyymmddhhmmss>_<snake_case_name>.go`. The timestamp decides the order in which migrations run.

migro guesses the table from the name and fills in the body: `create_X_table` gets a `CreateTable("X")` stub, and `add_..._to_X_table` an `AlterTable("X")` stub. Otherwise, or to be explicit, pass `--create=flights` or `--table=flights`. `--path=dir` writes somewhere else (set `MIGRO_MIGRATIONS_DIR` to change the folder for good).

`migro make:model Flight -m` creates a model and its migration together. `migro migration:diff` writes a migration from the difference between your models and the database ([Schema diff](schema-diff.md)).

### Squashing migrations

After a few hundred migrations, building a fresh database means replaying all of them. `migro schema:dump` squashes them into one SQL file, `database/schema/<dialect>-schema.sql`. On a database with no migration history, `migration:run` loads that file first, then runs the migrations written after it. Commit the file. Unlike Laravel's, the dump needs no `pg_dump` or `mysqldump`, and it works on every dialect: see [Schema dump](schema-dump.md).

## Migration structure

A migration is a Go type with `Up` and `Down`. `Up` adds tables, columns or indexes; `Down` reverses what `Up` did. Each file registers its migration when the package loads:

```go
// database/migrations/20260101120000_create_flights_table.go
package migrations

import (
    "context"

    "github.com/LouisRiverstone/migro/migrate"
    "github.com/LouisRiverstone/migro/schema"
)

func init() { migrate.Register(&CreateFlightsTable{}) }

type CreateFlightsTable struct{}

func (CreateFlightsTable) Up(ctx context.Context, s *schema.Builder) error {
    return s.CreateTable(ctx, "flights", func(t *schema.Table) {
        t.BigIncrements("id")
        t.String("name").NotNull()
        t.String("airline").NotNull()
        t.ForeignID("pilot_id").Constrained().OnDelete("CASCADE")
        t.Timestamps()
    })
}

func (CreateFlightsTable) Down(ctx context.Context, s *schema.Builder) error {
    return s.DropTableIfExists(ctx, "flights")
}
```

The rules:

- **The name comes from the file name.** `migrate.Register` reads the calling file's name, so there is no string to keep in sync. A file that does not match `^\d{14}_[a-z0-9_]+\.go$`, or a duplicate, is reported when migrations load (`MIGRATION_FAILED`). Do not rename a file after it has run: it would count as a new migration.
- **The Go type name must be unique in the package.** `make:migration` derives it from the file name and refuses duplicates.
- **Order is by name**, so the timestamp prefix decides it. `make:migration` always gives a new file a timestamp later than the newest existing one.
- **The package must be linked into whatever runs the migrations.** The `migro` command does that for you; in your own `main`, import it for its side effects: `import _ "example.com/app/database/migrations"`.

`s.DB()` is the `*migro.DB` behind the builder, for a migration that also moves data (`query.Table(s.DB(), "users").Update(...)`).

#### Setting the migration connection

A migration that belongs to another connection says so (Laravel's `$connection`):

```go
func (CreateEventsTable) Connection() string { return "analytics" }
```

`migro migration:run` then skips it, and `migro migration:run --database=analytics` runs only the migrations pinned to `analytics`. See [Several databases](command-line.md#several-databases).

#### Skipping migrations

Laravel's `shouldRun()` has no counterpart. To make a migration conditional, check inside `Up` and return nil to do nothing:

```go
func (CreateFlightsTable) Up(ctx context.Context, s *schema.Builder) error {
    if !features.Enabled("flights") {
        return nil
    }
    ...
}
```

The migration is still recorded as run, so it will not run later when the flag turns on. Put the feature behind a new migration instead if it must.

#### Running without a transaction

Each migration runs in a transaction where the database allows it. A statement that cannot run in a transaction (PostgreSQL's `CREATE INDEX CONCURRENTLY`, for instance) needs the migration to opt out:

```go
func (AddSearchIndex) DisableTransaction() bool { return true }
```

#### Other forms

Migrations written as values still work: `m.Add(migrate.Migration{Name: "...", Up: fn, Down: fn})`. SQL files load with `migrate.FromFS(embeddedFS, "migrations")`, as pairs of `<name>.up.sql` and `<name>.down.sql`. SQL-file migrations get a checksum, and editing one after it ran fails with `MIGRATION_CHECKSUM_MISMATCH`.

## Running migrations

```
migro migration:run       # or: migro migrate
```

runs every pending migration, as one batch. To see which have run:

```
migro migration:status
```

```
Ran?  Migration                                                    Batch
Yes   20260101120000_create_flights_table                          1
No    20260102090000_create_pilots_table
```

`--step` gives each migration its own batch, so each can be rolled back on its own:

```
migro migration:run --step
```

`--pretend` prints the SQL the migrations would run, and changes nothing:

```
migro migration:run --pretend
```

In pretend mode, every write statement is recorded instead of run, including statements issued through `s.DB()`. Reads still run, so a migration can inspect the database. A statement that must return rows (`INSERT ... RETURNING`) cannot be emulated and fails the pretend run. Nothing is created or locked, not even the tracking tables.

`--seed` runs the seeders after the migrations.

#### Isolating migration execution

Laravel's `--isolated` takes a lock so that two servers deploying at once do not both migrate. migro always does: the tracking tables include a lock, and a second runner fails with `MIGRATION_LOCKED` instead of running at the same time. The lock lives in the database itself, so no shared cache is needed.

If a run is killed while it holds the lock, release it:

```
migro migration:unlock
```

#### Forcing migrations to run in production

When `MIGRO_ENV` (or `APP_ENV`) is `production`, commands that change the database ask for confirmation in a terminal, and refuse with `PRODUCTION_BLOCKED` when there is none (CI, pipes). `--force` runs them without asking:

```
migro migration:run --force
```

## Rolling back migrations

```
migro migration:rollback             # the last batch, which may be several files
migro migration:rollback --step=5    # the five most recent migrations
migro migration:rollback --batch=3   # everything after batch 3 (0 = everything)
migro migration:rollback --pretend   # print the SQL only
migro migration:reset                # every migration
```

Laravel's `--batch=3` rolls back batch 3 itself; migro's rolls back the batches after 3, which leaves the database as it was after batch 3.

#### Roll back and migrate using a single command

```
migro migration:refresh          # roll back everything, then migrate
migro migration:refresh --seed   # ... and seed
```

#### Drop all tables and migrate

```
migro migration:fresh            # DROP every table, then migrate
migro migration:fresh --seed
migro migration:fresh --database=admin
```

{: .warning }
`migration:fresh` drops **every** table in the database, whatever its prefix and whether migro created it. Be careful on a database shared with other applications. `migro db:wipe` drops them all without migrating.

## Running migrations from Go

The commands are thin wrappers over `migrate.Migrator`, which you can use at startup or in tests:

```go
m := migrate.New(db)
if err := m.AddRegistered(); err != nil { // everything registered with migrate.Register
    return err
}

res, err := m.Latest(ctx)                                 // all pending, as one batch
res, err = m.Run(ctx, migrate.RunOptions{Step: true})     // each in its own batch
res, err = m.Run(ctx, migrate.RunOptions{Pretend: true})  // res.SQL[name] holds the SQL
res, err = m.Rollback(ctx)                                // the last batch
res, err = m.RollbackSteps(ctx, 2)                        // the 2 most recent migrations
res, err = m.RollbackToBatch(ctx, 1)                      // everything after batch 1
res, err = m.Reset(ctx)                                   // everything
res, err = m.Refresh(ctx)                                 // Reset + Latest
res, err = m.Fresh(ctx)                                   // DROP every table, then Latest

status, err := m.Status(ctx)   // []Status{Name, Applied, Batch, AppliedAt}
err = m.Install(ctx)           // create the tracking tables now (otherwise done on demand)
err = m.Unlock(ctx)            // clear a stale lock
```

`Result` lists the migrations a call applied or reverted (`Names`) and their `Batch`. Tracking lives in `migro_migrations`; `migrate.WithTable("schema_migrations")` changes the name.

## Migration behavior

- Each migration runs in a transaction when the database supports transactional DDL: PostgreSQL, SQLite, SQL Server and Redshift. **MySQL, MariaDB, Oracle and CockroachDB commit DDL implicitly**, so a migration that fails halfway there leaves its earlier statements applied. Keep migrations small on those databases.
- A migration's error stops the run; the migrations before it in the batch stay applied.
- Laravel's migrations table and migro's are different, so a Laravel database is not "migrated" for migro. Port the schema into a first migration, or a [schema dump](schema-dump.md).

## Events

Laravel dispatches an event for each step. Pass an observer to see them:

```go
m := migrate.New(db, migrate.WithObserver(func(e migrate.Event) {
    switch e.Kind {
    case migrate.Migrating, migrate.Reverting:
        log.Printf("%s %s", e.Kind, e.Name)
    case migrate.Migrated, migrate.Reverted:
        log.Printf("%s %s (%s)", e.Kind, e.Name, e.Duration)
    }
}))
```

| Kind | Laravel event |
|---|---|
| `migrate.Migrating` | `MigrationStarted` (up) |
| `migrate.Migrated` | `MigrationEnded` (up), with the duration |
| `migrate.Reverting` | `MigrationStarted` (down) |
| `migrate.Reverted` | `MigrationEnded` (down), with the duration |

A failed migration is the error the call returns (`MigrationFailed`); an empty `Result.Names` means there was nothing to run (`NoPendingMigrations`).
