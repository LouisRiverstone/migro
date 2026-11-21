---
title: "Database: getting started"
nav_order: 5
description: "Open a database, DSNs per dialect, read and write connections, raw SQL, several connections, query events, inspecting and monitoring."
---

# Database: getting started

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

Almost every application talks to a database. migro gives you three ways to do it, the same three Laravel has:

- **raw SQL**, through the GORM handle every connection carries;
- the **[query builder](query-builder.md)**, for tables without a model (`query.Table(db, "users")`);
- **[models](models.md)**, Eloquent-style (`orm.New[User](db)`).

All three run on a `*migro.DB`, which wraps a `*gorm.DB` and knows its dialect. migro supports eight databases:

| Database | Dialect name(s) | Import |
|---|---|---|
| PostgreSQL | `postgres` | `github.com/LouisRiverstone/migro/dialect/postgres` |
| CockroachDB | `cockroachdb` | same package |
| Redshift | `redshift` | same package |
| MySQL | `mysql` | `github.com/LouisRiverstone/migro/dialect/mysql` |
| MariaDB | `mariadb` | same package |
| SQLite | `sqlite`, `sqlite3` | `github.com/LouisRiverstone/migro/dialect/sqlite` (pure Go, no cgo) |
| SQL Server | `sqlserver` | `github.com/LouisRiverstone/migro/dialect/sqlserver` |
| Oracle | `oracle` | `github.com/LouisRiverstone/migro/dialect/oracle` (pure Go) |

[Database notes](databases.md) lists what is verified against live servers and what each database supports.

### Configuration

Laravel reads `config/database.php`. migro takes a `migro.Config`, usually built from environment variables. Import the dialects you use: each registers itself, so drivers you do not import are not compiled into your binary.

```go
import (
    "github.com/LouisRiverstone/migro"
    _ "github.com/LouisRiverstone/migro/dialect/postgres"
)

db, err := migro.Open(migro.Config{
    Dialect: "postgres",
    DSN:     os.Getenv("DATABASE_URL"),
})
if err != nil {
    return err // a *migro.Error: UNSUPPORTED_DIALECT, CONNECTION_FAILED, INVALID_CONFIG
}
defer db.Close()
```

`migro.OpenContext(ctx, cfg)` does the same with a context that bounds the first connection. `db.Ping(ctx)` checks that the database still answers, for a health check.

