# migro usage guide

migro is a toolkit on top of [GORM](https://gorm.io), inspired by AdonisJS Lucid and Knex. This guide walks through each piece with working examples. For a short overview see the [README](../README.md).

- [Install](#install)
- [Connect](#connect)
- [Schema builder](#schema-builder)
- [Query builder](#query-builder)
- [Models](#models)
- [Relations GORM lacks](#relations-gorm-lacks)
- [Transactions](#transactions)
- [Migrations](#migrations)
- [Command line](#command-line-artisan--ace-style)
- [Generate migrations from models](#generate-migrations-from-models)
- [Seeders and factories](#seeders-and-factories)
- [Events](#events)
- [Pool, replicas and sharding](#pool-replicas-and-sharding)
- [Errors](#errors)
- [Database notes](#database-notes)

## Install

```
go get github.com/LouisRiverstone/migro
```

Requires Go 1.22+. Install the CLI with `go install github.com/LouisRiverstone/migro/cmd/migro@latest`.

## Connect

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

## Schema builder

Knex-style DDL, compiled per database.

```go
s := schema.New(db)

err := s.CreateTable(ctx, "users", func(t *schema.Table) {
    t.Increments("id")                                  // auto-increment primary key
    t.String("email", 120).NotNull().Unique()
    t.String("name")                                    // varchar(255) by default
    t.Boolean("active").NotNull().Default(true)
    t.Enum("role", "admin", "user").Default("user")     // native on MySQL, varchar+CHECK elsewhere
    t.Decimal("balance", 10, 2)
    t.JSON("settings")
    t.Timestamps()                                      // nullable created_at, updated_at
    t.SoftDeletes()                                     // nullable, indexed deleted_at
})

err = s.CreateTable(ctx, "posts", func(t *schema.Table) {
    t.BigIncrements("id")
    t.Integer("user_id").Unsigned().NotNull()           // see the MySQL note below
    t.String("title")
    t.Foreign("user_id").References("users", "id").OnDelete("CASCADE")
    t.Index("title")
})
```

**Column types:** `Increments`, `BigIncrements`, `String(name, length?)`, `Text`, `SmallInteger`, `Integer`, `BigInteger`, `Boolean`, `Float`, `Double`, `Decimal(name, precision, scale)`, `Date`, `Time`, `DateTime`, `Timestamp`, `JSON`, `UUID`, `Binary`, `Enum`.

**Column modifiers:** `NotNull()`, `Nullable()`, `Default(v)`, `DefaultRaw(sql)`, `DefaultNow()`, `Unsigned()` (MySQL only; ignored elsewhere), `Primary()`, `Unique()`, `Index()`.

**Relations (Lucid / Laravel style):**

```go
t.ForeignID("user_id").Constrained().OnDelete("CASCADE")          // bigint unsigned NOT NULL -> users(id); table inferred from the name
t.ForeignID("editor_id").Nullable().Constrained("users").OnDelete("SET NULL")
t.Integer("author_id").Unsigned().References("authors", "id").OnDelete("CASCADE")   // Knex/Lucid form
t.Morphs("commentable")            // commentable_type + commentable_id (+ composite index); NullableMorphs for nullable
```

`ForeignID` pairs with `BigIncrements`; if the referenced key is `Increments` (int), use `Integer(...).Unsigned().References(...)` so the types match.

**Table-level:** `Primary(cols...)` for composite keys, `Unique(cols...)`, `Index(cols...)` (both accept `.Named("x")`), `Foreign(cols...).References(table, cols...).OnDelete(...).OnUpdate(...)`.

Columns are nullable by default, as in Knex. Names for indexes and constraints are generated (`users_email_unique`, `posts_user_id_foreign`) and shortened with a hash if they exceed 63 characters.

### Altering tables

```go
err = s.AlterTable(ctx, "users", func(t *schema.Table) {
    t.String("nickname")                  // add column
    t.DropColumn("legacy_flag")
    t.RenameColumn("name", "full_name")
    t.Index("nickname")
    t.DropIndex("users_old_index")
})

s.RenameTable(ctx, "users", "members")
s.DropTable(ctx, "members")
s.DropTableIfExists(ctx, "members")
ok, _ := s.HasTable(ctx, "users")
ok, _ = s.HasColumn(ctx, "users", "email")
s.Raw(ctx, "CREATE EXTENSION IF NOT EXISTS pgcrypto")
```

Changing a column's type is not supported; use `Raw` for that.

### Inspect the SQL without running it

```go
stmts, err := schema.CreateSQL(db.Dialect, "users", func(t *schema.Table) { t.Increments("id") })
```

`schema.AlterSQL` does the same for `AlterTable`.

## Query builder

For tables without a model. Builders are mutable: each method changes and returns the same `*Builder`.

```go
users := func() *query.Builder { return query.Table(db, "users") }

rows, err := users().Where("age > ?", 18).OrderBy("name", "asc").Limit(20).Get(ctx) // []map[string]any
row, err := users().Where("id = ?", 1).First(ctx)       // errors.Is(err, migro.ErrNotFound) if missing
n, err := users().WhereIn("id", []int{1, 2, 3}).Count(ctx)
err = users().Insert(ctx, query.Row{"name": "Ada"}, query.Row{"name": "Alan"})
affected, err := users().Where("id = ?", 1).Update(ctx, query.Row{"name": "Ada L"})
affected, err = users().Where("id = ?", 2).Delete(ctx)
page, err := users().OrderBy("id", "asc").Paginate(ctx, 1, 25) // Data, Total, PerPage, CurrentPage, LastPage
```

Also available: `Select`, `OrWhere`, `Not`, `WhereNull`, `WhereNotNull`, `WhereBetween`, `Join("JOIN posts ON …")`, `GroupBy`, `Having`, `Offset`, `Pluck`.

Values are always bound as parameters. `Update` and `Delete` without a `Where` return an error rather than touching every row. `OrderBy` only accepts `asc` or `desc`.

## Models

Models are plain GORM structs. `orm.Model[T]` adds a Lucid-style repository.

```go
type User struct {
    orm.SoftDeleteModel            // ID, CreatedAt, UpdatedAt, DeletedAt (soft deletes)
    Email string
    Name  string
    Posts []Post                   // GORM relation, load it with Preload
}

users := orm.New[User](db)

u := &User{Email: "ada@example.com", Name: "Ada"}
err := users.Create(ctx, u)
found, err := users.Find(ctx, u.ID)                      // ErrNotFound when missing
found, err = users.FindBy(ctx, "email", "ada@example.com")
found.Name = "Ada Lovelace"
err = users.Save(ctx, found)
err = users.Delete(ctx, found)                           // soft delete (sets deleted_at)

all, err := users.All(ctx)
created, wasCreated, err := users.FirstOrCreate(ctx, User{Email: "x@y.z"}, User{Name: "X"})
```

Use `orm.BaseModel` (ID, CreatedAt, UpdatedAt) when you do not want soft deletes.

### Queries

```go
page, err := users.Query().
    Where("email LIKE ?", "%@example.com").
    OrderBy("id", "desc").
    Preload("Posts").                    // eager loading, one extra query
    Paginate(ctx, 1, 20)                 // Data, Total, PerPage, CurrentPage, LastPage

first, err := users.Query().Where("name = ?", "Ada").First(ctx)
ok, err := users.Query().Where("email = ?", e).Exists(ctx)
n, err := users.Query().WhereIn("id", ids).Count(ctx)
n, err = users.Query().Where("name = ?", "Old").Update(ctx, map[string]any{"name": "New"})
n, err = users.Query().Where("id = ?", 1).Delete(ctx)
```

Soft deletes: `Query().WithTrashed()` includes deleted rows, `OnlyTrashed()` returns only them. `Scope(fn...)` applies reusable `func(*gorm.DB) *gorm.DB` fragments. `OnlyTrashed` assumes the column is named `deleted_at`.

Lifecycle hooks (`BeforeCreate`, `AfterSave`, …) and relations use GORM's own mechanisms. For a global event bus see [Events](#events).

## Relations GORM lacks

GORM covers has-one, has-many, belongs-to, many-to-many and polymorphic has-many. migro adds the missing ones as batch loaders: one query per hop, never per parent. They return maps keyed by the parent key you pass in.

### hasManyThrough

Country → Citizen → Story:

```go
rel := orm.HasManyThrough[Story, Citizen]{
    ForeignKey:        "country_id", // column on Citizen pointing at Country
    ThroughForeignKey: "citizen_id", // column on Story pointing at Citizen
    // ThroughLocalKey defaults to "id"; Scope can order or filter the Story query
}
byCountry, err := rel.Load(ctx, db, []any{1, 2, 3}) // map[any][]Story
```

### morphTo

For a table with `commentable_type` / `commentable_id`:

```go
reg := orm.NewMorphRegistry()
orm.RegisterMorph[Article](reg, db, "article")
orm.RegisterMorph[Video](reg, db, "video")

owners, err := reg.Load(ctx, db, []orm.Ref{{Type: "article", ID: 7}, {Type: "video", ID: 2}})
article := owners[orm.Ref{Type: "article", ID: 7}].(*Article)
```

An unregistered type returns `UNKNOWN_MORPH_TYPE`. Refs whose row no longer exists are missing from the result.

### morphToMany / morphedByMany

For `taggables(tag_id, taggable_id, taggable_type)`:

```go
pivot := orm.Pivot{
    Table: "taggables", MorphIDColumn: "taggable_id", MorphTypeCol: "taggable_type",
    RelatedIDCol: "tag_id", MorphType: "article",
}
tags := orm.MorphToMany[Tag]{Pivot: pivot}

tags.Attach(ctx, db, articleID, 1, 2)   // idempotent
tags.Sync(ctx, db, articleID, 2, 3)     // exactly these links, atomically
tags.Detach(ctx, db, articleID, 3)      // omit keys to detach all
byArticle, err := tags.Load(ctx, db, []any{articleID})                      // map[any][]Tag
articles, err := orm.MorphedByMany[Article]{Pivot: pivot}.Load(ctx, db, []any{tagID}) // inverse
```

Keys are matched by their printed value, so `uint`, `int64` and numeric strings compare equal. Very large key lists are not chunked.

## Transactions

```go
err := db.Transaction(ctx, func(tx *migro.DB) error {
    if err := orm.New[User](tx).Create(ctx, &User{Email: "a@x.io"}); err != nil {
        return err // rolls back
    }
    return query.Table(tx, "audit").Insert(ctx, query.Row{"event": "user_created"})
})
```

`tx` works with every layer (schema, query, orm, migrate, seed, relations). Returning an error rolls back; returning nil commits.

## Migrations

Migrations are Go files, one per change, named like Laravel's: `<yyyymmddhhmmss>_<snake_case_name>.go`. Each file is a Lucid-style class with `Up` and `Down`, and registers itself when the package loads.

```go
// database/migrations/20260101120000_create_users_table.go
package migrations

import (
    "context"

    "github.com/LouisRiverstone/migro/migrate"
    "github.com/LouisRiverstone/migro/schema"
)

func init() { migrate.Register(&CreateUsersTable{}) }

type CreateUsersTable struct{}

func (CreateUsersTable) Up(ctx context.Context, s *schema.Builder) error {
    return s.CreateTable(ctx, "users", func(t *schema.Table) {
        t.BigIncrements("id")
        t.String("email", 120).NotNull().Unique()
        t.String("name")
        t.Timestamps()
        t.SoftDeletes()
    })
}

func (CreateUsersTable) Down(ctx context.Context, s *schema.Builder) error {
    return s.DropTableIfExists(ctx, "users")
}
```

```go
// database/migrations/20260102090000_create_posts_table.go  (indexes and relations)
func (CreatePostsTable) Up(ctx context.Context, s *schema.Builder) error {
    return s.CreateTable(ctx, "posts", func(t *schema.Table) {
        t.BigIncrements("id")
        t.ForeignID("user_id").Constrained().OnDelete("CASCADE")
        t.String("title").Index()
        t.Text("body")
        t.Morphs("subject")
        t.Unique("user_id", "title")
        t.Timestamps()
    })
}
```

How it works:

- **The name comes from the file name.** `migrate.Register` reads the calling file's name, so there is no string to keep in sync. A file that does not match `^\d{14}_[a-z0-9_]+\.go$`, or a duplicate, is reported when migrations load (`MIGRATION_FAILED`). Do not rename a file after it has run: it would count as a new migration.
- **The Go type name must be unique in the package** (`CreateUsersTable`). `make:migration` derives it from the file name and refuses duplicates.
- **Order is by name**, so the timestamp prefix decides it. `make:migration` always gives a new file a timestamp later than the newest existing one.
- **Optional behaviors** on the type: `Connection() string` pins it to a named connection (see [Several databases](#several-databases)), and `DisableTransaction() bool` opts out of the wrapping transaction.
- The package must be linked into whatever runs the migrations. The `migro` command does that for you ([Command line](#command-line-artisan--ace-style)); in your own `main`, import it for its side effects: `import _ "example.com/app/database/migrations"`.

### Running them from Go code

```go
m := migrate.New(db)
if err := m.AddRegistered(); err != nil { ... }      // everything registered via Register

res, err := m.Latest(ctx)                             // all pending, as one batch
res, err = m.Run(ctx, migrate.RunOptions{Step: true}) // every migration in its own batch
res, err = m.Run(ctx, migrate.RunOptions{Pretend: true}) // res.SQL[name] holds the SQL; nothing changes

res, err = m.Rollback(ctx)               // last batch
res, err = m.RollbackSteps(ctx, 2)       // the 2 most recent migrations
res, err = m.RollbackToBatch(ctx, 1)     // everything after batch 1 (0 = everything)
res, err = m.Reset(ctx)                  // everything
res, err = m.Refresh(ctx)                // Reset + Latest
res, err = m.Fresh(ctx)                  // DROP every table, then Latest (destructive)

status, err := m.Status(ctx)             // []Status{Name, Applied, Batch, AppliedAt}
err = m.Install(ctx)                     // create the tracking tables now (otherwise done on demand)
err = m.Unlock(ctx)                      // clear a stale lock
```

`migrate.New(db, migrate.WithObserver(func(e migrate.Event) { ... }))` reports `migrating`, `migrated`, `reverting`, `reverted` with durations.

Migrations written as plain values still work: `m.Add(migrate.Migration{Name: ..., Up: ..., Down: ...})`, and SQL files can be loaded with `migrate.FromFS(embeddedFS, "migrations")` (`<name>.up.sql` / `<name>.down.sql`).

### Behavior to know

- Tracking lives in `migro_migrations` (override with `migrate.WithTable`) plus a lock table. A second runner gets `MIGRATION_LOCKED` instead of running concurrently.
- Each migration runs in a transaction when the database supports transactional DDL (PostgreSQL, SQLite, SQL Server, Redshift). **MySQL, MariaDB, Oracle and CockroachDB commit DDL implicitly**, so a failed migration can leave partial changes. Keep migrations small there.
- `--pretend` records every write statement a migration makes instead of running it, including statements issued through `s.DB()`. Reads still run so a migration can inspect the database. A statement that must return rows (for example `INSERT ... RETURNING`) cannot be emulated and fails the pretend run. Nothing is created or locked, not even the tracking tables.
- Migrations with a `Checksum` are checked against what ran (`MIGRATION_CHECKSUM_MISMATCH`). SQL-file migrations get one automatically.

## Command line (artisan / ace style)

Install it once: `go install github.com/LouisRiverstone/migro/cmd/migro@latest`. Then, from your project (anywhere inside the Go module), with the application running or not:

```
migro make:migration create_users_table
migro migration:run
migro migration:rollback --step=2
migro migration:status
migro db:seed
```

### Configure the database

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

### Commands

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
| `migration:diff [name]` | Generate a migration from your models; see [below](#generate-migrations-from-models). |
| `db:seed` | Run seeders. `--class=UserSeeder` runs one; `--once` skips those that already ran. |
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

## Generate migrations from models

`migro migration:diff [name]` compares your GORM models with the live database and writes the difference as a new Go migration file. It treats every exported struct in `app/models` (`MIGRO_MODELS_DIR`) as a model; add `// migro:ignore` to a type's doc comment to skip it.

```
migro migration:diff add_profile_fields
migro migration:diff --rename=users.name=full_name     # a rename, not a drop + add
migro migration:diff cleanup --allow-destructive       # include DROP COLUMN / DROP INDEX
```

The file uses the schema builder (`t.String(...)`, `t.Index(...)`), so you can edit it before running `migration:run`. Behind it is `migrate/diff`, usable from code:

```go
plan, err := diff.Generate(ctx, db, diff.Options{
    Renames: map[string]string{"users.name": "full_name"}, // otherwise a rename is a drop + add
}, &User{}, &Post{})

for _, w := range plan.Warnings { log.Println("warning:", w) }
for _, op := range plan.Blocked { log.Printf("not applied (destructive): %s %s", op.Kind, op.Table) }

path, err := plan.WriteGoFile("database/migrations", "migrations", "sync_models", time.Now()) // a Go migration; "" when there is nothing to do
paths, err := plan.WriteFiles("migrations", "sync_models")                                     // or .up.sql / .down.sql files
```

What it detects: new tables, added/dropped/renamed columns, added/dropped indexes. Drops are listed in `plan.Blocked` and only written when `Options{AllowDestructive: true}`. It never drops tables.

What it does not do: detect column type or nullability changes (some are reported as warnings), or translate default expressions (reported as warnings). The `down` file for a dropped column is a comment, because the data cannot be restored. **Always read the generated SQL before applying it.**

## Seeders and factories

### Factories

```go
users := seed.Define(func(f *seed.Faker) User {
    return User{Name: f.Name(), Email: f.Email()}
}).
    State("admin", func(u *User) { u.Role = "admin" }).
    Relation("posts", func(ctx context.Context, db *migro.DB, u *User, n int, f *seed.Faker) error {
        for range n {
            if err := db.Gorm.Create(&Post{UserID: u.ID, Title: f.Sentence()}).Error; err != nil { return err }
        }
        return nil
    })

u, err := users.Make()                                             // built, not saved
list, err := users.CreateMany(ctx, db, 10)                         // saved; all-or-nothing
admin, err := users.Apply("admin").With("posts", 3).Create(ctx, db)
fixed, err := users.Merge(func(u *User) { u.Name = "Fixed" }).Seed(42).Make() // reproducible
```

`Merge`, `Apply`, `With` and `Seed` return copies, so a shared factory is never mutated. `After(fn)` registers a hook that runs after each create.

The faker is deterministic for a given seed. It offers `Name`, `FirstName`, `LastName`, `Email` (reserved `example.*` domains), `Username`, `Word`, `Words`, `Sentence`, `Paragraph`, `Int`, `Float`, `Bool`, `Pick`, `UUID` and `Date`.

### Seeders

A seeder is a type with a `Run` method, registered like a migration. `migro make:seeder User` creates it:

```go
// database/seeders/user_seeder.go
func init() { seed.Register(&UserSeeder{}) }

type UserSeeder struct{}

func (UserSeeder) Run(ctx context.Context, db *migro.DB) error {
    _, err := factories.UserFactory().Apply("admin").CreateMany(ctx, db, 10)
    return err
}

// optional: limit where it runs
func (UserSeeder) Environments() []string { return []string{"development", "staging"} }
```

```
migro db:seed                      # every seeder, by name
migro db:seed --class=UserSeeder   # one
migro db:seed --once               # skip seeders that already ran (tracked in migro_seeds)
migro migration:fresh --seed       # rebuild the database and seed it
```

From code: `r := seed.NewRunner(db, seed.Once()); r.AddRegistered(); r.Run(ctx)`, or `r.Add(seed.Seeder{Name: "...", Run: ...})` for ad-hoc ones.

- Seeders run in name order, each in its own transaction (a failure rolls back that seeder).
- Without `--once` they run every time (like Lucid), so make them idempotent.
- A seeder with no `Environments` runs everywhere **except production**; list `"production"` to allow it there.
- The environment comes from `MIGRO_ENV`, then `APP_ENV`, then `development`.

`migro make:factory User` creates `database/factories/user_factory.go` with a `UserFactory()` function; `migro make:model User -a` creates the model, its migration, factory and seeder together.

## Events

```go
bus := events.New()
if err := bus.Attach(db); err != nil { ... }

// Typed, per model. A before:* handler that returns an error aborts the write.
events.OnModel(bus, events.BeforeCreate, func(ctx context.Context, u *User) error {
    if u.Email == "" { return errors.New("email is required") }
    return nil
})
events.OnModel(bus, events.AfterCreate, func(ctx context.Context, u *User) error {
    return mailer.SendWelcome(u.Email)
})

// Free-form events and patterns
bus.On("after:*", logEvent)
bus.OnAny(audit)
off := bus.Once("user:welcomed", handler); off() // unsubscribe
err := bus.Emit(ctx, events.Event{Name: "user:welcomed", Payload: u})
bus.EmitAsync(ctx, ev, func(err error) { log.Println(err) }); bus.Wait()
```

Names: `before:create`, `after:create`, `before:update`, `after:update`, `before:save`, `after:save` (create or update), `before:delete`, `after:delete`, `after:find`.

- `Emit` returns `EVENT_ABORTED` on the first `before:*` error and stops. For other events every handler runs and failures are combined into `EVENT_HANDLER_FAILED`. A handler panic becomes an error.
- Handlers run inside the operation's transaction, so an abort rolls the write back.
- Like GORM hooks, events do not fire for `UpdateColumn(s)`, raw SQL, `Exec`, or the query builder.
- Events are in-process only.

## Pool, replicas and sharding

### Pool

```go
db, err := migro.Open(migro.Config{
    Dialect: "postgres", DSN: dsn,
    Pool: migro.Pool{
        Min: 2, Max: 10,
        AcquireTimeout: 3 * time.Second,
        IdleTimeout:    5 * time.Minute,
        MaxLifetime:    time.Hour,
        Validate:       true,
        AfterCreate: func(ctx context.Context, c migro.Conn) error {
            return c.Exec(ctx, "SET TIME ZONE 'UTC'")
        },
    },
})
```

| Field | Effect |
|---|---|
| `Max` | Maximum open connections (0 = unlimited). |
| `Min` | Connections opened at startup and kept idle. Best effort: `database/sql` can still close them after `IdleTimeout`/`MaxLifetime`. |
| `AcquireTimeout` | Longest wait for a free connection, then `POOL_ACQUIRE_TIMEOUT`. Never cancels a running query or transaction. |
| `IdleTimeout`, `MaxLifetime` | Recycle connections. |
| `Validate` | Ping a pooled connection on checkout and discard it if dead. Needs a driver implementing `driver.Pinger`. |
| `AfterCreate` | Runs once per new physical connection. If it fails, the connection is discarded. |

Invalid values (negative, or `Min > Max`) return `INVALID_CONFIG`.

### Read replicas

```go
db, err := migro.Open(migro.Config{
    Dialect: "postgres", DSN: primaryDSN,
    Replicas:      []string{replica1DSN, replica2DSN},
    ReplicaPolicy: "round-robin", // or "random" (default)
})

db.Gorm.Find(&users)                 // read  → a replica
db.Gorm.Create(&user)                // write → primary
db.WriteDB().Gorm.First(&user, id)   // force the primary (read your own write)
```

Transactions always use the primary. Replicas can lag, so a read right after a write may be stale; use `WriteDB()` when it matters.

### Sharding

`cluster` routes work across independent databases (for example one per tenant group):

```go
c, err := cluster.Open(ctx, map[string]migro.Config{
    "s1": {Dialect: "postgres", DSN: dsn1},
    "s2": {Dialect: "postgres", DSN: dsn2},
}, cluster.HashRouter([]string{"s1", "s2"}))
defer c.Close()

shard, err := c.For(tenantID)                      // *migro.DB that owns this key
results, err := c.Migrate(ctx, migrations)         // same migrations on every shard
err = c.Each(ctx, func(ctx context.Context, name string, db *migro.DB) error { return nil })
```

You can supply your own `cluster.Router` (`func(key any) (string, error)`) instead of `HashRouter`.

There are no cross-shard transactions. `Each` runs shards concurrently and independently: if one fails you get `SHARD_FAILED` naming it, and the shards that succeeded stay committed. Adding or removing a shard changes where most hash keys land, so move data first.

## Errors

Every failure is a `*migro.Error` with a stable `Code`.

```go
user, err := users.Find(ctx, id)
switch {
case errors.Is(err, migro.ErrNotFound):
    // 404
case err != nil:
    var me *migro.Error
    if errors.As(err, &me) { log.Println(me.Code, me.Message) }
}
code := migro.CodeOf(err) // "" when err is not a migro error
```

`errors.Is` matches by code, so the sentinels `ErrNotFound`, `ErrMigrationLocked`, `ErrUnsupportedFeature`, `ErrPoolAcquireTimeout` and `ErrEventAborted` work through wrapped errors.

| Code | Meaning |
|---|---|
| `UNSUPPORTED_DIALECT` | Unknown dialect name, or its package was not imported |
| `CONNECTION_FAILED` | Could not connect, ping or close |
| `INVALID_CONFIG` | Bad pool, replica or cluster settings |
| `POOL_ACQUIRE_TIMEOUT` | No free connection within `AcquireTimeout` |
| `NOT_FOUND` | No matching row |
| `QUERY_FAILED` | The database rejected a statement |
| `INVALID_SCHEMA` | Bad table or column definition |
| `UNSUPPORTED_FEATURE` | The dialect cannot do this (for example `CREATE INDEX` on Redshift) |
| `MIGRATION_LOCKED` / `MIGRATION_FAILED` / `MIGRATION_CHECKSUM_MISMATCH` | Migration runner |
| `EVENT_ABORTED` / `EVENT_HANDLER_FAILED` | Event bus |
| `SEED_FAILED` | Seeder or factory failure |
| `PRODUCTION_BLOCKED` | A destructive command ran in production without `--force` or a confirmation |
| `DIFF_FAILED` | Schema diff failure |
| `SHARD_NOT_FOUND` / `SHARD_FAILED` | Cluster routing or fan-out |
| `UNKNOWN_MORPH_TYPE` | Polymorphic type not registered |

## Database notes

- **MySQL / MariaDB:** `Increments`/`BigIncrements` are `unsigned`, so a foreign-key column must be `Integer(...).Unsigned()` / `BigInteger(...).Unsigned()` to match, otherwise MySQL rejects the constraint. DDL is not transactional.
- **SQLite:** cannot add a foreign key or primary key to an existing table (`UNSUPPORTED_FEATURE`); a unique constraint added with `AlterTable` becomes a unique index. Pure Go driver, no cgo.
- **Redshift:** no `CREATE INDEX` (`UNSUPPORTED_FEATURE`); JSON maps to `SUPER`; keys use `IDENTITY`. Constraints are informational only.
- **SQL Server:** `Enum` is `nvarchar` plus a `CHECK`; `Boolean` is `bit`.
- **Oracle:** identifiers are upper-cased; `Time` is stored as `VARCHAR2(15)` since Oracle has no `TIME` type; DDL is not transactional.
- **CockroachDB:** schema changes are not wrapped in transactions.

The full test suite runs on SQLite in CI (blocking) and on PostgreSQL and MySQL in non-blocking CI jobs. CockroachDB, Redshift, SQL Server, Oracle and MariaDB are generated and unit-tested but have not been run against live servers; please report problems.

## Testing your own code

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
