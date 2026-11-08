# migro

A Go toolkit inspired by [AdonisJS Lucid](https://github.com/adonisjs/lucid) and [Knex](https://github.com/knex/knex), built on [GORM](https://gorm.io):

- **`schema`** – Knex-style DDL builder, compiled per dialect
- **`query`** – Knex-style fluent query builder for tables without a model
- **`orm`** – Lucid-style generic models: `Find`, `Query().Where().Preload().Paginate()`, soft deletes, `FirstOrCreate`
- **`migrate`** – batch-based migrations with a lock table, checksums, Go or `.sql` file sources
- **`cmd/migro`** – CLI for SQL-file migrations

## Databases

| Dialect names | Driver | Notes |
|---|---|---|
| `postgres` | gorm.io/driver/postgres | |
| `cockroachdb` | postgres driver | DDL runs outside transactions |
| `redshift` | postgres driver (simple protocol) | no `CREATE INDEX`, `IDENTITY` keys, `SUPER` for JSON |
| `mysql`, `mariadb` | gorm.io/driver/mysql | DDL auto-commits |
| `sqlite` | github.com/glebarez/sqlite (pure Go, no cgo) | no `ADD CONSTRAINT` / foreign keys via `AlterTable` |
| `sqlserver` | gorm.io/driver/sqlserver | |
| `oracle` | go-ora via godoes/gorm-oracle (pure Go) | DDL auto-commits; identifiers upper-cased; no `TIME` type (stored as `VARCHAR2(15)`) |

Import only the dialects you use; each registers itself:

```go
import _ "github.com/LouisRiverstone/migro/dialect/postgres"
```

**Test coverage:** SQLite runs the full suite in CI (blocking). Postgres and MySQL run the same suite against service containers in non-blocking CI jobs that have not been verified green yet. SQL generation for every dialect is covered by golden-fragment tests, but CockroachDB, Redshift, SQL Server, Oracle and MariaDB have **not** been run against live servers. Treat them as unverified and report issues.

## Usage

```go
db, err := migro.Open(migro.Config{Dialect: "postgres", DSN: dsn})

// Schema
s := schema.New(db)
err = s.CreateTable(ctx, "users", func(t *schema.Table) {
    t.Increments("id")
    t.String("email", 120).NotNull().Unique()
    t.Enum("role", "admin", "user").Default("user")
    t.Timestamps()
    t.SoftDeletes()
})

// Models
type User struct {
    orm.SoftDeleteModel
    Email string
    Posts []Post
}
users := orm.New[User](db)
u, err := users.Find(ctx, 1)                       // errors.Is(err, migro.ErrNotFound)
page, err := users.Query().Where("email LIKE ?", "%@x.io").Preload("Posts").OrderBy("id", "desc").Paginate(ctx, 1, 20)

// Query builder
rows, err := query.Table(db, "users").Where("id > ?", 10).Limit(5).Get(ctx)

// Transactions work with every layer
err = db.Transaction(ctx, func(tx *migro.DB) error {
    return orm.New[User](tx).Create(ctx, &User{Email: "a@x.io"})
})
```

### Migrations

```go
m := migrate.New(db)
m.Add(migrate.Migration{
    Name: "20260101000000_create_users",
    Up:   func(ctx context.Context, s *schema.Builder) error { return s.CreateTable(ctx, "users", ...) },
    Down: func(ctx context.Context, s *schema.Builder) error { return s.DropTable(ctx, "users") },
})
res, err := m.Latest(ctx)    // one batch
res, err = m.Rollback(ctx)   // last batch
```

SQL files (`<name>.up.sql` / `<name>.down.sql`) load with `migrate.FromFS(fsys, dir)` (works with `embed.FS`) or the CLI:

```
migro -dialect postgres -dsn "$DSN" make create_users
migro -dialect postgres -dsn "$DSN" up | rollback | reset | status | unlock
```

- Migrations run in a transaction when the dialect supports transactional DDL; on MySQL, Oracle and CockroachDB a failed migration can leave partial changes.
- A lock row prevents concurrent runners (`MIGRATION_LOCKED`); `Unlock` clears a stale lock.
- SQL-file migrations record a SHA-256 checksum; editing an applied migration returns `MIGRATION_CHECKSUM_MISMATCH`.

## Errors

All failures are `*migro.Error` with a stable `Code` (`NOT_FOUND`, `QUERY_FAILED`, `MIGRATION_LOCKED`, `MIGRATION_FAILED`, `MIGRATION_CHECKSUM_MISMATCH`, `INVALID_SCHEMA`, `UNSUPPORTED_FEATURE`, `UNSUPPORTED_DIALECT`, `CONNECTION_FAILED`). Use `errors.Is(err, migro.ErrNotFound)` or `migro.CodeOf(err)`.

## Safety

Values are always bound; identifiers are quoted by the dialect. `DefaultRaw`, `Raw`, `Where` fragments and `Join` clauses take SQL text and must come from trusted code, never user input. `Update`/`Delete` without a `Where` return an error.

## Not included (yet)

Seeders/factories, column type changes in `AlterTable`, an event bus (use GORM hooks), read replicas.
