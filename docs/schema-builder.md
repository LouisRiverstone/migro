---
title: Schema builder
nav_order: 6
description: "Create, change, rename and drop tables; every column type and modifier per database; indexes and foreign keys."
---

# Schema builder

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

The schema builder creates and changes tables without writing SQL for each database: it is Laravel's `Schema` facade and `Blueprint`, with Knex's names. One definition compiles to the right SQL for every supported dialect. You will normally use it inside [migrations](migrations.md), which receive a ready `*schema.Builder`; elsewhere, make one with `schema.New(db)`.

## Tables

### Creating tables

`CreateTable` takes the table name and a function that defines its columns:

```go
s := schema.New(db)

err := s.CreateTable(ctx, "users", func(t *schema.Table) {
    t.BigIncrements("id")
    t.String("name")
    t.String("email", 120).NotNull().Unique()
    t.Timestamps()
})
```

`CreateTableIfNotExists` skips the table when it already exists.

#### Determining table and column existence

```go
ok, err := s.HasTable(ctx, "users")
ok, err = s.HasColumn(ctx, "users", "email")
```

Laravel's `Schema::hasIndex` has no builder method; GORM's migrator answers it: `db.Gorm.Migrator().HasIndex("users", "users_email_unique")`.

#### Database connection and table options

The builder runs on the connection it was made with, so `schema.New(otherDB)` is Laravel's `Schema::connection('sqlite')`. In a migration, pin the migration to a connection instead (see [Migrations](migrations.md#setting-the-migration-connection)).

Table options such as MySQL's `ENGINE`, `CHARSET` and `COLLATE`, temporary tables and table comments have no builder method. Run the statement with `s.Raw` after creating the table:

```go
s.Raw(ctx, "ALTER TABLE users ENGINE = InnoDB, CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
s.Raw(ctx, "COMMENT ON TABLE users IS 'Application users'") // PostgreSQL
```

### Updating tables

`AlterTable` adds, renames and drops columns and indexes on an existing table:

```go
err := s.AlterTable(ctx, "users", func(t *schema.Table) {
    t.Integer("votes")
})
```

### Renaming and dropping tables

```go
err := s.RenameTable(ctx, "users", "members")
err = s.DropTable(ctx, "members")
err = s.DropTableIfExists(ctx, "members")
```

#### Renaming tables with foreign keys

Constraint names are generated from the table name (`posts_user_id_foreign`). After a rename, existing constraints keep their old names, so `DropForeign` in a later migration must use the old name. Name constraints yourself with `.Named(...)` if you plan to rename the table.

## Columns

### Creating columns

Inside `CreateTable` or `AlterTable`, each column method adds a column and returns it, so modifiers can follow:

```go
err := s.AlterTable(ctx, "users", func(t *schema.Table) {
    t.Integer("votes").NotNull().Default(0)
})
```

{: .note }
Columns are **nullable by default**, as in Knex, where Laravel makes them `NOT NULL`. Add `.NotNull()` to the columns that need it.

### Available column types

| Method | PostgreSQL | MySQL / MariaDB | SQLite | SQL Server | Oracle |
|---|---|---|---|---|---|
| `Increments(name)` | `serial` primary key | `int unsigned auto_increment` primary key | `integer` primary key | `int identity` primary key | `NUMBER(10)` identity |
| `BigIncrements(name)` | `bigserial` primary key | `bigint unsigned auto_increment` | `integer` primary key | `bigint identity` | `NUMBER(19)` identity |
| `SmallInteger(name)` | `smallint` | `smallint` | `integer` | `smallint` | `NUMBER(5)` |
| `Integer(name)` | `integer` | `int` | `integer` | `int` | `NUMBER(10)` |
| `BigInteger(name)` | `bigint` | `bigint` | `integer` | `bigint` | `NUMBER(19)` |
| `Boolean(name)` | `boolean` | `tinyint(1)` | `boolean` | `bit` | `NUMBER(1)` |
| `Float(name)` | `real` | `float` | `real` | `real` | `BINARY_FLOAT` |
| `Double(name)` | `double precision` | `double` | `real` | `float` | `BINARY_DOUBLE` |
| `Decimal(name, precision, scale)` | `numeric(p,s)` | `decimal(p,s)` | `decimal(p,s)` | `decimal(p,s)` | `NUMBER(p,s)` |
| `String(name, length?)` | `varchar(255)` | `varchar(255)` | `varchar(255)` | `nvarchar(255)` | `VARCHAR2(255)` |
| `Text(name)` | `text` | `text` | `text` | `nvarchar(max)` | `CLOB` |
| `Enum(name, values...)` | `varchar` + `CHECK` | native `enum(...)` | `varchar` + `CHECK` | `nvarchar` + `CHECK` | `VARCHAR2` + `CHECK` |
| `Date(name)` | `date` | `date` | `date` | `date` | `DATE` |
| `Time(name)` | `time` | `time` | `time` | `time` | `VARCHAR2(15)` |
| `DateTime(name)` | `timestamp` | `datetime(3)` | `datetime` | `datetime2` | `TIMESTAMP` |
| `Timestamp(name)` | `timestamptz` | `datetime(3)` | `datetime` | `datetimeoffset` | `TIMESTAMP WITH TIME ZONE` |
| `JSON(name)` | `jsonb` | `json` | `text` | `nvarchar(max)` | `CLOB` |
| `UUID(name)` | `uuid` | `char(36)` | `varchar(36)` | `uniqueidentifier` | `VARCHAR2(36)` |
| `Binary(name)` | `bytea` | `longblob` | `blob` | `varbinary(max)` | `BLOB` |

