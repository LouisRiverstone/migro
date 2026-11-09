---
title: Connecting
nav_order: 5
description: Open a database with migro.Open, DSN formats per database.
---

# Connecting

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

`db.Gorm` is the underlying `*gorm.DB` and `db.Dialect` the dialect, so anything GORM can do is still available. `migro.Open` uses a silent GORM logger unless you pass your own `Config.Gorm`.

To adopt a `*gorm.DB` you already have: `migro.Adopt(gormDB, dialect)`.
