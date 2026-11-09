---
title: Schema builder
nav_order: 6
description: Knex-style table builder: columns, indexes, foreign keys, relations.
---

# Schema builder

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

## Altering tables

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

## Inspect the SQL without running it

```go
stmts, err := schema.CreateSQL(db.Dialect, "users", func(t *schema.Table) { t.Increments("id") })
```

`schema.AlterSQL` does the same for `AlterTable`.
