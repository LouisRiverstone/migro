---
title: Command line
nav_order: 12
description: "migro CLI: make:migration, migration:run, migration:rollback and friends."
---

# Command line

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

`migro` is the command-line interface included with the library, the counterpart of Laravel's Artisan and Adonis's Ace. It generates files (`make:*`) and runs migrations and seeders against your database. Run `migro help` for the list of commands.

## Installation

Install it once: `go install github.com/LouisRiverstone/migro/cmd/migro@latest`. Then, from your project (anywhere inside the Go module), with the application running or not:

```
migro make:migration create_users_table
migro migration:run
migro migration:rollback --step=2
migro migration:status
migro db:seed
```

## Configuration

`migro` reads `MIGRO_DIALECT` and `MIGRO_DSN` from the environment, or from a `.env` file in the project root (real environment variables win).

```
# .env
MIGRO_DIALECT=postgres
MIGRO_DSN=host=localhost user=app password=secret dbname=app sslmode=disable
MIGRO_ENV=development           # "production" turns on the safety prompt

# optional second database, selected with --database=analytics
MIGRO_ANALYTICS_DIALECT=mysql
MIGRO_ANALYTICS_DSN=user:pass@tcp(127.0.0.1:3306)/analytics?parseTime=true
```

Optional folders (defaults shown): `MIGRO_MIGRATIONS_DIR=database/migrations`, `MIGRO_SEEDERS_DIR=database/seeders`, `MIGRO_FACTORIES_DIR=database/factories`, `MIGRO_MODELS_DIR=app/models`.

### How it runs

`make:*` commands only write files. Every other command builds a small runner inside your module (in a temporary `_migro_run_*` folder, deleted afterwards) that imports your migrations, seeders and the database driver, then executes it. That is how your Go files get compiled in, so each command takes as long as a `go build`. If your `go.mod` lacks the driver module, `-mod=mod` lets Go add it. Add `_migro_run_*/` to `.gitignore` in case a run is killed.

To skip the build step in production, or to expose the commands from your own binary, embed the same code:

```go
// cmd/app/main.go
import (
    "os"

    "github.com/LouisRiverstone/migro"
    "github.com/LouisRiverstone/migro/console"
    _ "github.com/LouisRiverstone/migro/dialect/postgres"
    _ "example.com/app/database/migrations"
    _ "example.com/app/database/seeders"
)

func main() {
    app, _ := console.FromEnv(".")        // or build console.App yourself with your own Connections
    app.Models = []any{&models.User{}}    // only needed for migration:diff
    os.Exit(app.Main(os.Args[1:]))        // ./app migration:run
}
```

## Available commands

| Command | What it does |
|---|---|
| `make:migration <name>` | New migration file. `create_X_table` gets a `CreateTable("X")` stub and `add_..._to_X_table` an `AlterTable("X")` stub; or pass `--create=X` / `--table=X`. `--path=dir` overrides the folder. |
| `migration:run` (alias `migrate`) | Run pending migrations as one batch. `--step` gives each its own batch. `--pretend` prints the SQL and changes nothing. |
| `migration:rollback` (alias `migrate:rollback`) | Roll back the last batch. `--step=N` rolls back the N latest migrations; `--batch=N` rolls back everything after batch N (`--batch=0` = all). `--pretend` supported. |
| `migration:reset` | Roll back everything. |
| `migration:refresh` | Roll back everything, then migrate again. `--seed` runs the seeders afterwards. |
| `migration:fresh` | **Drop every table**, then migrate. `--seed` supported. |
| `migration:status` | List migrations and their batch. |
| `migration:install` | Create the tracking tables now. |
| `migration:unlock` | Release a lock left by a crashed run. |
| `migration:diff [name]` | Generate a migration from your models; see [below](schema-diff.md). |
| `db:seed` | Run seeders. Starts from `DatabaseSeeder` when you registered one (it calls the others with `seed.Call`), otherwise runs every seeder by name. `--class=UserSeeder` runs one; `--once` skips those that already ran. |
| `make:seeder <Name>` | New seeder. |
| `make:factory <Name>` | New factory. `--model=example.com/app/models.User` names the model (default: `<module>/app/models`). |
| `make:model <Name>` | New model. `-m` also creates its migration, `-f` a factory, `-s` a seeder, `-a` all three. |
| `help` | List commands. |

Every command accepts `--database=<name>` to pick a connection, and `--force` / `--no-interaction` (`-n`). Exit codes: `0` ok, `1` error, `2` bad usage.

```
$ migro migration:run
Migrating:  20260101120000_create_users_table
Migrated:   20260101120000_create_users_table (4.1ms)

$ migro migration:rollback --step=1 --pretend
-- 20260102090000_create_posts_table
DROP TABLE "posts";
```

### Production safety

When `MIGRO_ENV` (or `APP_ENV`) is `production`, commands that change data (`migration:run`, `rollback`, `reset`, `refresh`, `fresh`, `db:seed`) ask for confirmation in a terminal, and refuse with `PRODUCTION_BLOCKED` when there is none (CI, pipes). Pass `--force` to proceed without asking. `--pretend`, `migration:status` and the `make:*` commands never ask. `migration:fresh` and `migration:reset` are destructive anywhere else; there is no prompt outside production, as in Laravel.

### Several databases

Each connection has its own tracking table. By default every registered migration runs on whichever connection you select. To give a connection its own migrations, pin them:

```go
func (CreateEventsTable) Connection() string { return "analytics" }
```

`migro migration:run` then runs only unpinned migrations (on the default connection), and `migro migration:run --database=analytics` runs only those pinned to `analytics`.