CockroachDB follows PostgreSQL. Redshift does too, except `Text` is `varchar(65535)`, `JSON` is `SUPER`, `UUID` is `varchar(36)` and keys use `IDENTITY`.

Helpers that add more than one column:

| Method | Adds |
|---|---|
| `Timestamps()` | nullable `created_at` and `updated_at` (`Timestamp`), maintained by models |
| `SoftDeletes()` | nullable, indexed `deleted_at`, for [soft deleting](models.md#soft-deleting) models |
| `ForeignID(name)` | a `BigInteger(name).Unsigned().NotNull()`, ready for `.Constrained()` |
| `Morphs(name)` | `<name>_type` (`String`) and `<name>_id` (`BigInteger` unsigned), `NOT NULL`, plus a composite index |
| `NullableMorphs(name)` | the same, nullable |

#### Laravel types without a method

| Laravel | In migro |
|---|---|
| `id()` | `BigIncrements("id")` |
| `char`, `tinyText`, `mediumText`, `longText` | `String(name, n)` or `Text` (unbounded on every database) |
| `tinyInteger`, `mediumInteger`, `unsigned*Integer` | `SmallInteger` / `Integer` / `BigInteger`, with `.Unsigned()` on MySQL |
| `tinyIncrements`, `smallIncrements`, `mediumIncrements` | `Increments` |
| `dateTimeTz`, `timestampTz`, `timestampsTz` | `Timestamp` is already time-zone aware where the database has such a type |
| `softDeletesTz` | `SoftDeletes` |
| `jsonb` | `JSON` (it is `jsonb` on PostgreSQL) |
| `uuid`, `foreignUuid` | `UUID(name)`, then `.References(...)` |
| `ulid`, `foreignUlid` | `String(name, 26)` |
| `uuidMorphs`, `ulidMorphs` | `String(name+"_type")` and `UUID(name+"_id")` (or `String(..., 26)`), plus `Index` |
| `year` | `SmallInteger` |
| `set`, `ipAddress`, `macAddress`, `geometry`, `geography`, `vector` | `s.Raw` with the database's own type |
| `rememberToken()` | `String("remember_token", 100)` |
| `foreignIdFor(User::class)` | `ForeignID("user_id")` |

### Column modifiers

Modifiers chain onto a column:

```go
t.String("email").NotNull().Unique()
t.Integer("votes").Default(0)
t.Timestamp("published_at").Nullable()
```

| Modifier | Effect |
|---|---|
| `NotNull()` | `NOT NULL` |
| `Nullable()` | allows `NULL` (the default) |
| `Default(v)` | a default value, quoted for the database |
| `DefaultRaw(sql)` | a default expression, sent as written (`DefaultRaw("gen_random_uuid()")`) |
| `DefaultNow()` | defaults to the current timestamp (Laravel's `useCurrent()`) |
| `Unsigned()` | `UNSIGNED`, on MySQL and MariaDB; ignored elsewhere |
| `Primary()` | makes the column the primary key |
| `Unique()` | a unique index on the column |
| `Index()` | an index on the column |

Laravel's `after`, `first`, `charset`, `collation`, `comment`, `storedAs`, `virtualAs`, `invisible`, `useCurrentOnUpdate` and `autoIncrement` on a non-key column have no modifier. They are specific to one or two databases; use `s.Raw` for them.

#### Default expressions

`Default` quotes its value. To call a database function, use `DefaultRaw`:

```go
t.JSON("movies").DefaultRaw("(JSON_ARRAY())") // MySQL 8
t.UUID("id").DefaultRaw("gen_random_uuid()")  // PostgreSQL 13+
t.Timestamp("created_at").DefaultNow()
```

Support for default expressions depends on the database, its version and the column type.

### Modifying columns

Laravel's `->change()` has no counterpart: changing a column's type, length or nullability differs too much between databases (SQLite has to rebuild the table). Use `s.Raw` with your database's statement:

```go
s.Raw(ctx, "ALTER TABLE users ALTER COLUMN name TYPE varchar(50)")          // PostgreSQL
s.Raw(ctx, "ALTER TABLE users MODIFY name varchar(50) NOT NULL")             // MySQL
s.Raw(ctx, "ALTER TABLE users ALTER COLUMN birthday TYPE date USING birthday::date") // PostgreSQL, with a cast
```

### Renaming columns

```go
err := s.AlterTable(ctx, "users", func(t *schema.Table) {
    t.RenameColumn("from", "to")
})
```

### Dropping columns

```go
err := s.AlterTable(ctx, "users", func(t *schema.Table) {
    t.DropColumn("votes")
    t.DropColumn("avatar", "location") // several at once
})
```

Laravel's aliases are plain `DropColumn` calls: `dropTimestamps()` is `DropColumn("created_at", "updated_at")`, `dropSoftDeletes()` is `DropColumn("deleted_at")`, and `dropMorphs("taggable")` is `DropColumn("taggable_type", "taggable_id")`.

## Indexes

### Creating indexes

Add an index on a column as you define it:

```go
t.String("email").Unique()
t.String("state").Index()
```

or afterwards, which also allows composite indexes:

```go
t.Unique("email")
t.Index("account_id", "created_at")
t.Unique("email").Named("unique_email") // a name of your own
```

Generated names follow Laravel's pattern (`users_email_unique`, `geo_state_index`), and are shortened with a hash past 63 characters.

#### Available index types

| Method | Index |
|---|---|
| `Primary(cols...)` | a (composite) primary key; `Column.Primary()` for one column |
| `Unique(cols...)` | a unique index |
| `Index(cols...)` | a plain index |
| `FullText(cols...)` | a full-text index: `FULLTEXT` on MySQL and MariaDB, a GIN index on `to_tsvector` on PostgreSQL and CockroachDB; `UNSUPPORTED_FEATURE` elsewhere |

Query a full-text index with `WhereFullText` ([Query builder](query-builder.md#full-text-where-clauses)). Spatial and vector indexes, and online index creation (`CONCURRENTLY`, `ONLINE = ON`), need `s.Raw`.

### Renaming indexes

There is no `RenameIndex`; use `s.Raw` (`ALTER INDEX ... RENAME TO ...` on PostgreSQL, `ALTER TABLE ... RENAME INDEX ... TO ...` on MySQL), or drop the index and create it again.

### Dropping indexes

Drop an index by name:

```go
t.DropIndex("geo_state_index")
t.DropUnique("users_email_unique")
```

Names are `<table>_<columns>_<type>`, so the index created by `t.Index("state")` on `geo` is `geo_state_index`.

{: .note }
On MySQL, an index that also serves a foreign key cannot be dropped while the key exists (error 1553). Drop the foreign key first.

### Foreign key constraints

A foreign key makes the database keep references consistent. The short form creates the column and the constraint together; `Constrained` infers the table from the column name (`user_id` refers to `users.id`):

```go
err := s.CreateTable(ctx, "posts", func(t *schema.Table) {
    t.BigIncrements("id")
    t.ForeignID("user_id").Constrained()
})
```

Give the table when the name does not follow the convention, and add actions:

```go
t.ForeignID("editor_id").Nullable().Constrained("users").OnDelete("SET NULL")
t.ForeignID("user_id").Constrained().OnDelete("CASCADE").OnUpdate("CASCADE")
```

`OnDelete` and `OnUpdate` take the SQL action: `CASCADE`, `SET NULL`, `RESTRICT`, `NO ACTION`. They cover Laravel's `cascadeOnDelete()`, `nullOnDelete()`, `restrictOnDelete()` and the rest. Put other modifiers such as `Nullable()` before `Constrained()`.

The long form names every part:

```go
t.Integer("author_id").Unsigned()
t.Foreign("author_id").References("authors", "id").OnDelete("CASCADE").Named("posts_author_fk")
t.Integer("author_id").Unsigned().References("authors", "id") // the column form
```

{: .warning }
On MySQL, `Increments` and `BigIncrements` are unsigned, so the referencing column must be unsigned too or MySQL rejects the constraint. `ForeignID` already is. If the referenced key is `Increments` (an `int`), use `Integer(...).Unsigned().References(...)` so the types match.

SQLite cannot add a foreign key to an existing table (`UNSUPPORTED_FEATURE`): declare it in `CreateTable`.

#### Dropping foreign keys

```go
t.DropForeign("posts_user_id_foreign")
```

The generated name is the table, the columns and `_foreign`.

#### Toggling foreign key constraints

There is no portable switch. Use your database's statement, for example while loading data:

```go
s.Raw(ctx, "SET FOREIGN_KEY_CHECKS = 0") // MySQL; 1 to turn them back on
s.Raw(ctx, "PRAGMA foreign_keys = OFF")  // SQLite
s.Raw(ctx, "SET session_replication_role = replica") // PostgreSQL (superuser)
```

## Raw statements

`s.Raw` runs any SQL as part of the migration, including in `--pretend` mode, where it is recorded instead of run:

```go
s.Raw(ctx, "CREATE EXTENSION IF NOT EXISTS pgcrypto")
```

`s.DB()` gives the `*migro.DB` behind the builder, for queries.

## Inspecting the SQL without running it

```go
stmts, err := schema.CreateSQL(db.Dialect, "users", func(t *schema.Table) { t.BigIncrements("id") })
stmts, err = schema.AlterSQL(db.Dialect, "users", func(t *schema.Table) { t.String("nickname") })
```

They return the statements for a dialect without a connection, which is handy in tests. For a whole migration, run it with `--pretend`.