| Field | Meaning |
|---|---|
| `Dialect` | One of the names above |
| `DSN` | The driver's connection string, see below |
| `Pool` | Connection pool: `Min`, `Max`, `AcquireTimeout`, `IdleTimeout`, `MaxLifetime`, `Validate`, `AfterCreate` ([Pool, replicas, sharding](scaling.md#pool)) |
| `Replicas`, `ReplicaPolicy` | Read connections ([below](#read-and-write-connections)) |
| `Gorm` | A `*gorm.Config` of your own; nil uses GORM's defaults with a silent logger |

The `migro` command line reads the same settings from `MIGRO_DIALECT` and `MIGRO_DSN`, or from `.env` (see [Command line](command-line.md#configuration)).

#### DSN formats

| Dialect | DSN |
|---|---|
| PostgreSQL, CockroachDB, Redshift | `host=localhost user=app password=secret dbname=app port=5432 sslmode=disable`, or `postgres://app:secret@localhost:5432/app?sslmode=disable` |
| MySQL, MariaDB | `app:secret@tcp(127.0.0.1:3306)/app?parseTime=true` |
| SQLite | `database.sqlite`, `file::memory:?cache=shared` |
| SQL Server | `sqlserver://app:secret@localhost:1433?database=app` |
| Oracle | `oracle://app:secret@localhost:1521/service_name` |

Laravel's `DB_URL` is the URL form of the DSN, where the driver accepts one (PostgreSQL, SQL Server, Oracle).

#### SQLite configuration

The database is a file, created on first use. Pass its path as the DSN, or `file::memory:?cache=shared` for an in-memory database that lives as long as the pool. Laravel turns SQLite's foreign keys on by default; with the driver, add the pragma to the DSN:

```
database.sqlite?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)
```

`busy_timeout` makes concurrent writers wait instead of failing with "database is locked".

#### MySQL configuration

Add `parseTime=true` so `DATETIME` columns scan into `time.Time`, and `charset=utf8mb4` for full Unicode. MySQL's DDL is not transactional: see [Database notes](databases.md#per-database-notes).

#### SQL Server and Oracle configuration

Both drivers are pure Go, so no ODBC driver or Oracle client is needed. Oracle upper-cases unquoted identifiers; migro quotes them consistently.

### Read and write connections

To send `SELECT`s to replicas and everything else to the primary, list the replicas:

```go
db, err := migro.Open(migro.Config{
    Dialect:       "postgres",
    DSN:           primaryDSN,
    Replicas:      []string{replica1DSN, replica2DSN},
    ReplicaPolicy: "round-robin", // or "random", the default
})
```

Reads go to a replica, writes and transactions to the primary. Each DSN carries its own host, credentials and options.

#### The sticky option

A replica can lag behind the primary, so a read right after a write may not see it. Laravel's `sticky` sends the rest of a request's reads to the primary once the request has written. In migro, mark the request's context:

```go
ctx = migro.Sticky(ctx)               // once, where the request or job starts
orm.New[Post](db).Create(ctx, post)   // a write
orm.New[Post](db).Find(ctx, post.ID)  // read on the primary: the new row is there
```

Pass the returned context to every call of the unit of work. Other contexts are not affected.

#### Using the write connection for reads

`db.WriteDB()` returns a handle that always uses the primary, for a read that must be current (Laravel's `usingWritePdo`):

```go
user, err := orm.New[User](db.WriteDB()).Find(ctx, id)
```

[Pool, replicas, sharding](scaling.md) covers pool options and key-based sharding.

## Running SQL queries

For SQL that the other layers do not express, use the GORM handle, `db.Gorm`. Always pass values as arguments: they are bound as parameters, which protects you from SQL injection. Write `?` placeholders on every dialect; GORM rewrites them for PostgreSQL, SQL Server and Oracle.

#### Running a select query

```go
var users []User
err := db.Gorm.WithContext(ctx).Raw("SELECT * FROM users WHERE active = ?", true).Scan(&users).Error

var rows []map[string]any
err = db.Gorm.WithContext(ctx).Raw("SELECT id, email FROM users WHERE id > ?", 100).Scan(&rows).Error
```

#### Selecting scalar values

```go
var burgers int64
err := db.Gorm.WithContext(ctx).Raw("SELECT count(*) FROM menu WHERE burger = ?", true).Scan(&burgers).Error
```

#### Using named bindings

```go
err := db.Gorm.WithContext(ctx).
    Raw("SELECT * FROM users WHERE id = @id", sql.Named("id", 1)).
    Scan(&users).Error
```

#### Running insert, update and delete statements

`Exec` runs a statement that returns no rows. `RowsAffected` tells you how many rows it touched:

```go
err := db.Gorm.WithContext(ctx).Exec("INSERT INTO users (id, name) VALUES (?, ?)", 1, "Marc").Error

res := db.Gorm.WithContext(ctx).Exec("UPDATE users SET votes = 100 WHERE name = ?", "Anita")
affected, err := res.RowsAffected, res.Error

err = db.Gorm.WithContext(ctx).Exec("DELETE FROM users WHERE id = ?", 1).Error
```

#### Running a general statement

```go
err := db.Gorm.WithContext(ctx).Exec("DROP TABLE users").Error
```

Never build statements from user input. If an identifier must vary, check it against a list of allowed names first.

#### Implicit commits

On MySQL and MariaDB, DDL statements such as `CREATE TABLE` commit the current transaction implicitly. Run one inside `db.Transaction` and the work before it is committed, beyond the reach of a rollback. Keep DDL in [migrations](migrations.md), which account for this.

### Using multiple database connections

`migro.Manager` holds several databases by name and opens each the first time it is used (Laravel's `DB::connection('audit')`):

```go
mgr, err := migro.NewManager("main", map[string]migro.Config{
    "main":  {Dialect: "postgres", DSN: mainDSN},
    "audit": {Dialect: "postgres", DSN: auditDSN, Replicas: []string{auditReplicaDSN}},
})
defer mgr.Close()

db, err := mgr.Connection(ctx, "audit") // "" is the default
main, err := mgr.Default(ctx)
mgr.Names()                            // every configured name
```

A model can say where it lives, and `orm.Using` follows it:

```go
func (AuditLog) Connection() string { return "audit" }

logs, err := orm.Using[AuditLog](ctx, mgr) // a repository on the "audit" connection
```

`mgr.Purge("audit")` closes a connection, and the next call reopens it. Each connection has its own pool, replicas, listeners and strict settings. For many identical databases routed by key, use [sharding](scaling.md#sharding) instead.

#### Using GORM directly

`db.Gorm` is the underlying `*gorm.DB` and `db.Dialect` its dialect, so everything GORM does is still available. `migro.Adopt(gormDB, dialect)` wraps a `*gorm.DB` you already have. `db.WithGorm(g)` returns a `*migro.DB` around another GORM session (from `Debug()`, `Session(...)`, ...) that keeps the dialect and the listeners.

### Listening for query events

`db.Listen` runs a function after every statement (Laravel's `DB::listen`), for logging or metrics:

```go
off := db.Listen(func(e migro.QueryEvent) {
    slog.Debug("query", "sql", e.SQL, "rows", e.Rows, "took", e.Duration, "err", e.Err)
})
defer off()
```

`QueryEvent` carries `SQL` with its bindings inlined (what Laravel's `toRawSql()` gives), `Rows`, `Duration` and `Err`. The listener runs on the goroutine that ran the statement, so keep it fast. Your own GORM logger keeps working alongside it.

`db.EnableQueryLog()`, `db.QueryLog()` and `db.FlushQueryLog()` keep the statements in memory instead, which is handy in tests. See [Strict mode and debugging](strict-mode.md).

### Monitoring cumulative query time

Laravel's `whenQueryingForLongerThan` warns when a unit of work spends too long in the database. A listener with a counter does the same:

```go
func Timed(db *migro.DB, limit time.Duration, warn func(total time.Duration)) (done func()) {
    var total atomic.Int64
    off := db.Listen(func(e migro.QueryEvent) { total.Add(int64(e.Duration)) })
    return func() {
        off()
        if d := time.Duration(total.Load()); d > limit {
            warn(d)
        }
    }
}
```

The listener sees every statement on `db`, so use it around work that runs alone, such as a job or a CLI command.

## Database transactions

`db.Transaction` runs a function in a transaction. It commits when the function returns nil, and rolls back when the function returns an error or panics. Every layer accepts the `tx` it hands you:

```go
err := db.Transaction(ctx, func(tx *migro.DB) error {
    if err := orm.New[User](tx).Create(ctx, &User{Email: "a@x.io"}); err != nil {
        return err // rolls back
    }
    return query.Table(tx, "audit").Insert(ctx, query.Row{"event": "user_created"})
})
```

[Transactions](transactions.md) covers nesting, retrying deadlocks, manual transactions and moving a loaded model into a transaction.

## Connecting to the database CLI

Laravel's `php artisan db` opens the database's own client. migro has no such command: run the client with the same DSN (`psql "$MIGRO_DSN"`, `mysql`, `sqlite3 database.sqlite`, `sqlcmd`).

## Inspecting your databases

```
migro db:show            # dialect, connection and tables
migro db:show --counts   # ... with row counts (slow on large tables)
migro db:table users     # columns, types and indexes of one table
migro model:show User    # a model's table, columns, relations and behaviours
```

From Go, the schema builder answers the existence questions, and GORM's migrator lists the rest (Laravel's `Schema::getTables()`, `getColumns()`, `getIndexes()`):

```go
s := schema.New(db)
ok, err := s.HasTable(ctx, "users")
ok, err = s.HasColumn(ctx, "users", "email")

m := db.Gorm.WithContext(ctx).Migrator()
tables, err := m.GetTables()
columns, err := m.ColumnTypes("users")
indexes, err := m.GetIndexes("users")
```

## Monitoring your databases

Laravel's `db:monitor` alerts when a database has too many open connections. `db.Gorm.DB()` returns the `*sql.DB`, whose `Stats()` reports `OpenConnections`, `InUse`, `Idle` and `WaitCount`. Export them to your metrics, or check them on a schedule:

```go
sqlDB, _ := db.Gorm.DB()
if s := sqlDB.Stats(); s.OpenConnections > 100 {
    alert("database %s has %d open connections", db.Name(), s.OpenConnections)
}
```
