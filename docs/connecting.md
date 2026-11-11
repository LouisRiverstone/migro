---
title: Connecting
nav_order: 5
description: Open a database with migro.Open, DSN formats per database.
---

# Connecting

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

Almost every application talks to a database. migro opens it with `migro.Open`, which returns a `*migro.DB` that every other layer (schema, query builder, models, migrations, seeders) accepts. It wraps a `*gorm.DB`, so anything GORM can do is still available.

## Configuration

Import the dialect packages you need. Each registers itself, so unused drivers are not compiled in.

```go
import (
    "github.com/LouisRiverstone/migro"
    _ "github.com/LouisRiverstone/migro/dialect/postgres"
)

db, err := migro.Open(migro.Config{
    Dialect: "postgres",
    DSN:     "host=localhost user=postgres password=postgres dbname=app port=5432 sslmode=disable",
})
if err != nil { /* *migro.Error */ }
defer db.Close()
```

| Dialect name(s) | Import | DSN example |
|---|---|---|
| `postgres`, `cockroachdb`, `redshift` | `dialect/postgres` | `host=… user=… password=… dbname=… port=5432 sslmode=disable` (or a `postgres://` URL) |
| `mysql`, `mariadb` | `dialect/mysql` | `user:pass@tcp(127.0.0.1:3306)/app?parseTime=true` |
| `sqlite` | `dialect/sqlite` | `file.db` or `file::memory:?cache=shared` |
| `sqlserver` | `dialect/sqlserver` | `sqlserver://user:pass@localhost:1433?database=app` |
| `oracle` | `dialect/oracle` | `oracle://user:pass@host:1521/service_name` |

### Supported databases

The table above lists each dialect, the package to import and a sample DSN.

### Using GORM directly

`db.Gorm` is the underlying `*gorm.DB` and `db.Dialect` the dialect, so anything GORM can do is still available. `migro.Open` uses a silent GORM logger unless you pass your own `Config.Gorm`.

### Adopting an existing connection

To adopt a `*gorm.DB` you already have: `migro.Adopt(gormDB, dialect)`.

### Named connections

`migro.Manager` holds several databases by name and opens each the first time it is used (Laravel's `DB::connection`):

```go
mgr, err := migro.NewManager("main", map[string]migro.Config{
    "main":  {Dialect: "postgres", DSN: mainDSN},
    "audit": {Dialect: "postgres", DSN: auditDSN, Replicas: []string{auditReplicaDSN}},
})
defer mgr.Close()

db, err := mgr.Connection(ctx, "audit")   // "" means the default
```

A model can say where it lives, and `orm.Using` follows it:

```go
func (AuditLog) Connection() string { return "audit" }

logs, err := orm.Using[AuditLog](ctx, mgr)   // a repository on the "audit" connection
```

`mgr.Purge("audit")` closes a connection so the next call reopens it. Each connection keeps its own pool, replicas and events. For key-based routing across identical databases use [sharding](scaling.html); the manager is for databases that differ.

### Joining a transaction

A model loaded outside a transaction can move into one (Lucid's `useTransaction`):

```go
err := db.Transaction(ctx, func(tx *migro.DB) error {
    user.UseTransaction(tx)
    user.Name = "Ana"
    return user.Save(ctx)   // inside tx
})
```
