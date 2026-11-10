---
title: Testing your code
nav_order: 19
description: Testing application code that uses migro.
---

# Testing your code

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

migro makes it simple to test code that touches the database: a throwaway database, migrations, and factories that build the data. This page is the equivalent of Laravel's *Database Testing*.

## Creating a test database

SQLite makes a fast test database. A file in `t.TempDir()` avoids the "each connection gets its own `:memory:` database" trap:

```go
func newTestDB(t *testing.T) *migro.DB {
    db, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
    if err != nil { t.Fatal(err) }
    t.Cleanup(func() { _ = db.Close() })
    return db
}
```

## Migrating and seeding

Run your migrations in the test with `migrate.New(db)` and use factories to build data. Because dialect SQL differs, also run the suite against your production database type in CI.

### Testing with the whole pipeline

To test against a realistic schema, register your migrations and seeders by importing their packages, then migrate and seed in the test:

```go
import (
    _ "example.com/shop/database/migrations"   // init() registers each migration
    _ "example.com/shop/database/seeders"      // init() registers each seeder
)

func seededDB(t *testing.T) *migro.DB {
    db := newTestDB(t)
    m := migrate.New(db)
    if err := m.AddRegistered(); err != nil { t.Fatal(err) }
    if _, err := m.Latest(ctx); err != nil { t.Fatal(err) }

    r := seed.NewRunner(db, seed.WithEnvironment("testing"))
    if err := r.AddRegistered(); err != nil { t.Fatal(err) }
    if _, err := r.Seed(ctx); err != nil { t.Fatal(err) }   // DatabaseSeeder, or every seeder by name
    return db
}
```

Two traps:

- **SQLite does not enforce foreign keys by default.** Without it, tests that expect a constraint error pass vacuously. Open the file with the pragma in the DSN: `Dialect: "sqlite", DSN: path + "?_pragma=foreign_keys(1)"`.
- **Registered seeders run in name order when there is no `DatabaseSeeder`**, which ignores foreign keys. Start from the root seeder (`r.Seed(ctx)`) or pick one with `r.RunOnly(ctx, "UserSeeder")`.

The repository's own [`integration/`](https://github.com/LouisRiverstone/migro/tree/main/integration) suite is a worked example: 16 migrations, class seeders, factories and models, with tests for constraints, referential actions, every relation kind, rollbacks and seeder atomicity. It runs on SQLite, PostgreSQL and MySQL.
