# migro

A Go toolkit inspired by [AdonisJS Lucid](https://github.com/adonisjs/lucid) and [Knex](https://github.com/knex/knex), built on [GORM](https://gorm.io):

- **`schema`** – Knex-style DDL builder, compiled per dialect
- **`query`** – Knex-style fluent query builder for tables without a model
- **`orm`** – Lucid-style generic models: `Find`, `Query().Where().Preload().Paginate()`, soft deletes, `FirstOrCreate`
- **`migrate`** – batch-based migrations with a lock table, checksums, Go or `.sql` file sources
- **`events`** – Lucid-style event bus with model lifecycle events
- **`seed`** – Lucid-style factories (states, relations, faker) and seeders
- **`cluster`** – application-level sharding across databases
- **`migrate/diff`** – generate migration files from GORM models
- **`cmd/migro`** – CLI for SQL-file migrations and seeds

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

## Connection pool (Knex-style)

```go
db, err := migro.Open(migro.Config{
    Dialect: "postgres", DSN: dsn,
    Pool: migro.Pool{
        Min: 2, Max: 10,                       // Min is kept warm (best effort), Max caps open conns
        AcquireTimeout: 3 * time.Second,       // POOL_ACQUIRE_TIMEOUT instead of waiting forever
        IdleTimeout:    5 * time.Minute,
        MaxLifetime:    time.Hour,
        Validate:       true,                  // ping a pooled connection on checkout, discard if dead
        AfterCreate: func(ctx context.Context, c migro.Conn) error { // once per new physical connection
            return c.Exec(ctx, "SET TIME ZONE 'UTC'")
        },
    },
})
```

`AcquireTimeout` only bounds the wait for a free connection; it never cancels a query or transaction that is already running. `Min` and `Validate` rely on `database/sql`, so `Validate` needs a driver that implements `driver.Pinger`.

## Read replicas and sharding

```go
db, _ := migro.Open(migro.Config{
    Dialect: "postgres", DSN: primaryDSN,
    Replicas: []string{replica1, replica2}, ReplicaPolicy: "round-robin", // or "random"
})
db.Gorm.Find(&rows)         // reads go to a replica
db.Gorm.Create(&row)        // writes and transactions go to the primary
db.WriteDB().Gorm.Find(&x)  // force the primary (read-your-writes; replicas can lag)
```

`cluster` routes keys to independent databases (tenant/user sharding):

```go
c, _ := cluster.Open(ctx, map[string]migro.Config{"s1": cfg1, "s2": cfg2}, cluster.HashRouter([]string{"s1", "s2"}))
db, _ := c.For(tenantID)             // the shard that owns this key
_, err = c.Migrate(ctx, migrations)  // same migrations on every shard
err = c.Each(ctx, func(ctx context.Context, name string, db *migro.DB) error { ... })
```

There are no cross-shard transactions: `Each` runs shards independently and returns `SHARD_FAILED` naming the ones that failed, while the others stay committed. Changing the shard list remaps most hash keys; move data first.

## Events

```go
bus := events.New()
bus.Attach(db)
events.OnModel(bus, events.BeforeCreate, func(ctx context.Context, u *User) error {
    if u.Email == "" { return errors.New("email required") } // aborts the insert (EVENT_ABORTED)
    return nil
})
bus.On("after:*", handler)   // globs; OnAny, Once, and EmitAsync/Wait are available too
bus.Emit(ctx, events.Event{Name: "user:welcomed", Payload: u}) // your own events
```

Lifecycle names: `before|after:create|update|save|delete`, `after:find`. As with GORM hooks, they do not fire for `UpdateColumn(s)`, raw SQL, or `query.Builder`. Handlers run inside the operation's transaction.

## Relations beyond GORM

```go
// Country -> Citizen -> Story
rel := orm.HasManyThrough[Story, Citizen]{ForeignKey: "country_id", ThroughForeignKey: "citizen_id"}
stories, _ := rel.Load(ctx, db, []any{1, 2})        // map[parentKey][]Story, two queries total

// polymorphic: Comment.commentable_type/commentable_id
reg := orm.NewMorphRegistry()
orm.RegisterMorph[Article](reg, db, "article")
owners, _ := reg.Load(ctx, db, []orm.Ref{{Type: "article", ID: 7}}) // map[Ref]any (*Article)

// polymorphic many-to-many: taggables(tag_id, taggable_id, taggable_type)
tags := orm.MorphToMany[Tag]{Pivot: orm.Pivot{Table: "taggables", MorphIDColumn: "taggable_id",
    MorphTypeCol: "taggable_type", RelatedIDCol: "tag_id", MorphType: "article"}}
tags.Attach(ctx, db, articleID, 1, 2); tags.Sync(ctx, db, articleID, 2, 3); tags.Detach(ctx, db, articleID)
articles, _ := orm.MorphedByMany[Article]{Pivot: tags.Pivot}.Load(ctx, db, []any{tagID})
```

## Seeders and factories

```go
users := seed.Define(func(f *seed.Faker) User { return User{Name: f.Name(), Email: f.Email()} }).
    State("admin", func(u *User) { u.Role = "admin" }).
    Relation("posts", func(ctx context.Context, db *migro.DB, u *User, n int, f *seed.Faker) error { ... })

users.Apply("admin").With("posts", 3).CreateMany(ctx, db, 10) // atomic: all or nothing
users.Seed(42).Make()                                          // reproducible, not persisted

r := seed.NewRunner(db, seed.Once())   // Once() records runs in migro_seeds; default re-runs like Lucid
r.Add(seed.Seeder{Name: "001_users", Run: func(ctx context.Context, db *migro.DB) error { ... }})
r.Run(ctx)
```

A seeder with no `Environments` runs everywhere except `production`; list `"production"` explicitly to seed it. SQL files work too: `migro seed` (`-seeds dir`, `-once`, `-env`) and `migro seed:make name`.

## Schema diff (models → migration files)

```go
plan, _ := diff.Generate(ctx, db, diff.Options{Renames: map[string]string{"users.name": "full_name"}}, &User{}, &Post{})
paths, _ := plan.WriteFiles("migrations", "sync_models") // .up.sql + .down.sql, ready for migrate.FromFS
// plan.Warnings: NOT NULL columns without defaults, skipped default expressions, unmapped types
// plan.Blocked:  drops that were NOT written; pass AllowDestructive: true to include them
```

It detects new tables, added/removed/renamed columns and indexes. It does not detect type or nullability changes (reported as warnings where possible), never drops tables, and a dropped column's `down` migration is a comment because the data cannot be restored. Review the generated SQL before applying it.

## Errors

All failures are `*migro.Error` with a stable `Code` (`NOT_FOUND`, `QUERY_FAILED`, `MIGRATION_LOCKED`, `MIGRATION_FAILED`, `MIGRATION_CHECKSUM_MISMATCH`, `INVALID_SCHEMA`, `UNSUPPORTED_FEATURE`, `UNSUPPORTED_DIALECT`, `CONNECTION_FAILED`, `INVALID_CONFIG`, `POOL_ACQUIRE_TIMEOUT`, `EVENT_ABORTED`, `EVENT_HANDLER_FAILED`, `SEED_FAILED`, `DIFF_FAILED`, `SHARD_NOT_FOUND`, `SHARD_FAILED`, `UNKNOWN_MORPH_TYPE`). Use `errors.Is(err, migro.ErrNotFound)` or `migro.CodeOf(err)`.

## Safety

Values are always bound; identifiers are quoted by the dialect. `DefaultRaw`, `Raw`, `Where` fragments and `Join` clauses take SQL text and must come from trusted code, never user input. `Update`/`Delete` without a `Where` return an error.

## Not included

Left out on purpose because neither Lucid nor Knex has them: table partitioning/sharding inside one database, cross-shard (two-phase) transactions, replica failover, distributed or queued events, and type-change detection in the schema diff. Column type changes in `AlterTable` are also not supported.
