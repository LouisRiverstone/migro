---
title: Seeding
nav_order: 14
description: "Write seeders, call them from a DatabaseSeeder, use factories, mute model events, run them once or per environment."
---

# Seeding

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

migro can fill your database with data using seeders. Seeders live in `database/seeders` (`MIGRO_SEEDERS_DIR`). A `DatabaseSeeder` is the entry point: it calls the other seeders, in the order you choose.

Models built from structs are not subject to mass assignment protection, so seeders can set any field, like Laravel's, which disables that protection while seeding.

## Writing seeders

```
migro make:seeder UserSeeder
```

creates `database/seeders/user_seeder.go`. A seeder is a type with a `Run` method, registered when the package loads. `db:seed` calls `Run`; insert data with the query builder or with models:

```go
// database/seeders/user_seeder.go
package seeders

func init() { seed.Register(&UserSeeder{}) }

type UserSeeder struct{}

func (UserSeeder) Run(ctx context.Context, db *migro.DB) error {
    hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
    if err != nil {
        return err
    }
    return query.Table(db, "users").Insert(ctx, query.Row{
        "name":     seed.NewFaker(time.Now().UnixNano()).Name(),
        "email":    "admin@example.com",
        "password": string(hash),
    })
}
```

Laravel injects dependencies into `run()` from the container. In Go, give the seeder fields and register a value that has them: `seed.Register(&UserSeeder{Mailer: mailer})`.

{: .note }
Without `--once`, seeders run every time you call `db:seed`, as Lucid's do. Make them idempotent: `UpdateOrCreateMany` (below) inserts or updates by key, so a second run changes nothing.

```go
users, err := orm.New[User](db).UpdateOrCreateMany(ctx, []string{"email"}, []User{
    {Email: "ana@example.com", Name: "Ana"},
    {Email: "bob@example.com", Name: "Bob"},
})
_, err = users[0].Posts.Create(ctx, map[string]any{"title": "Hello"}) // relations work in seeders
```

### Using model factories

Writing every row by hand is tedious. [Factories](factories.md) generate them:

```go
func (UserSeeder) Run(ctx context.Context, db *migro.DB) error {
    _, err := factories.UserFactory().With("posts", 1).CreateMany(ctx, db, 50)
    return err
}
```

creates 50 users, each with one post.

### Calling additional seeders

Registered seeders run in name order, which ignores foreign keys. As in Laravel, a `DatabaseSeeder` calls the others in the right order with `seed.Call`:

```go
type DatabaseSeeder struct{}

func init() { seed.Register(&DatabaseSeeder{}) }

func (DatabaseSeeder) Run(ctx context.Context, db *migro.DB) error {
    return seed.Call(ctx, db, &CountrySeeder{}, &UserSeeder{}, &PostSeeder{}, &CommentSeeder{})
}
```

`seed.Call` stops at the first error and names the seeder that failed. The whole `DatabaseSeeder` runs in one transaction, so a failure in the third seeder rolls back the first two.

### Muting model events

`seed.WithoutEvents()` mutes [model events](events.md) while seeding (Laravel's `WithoutModelEvents`), including in the seeders `seed.Call` runs:

```go
r := seed.NewRunner(db, seed.WithoutEvents())
```

### Limiting seeders to environments

A seeder can say where it runs:

```go
func (DemoDataSeeder) Environments() []string { return []string{"development", "staging"} }
```

A seeder with no `Environments` runs everywhere **except production**; list `"production"` to allow it there. The environment comes from `MIGRO_ENV`, then `APP_ENV`, then `development`.

### SQL seed files

Seed data kept as SQL loads with `seed.FromFS(embeddedFS, "seeders")`, one seeder per `.sql` file.

## Running seeders

```
migro db:seed                          # DatabaseSeeder if registered, otherwise every seeder by name
migro db:seed --class=UserSeeder       # one seeder
migro db:seed --once                   # skip the seeders that already ran (tracked in migro_seeds)
migro migration:fresh --seed           # drop every table, migrate, seed
migro migration:refresh --seed
```

Each top-level seeder runs in its own transaction, so a failure rolls back that seeder only.

From Go:

```go
r := seed.NewRunner(db, seed.Once(), seed.WithEnvironment("staging"))
if err := r.AddRegistered(); err != nil {
    return err
}
res, err := r.Seed(ctx)             // DatabaseSeeder, or every seeder
res, err = r.RunOnly(ctx, "UserSeeder")
res, err = r.Run(ctx)               // every seeder, by name
r.Add(seed.Seeder{Name: "Demo", Run: func(ctx context.Context, db *migro.DB) error { ... }})
```

#### Forcing seeders to run in production

In production (`MIGRO_ENV=production`), `db:seed` asks for confirmation in a terminal, and refuses with `PRODUCTION_BLOCKED` when there is none. `--force` runs it without asking. Seeders still have to allow `"production"` in their `Environments`.

```
migro db:seed --force
```
