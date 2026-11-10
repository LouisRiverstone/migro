---
title: Home
nav_order: 1
description: Models and migrations for Go, with Lucid and Knex ergonomics on top of GORM.
permalink: /
---

# migro

**Models and migrations for Go.** migro brings the ergonomics of [AdonisJS Lucid](https://github.com/adonisjs/lucid), [Knex](https://github.com/knex/knex) and Laravel's artisan to Go, on top of [GORM](https://gorm.io).

You get a schema builder, a query builder, generic models, Laravel-style migration files, seeders and factories, events, read replicas and sharding, and a CLI you run next to your application:

```
migro make:migration create_users_table
migro migration:run
migro migration:rollback --step=2
migro db:seed
```

## What is in the box

| Piece | What it gives you |
|---|---|
| [Schema builder](schema-builder.md) | `t.String("email").Unique()`, indexes, `ForeignID(...).Constrained()`, `Morphs(...)`, one definition for every database |
| [Query builder](query-builder.md) | Knex-style `query.Table(db, "users").Where(...).Get(ctx)` for tables without a model |
| [Models](models.md) | Eloquent and Lucid style: `orm.New[User](db)`, instance `Save`/`Delete`, dirty tracking, mass assignment, serialization, aggregates |
| [Relations](relations.md) | Typed relation fields (`HasMany[Post]`, `BelongsToMany[Role]`, `MorphTo`, ...), `With`/`WithCount`/`Has`, pivot `Attach`/`Sync`, morph map |
| [Query scopes](query-scopes.md) | Reusable `Apply(Published, By(1))` local scopes and model-level global scopes |
| [Mutators and casting](mutators-casting.md) | Hashed, encrypted, enum, date and typed JSON columns; custom mutators and accessors |
| [Serialization](serialization.md) and [Resources](resources.md) | `ToMap`/`JSON` with `MakeVisible`/`DateFormat`, and hand-shaped API resources with pagination |
| [Collections](collections.md) | `Filter`, `KeyBy`, `GroupBy`, `Load`, `Fresh` on every multi-row result |
| [Migrations](migrations.md) | Files like `20260101120000_create_users_table.go` with `Up` and `Down`, batches, locking, `--pretend` |
| [Command line](command-line.md) | `make:*`, `migration:*`, `db:seed`, production prompt, several connections |
| [Seeders and factories](seeding.md) | Lucid-style factories with states and relations, environment-aware seeders |
| [Events](events.md) | An event bus plus `before:create`, `after:update` and other model lifecycle events |
| [Pool, replicas, sharding](scaling.md) | Knex-style pool options, read replicas, key-based sharding |
| [Schema diff](schema-diff.md) | Generate a migration from the difference between your models and the database |

## Databases

PostgreSQL, CockroachDB, Redshift, MySQL, MariaDB, SQLite (pure Go), SQL Server and Oracle (pure Go). Each is a separate import, so you only compile the driver you use. See [Database notes](databases.md) for what is verified against live servers and what is not.

## Where to start

1. [Installation](installation.md): requirements and the two things to install.
2. [Getting started](getting-started.md): a small project from an empty folder to a seeded database, about ten minutes.
3. [How it works](how-it-works.md): the mental model, so the rest of the guide makes sense.

Then pick the page you need from the sidebar. If you want to help, read [Contributing](contributing.md).

{: .note }
migro is young. There are no tagged releases yet, so `@latest` means the newest commit on `main`. Pin a commit in `go.mod` if you need reproducible builds.
