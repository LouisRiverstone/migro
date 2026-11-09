---
title: Testing your code
nav_order: 19
description: Testing application code that uses migro.
---

# Testing your code

SQLite makes a fast test database. A file in `t.TempDir()` avoids the "each connection gets its own `:memory:` database" trap:

```go
func newTestDB(t *testing.T) *migro.DB {
    db, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
    if err != nil { t.Fatal(err) }
    t.Cleanup(func() { _ = db.Close() })
    return db
}
```

Run your migrations in the test with `migrate.New(db)` and use factories to build data. Because dialect SQL differs, also run the suite against your production database type in CI.
