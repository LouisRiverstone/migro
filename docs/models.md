---
title: Models
nav_order: 8
description: Eloquent and Lucid style models on top of GORM — retrieving, inserting, updating, deleting, mass assignment, serialization.
---

# Models

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

migro includes an object-relational mapper in the spirit of Laravel's Eloquent and AdonisJS's Lucid. Each database table has a corresponding **model** used to interact with it. Besides retrieving rows, models let you insert, update and delete them, and define [relationships](relations.md) between tables.

A model is a GORM struct that embeds `orm.BaseModel` (or `orm.SoftDeleteModel`). `orm.New[T](db)` is the repository, the equivalent of `User::` in Eloquent or `User.` in Lucid. Rows it returns are *bound* to the database, which turns on the instance API (`user.Save(ctx)`) and lazy relations.

## Defining models

```go
type User struct {
    orm.SoftDeleteModel            // ID, CreatedAt, UpdatedAt, DeletedAt
    Email    string
    Name     string
    Password string

    Posts   orm.HasMany[Post]      // see Relations
    Profile orm.HasOne[Profile]
}

func (User) Fillable() []string { return []string{"email", "name"} }   // $fillable
func (User) Hidden() []string   { return []string{"password"} }        // $hidden
```

`orm.BaseModel` has no soft deletes; `orm.SoftDeleteModel` adds them. Models whose key is composite (a pivot-like table) cannot embed `BaseModel`; they still work as the related side of a relation and with `orm.Bind`.

Generate one with `migro make:model User`; add `-a` to also create its migration, factory and seeder ([Command line](command-line.md)).

### Table names, primary keys and timestamps

These come from GORM. The table is the pluralized snake_case type name (`User` → `users`) unless the model has a `TableName() string` method. The key is the field named `ID` (or tagged `gorm:"primaryKey"`), and `BaseModel` maintains `CreatedAt` and `UpdatedAt`.

### Bound models

A model is bound when it comes from `Find`, `Get`, `First`, `Paginate`, `Create*`, a relation or a factory. A struct you build by hand is not: use `users.Make(attrs)`, create it through the repository, or call `orm.Bind(db, &u)`. Calling `Save` on an unbound model returns `UNBOUND_MODEL`.

Go copies structs by value, and a copy is detached from the row it came from. Keep pointers (`&rows[0]`, not `row := rows[0]`) when you rely on the instance API or lazy relations after loading.

## Retrieving models

```go
users := orm.New[User](db)

all, err := users.All(ctx)
user, err := users.Find(ctx, 1)                  // NOT_FOUND when missing (FindOrFail is the same)
many, err := users.FindMany(ctx, 1, 2, 3)
user, err = users.FindBy(ctx, "email", "ada@example.com")
```

### Building queries

`users.Query()` returns a fluent `Query[T]`. Every model method is available on it, and the rows it returns are bound.

```go
page, err := users.Query().
    Where("email LIKE ?", "%@example.com").
    Latest().                             // ORDER BY created_at DESC (Oldest, OrderByDesc, OrderBy)
    With("Posts.Comments").               // eager loading, see Relations
    WithCount("Posts").
    Paginate(ctx, 1, 20)                  // Data, Total, PerPage, CurrentPage, LastPage

first, err := users.Query().Where("name = ?", "Ada").First(ctx)     // FirstOrFail is the same
```

Also: `WhereIn`, `WhereNotIn`, `WhereNull`, `WhereNotNull`, `WhereBetween`, `Not`, `OrWhere`, `When(cond, fn)`, `Tap(fn)`, `Scope(fn...)`, `WithTrashed()` and `OnlyTrashed()` (which assumes the column is `deleted_at`).

### Aggregates and single values

```go
ok, err := users.Query().Where("email = ?", e).Exists(ctx)
n, err := users.Query().WhereIn("id", ids).Count(ctx)
total, err := users.Query().Sum(ctx, "visits")                          // Avg, Min, Max too

names, err := orm.Pluck[User, string](ctx, users.Query(), "name")      // generic: a function, not a method
email, err := orm.Value[User, string](ctx, users.Query().Where("id = ?", 1), "email")
```

### Chunking results

`Chunk` walks a large table in batches with keyset pagination on the primary key, so memory stays flat:

```go
err = users.Query().Chunk(ctx, 500, func(batch []User) error {
    return nil
})
```

## Inserting and updating models

### Inserts

```go
user, err := users.CreateFrom(ctx, map[string]any{"email": "ada@example.com", "name": "Ada"})

u := &User{Email: "alan@example.com"}
err = users.Create(ctx, u)

err = users.CreateMany(ctx, rows)
```

### Updates

`Save` writes only the changed columns, and does nothing when the model is clean.

```go
user.Name = "Ada Lovelace"
err = user.Save(ctx)                                        // UPDATE name only
err = user.Update(ctx, map[string]any{"name": "Ada L."})    // fill + save

n, err := users.Query().Where("name = ?", "Old").Update(ctx, map[string]any{"name": "New"})
n, err = users.Query().Where("id = ?", 1).Increment(ctx, "visits", 1)
err = user.Increment(ctx, "visits", 1)
```

### Examining attribute changes

```go
user.IsDirty("name")             // true
user.Dirty()                     // map[name:Ada Lovelace]
user.GetOriginal("name")         // "Ada"
err = user.Refresh(ctx)          // reload in place
fresh, err := orm.Fresh(ctx, user)   // a new instance, user untouched
```

### Upserts

```go
u, created, err := users.FirstOrCreate(ctx, User{Email: e}, User{Name: "Ada"})
u, err = users.UpdateOrCreate(ctx, User{Email: e}, User{Name: "Ada"})

rows, err := users.UpdateOrCreateMany(ctx, []string{"email"}, rows)   // upsert by key columns, one transaction
rows, err = users.FirstOrCreateMany(ctx, []string{"email"}, rows)     // leaves existing rows alone
```

`UpdateOrCreateMany` never rewrites the primary key, `created_at` or `deleted_at`, so a soft-deleted row is not resurrected.

## Deleting models

```go
err = user.Delete(ctx)                     // soft delete when the model has DeletedAt
n, err := users.Destroy(ctx, 1, 2, 3)
n, err = users.Query().Where("id = ?", 1).Delete(ctx)
err = users.Truncate(ctx)
```

### Soft deleting

Embed `orm.SoftDeleteModel` and `Delete` sets `deleted_at` instead of removing the row. Queries skip trashed rows automatically.

```go
user.Trashed()                             // true
err = user.Restore(ctx)
err = user.ForceDelete(ctx)                // really delete

all, err := users.Query().WithTrashed().Get(ctx)
gone, err := users.Query().OnlyTrashed().Get(ctx)
```

## Mass assignment

`Fill`, `Update`, `Make` and `CreateFrom` take a `map[string]any` keyed by column or field name. Add `Fillable()` (an allow-list) or `Guarded()` (a deny-list) to the model; with neither, everything is allowed. A key outside the rules fails with `MASS_ASSIGNMENT`; nothing is dropped silently.

## Serialization

```go
m, err := orm.ToMap(user)      // map[string]any, column names as keys
js, err := orm.JSON(user)
```

`Hidden()` removes columns, `Visible()` keeps only the listed ones, and `Appends()` adds computed values. Loaded relations are nested and extras such as `posts_count` are included. See [Serialization](serialization.md) for per-call options and date formatting, and [Resources](resources.md) to shape an API response by hand.

## Scopes, casts and collections

- [Query scopes](query-scopes.md): reusable constraints, local (`Apply`) and global (`GlobalScopes`).
- [Mutators and casting](mutators-casting.md): hashed, encrypted, enum, date and JSON columns, and your own mutators and accessors.
- [Collections](collections.md): the type every multi-row query returns.

## Events

Lifecycle hooks (`BeforeCreate`, `AfterSave`, …) use GORM's own mechanisms. For typed, per-model events and a global event bus see [Events](events.md).

## Change tracking after a save

```go
user.Name = "Ana"
_ = user.Save(ctx)

user.WasChanged("name")      // true
user.WasChanged("email")     // false
user.GetChanges()            // {"name": "Ana", "updated_at": ...}
user.WasRecentlyCreated()    // true when that Save inserted the row
```

`IsDirty` and `Dirty` describe the changes not yet saved; `WasChanged` and `GetChanges` describe the last save.

## Quiet saves

`SaveQuietly` and `DeleteQuietly` run without model events. `SaveWithoutTimestamps` leaves `updated_at` alone, and also skips events and hooks. `IncrementEach` adds a different amount to several columns in one atomic update:

```go
user.IncrementEach(ctx, map[string]int{"credits": 5, "visits": 1})
```

## Replicating

```go
copy, err := orm.Replicate(ctx, post, "slug")   // clear the slug too
copy.Title += " (copy)"
_ = copy.Save(ctx)
```

The copy loses its primary key, timestamps, soft-delete mark, loaded relations and the named columns. `before:replicate` listeners may abort it.

## Comparing models

`orm.Is(a, b)` is true for the same model type and the same non-zero primary key; `orm.IsNot` negates it.

## Default attributes

```go
func (Post) Defaults() map[string]any { return map[string]any{"status": "draft"} }

post, _ := posts.Make(map[string]any{"title": "Hi"})   // Status == "draft"
```

Defaults apply in `Make` and `CreateFrom`, before the attributes you pass. A struct literal is never touched: Go cannot tell a field left at its zero value from one set to it.

## UUID and ULID keys

Embed `orm.UUIDModel` (UUID version 7, time ordered) or `orm.ULIDModel` instead of `BaseModel`. They carry the same timestamps and every instance method (`Save`, `Delete`, `Refresh`, `Touch`, relations), with a string key:

```go
type Document struct {
    orm.UUIDModel
    Title string
}

type Archive struct {
    orm.SoftDeleteULIDModel   // soft deletes too; also SoftDeleteUUIDModel
    Name string
}
```

The key is generated on insert when empty; one you set is kept. Foreign keys that point at these models are plain `string` columns. `orm.NewUUID()` and `orm.NewULID()` generate the values directly, and `make:model Document --uuid` (or `--ulid`) scaffolds the model.

`orm.UUIDKey` and `orm.ULIDKey` are the bare key columns, for a model that brings its own timestamps. Embed `orm.Record` next to them to get the instance methods back.

## Bulk writes

```go
posts.Upsert(ctx, rows, []string{"slug"}, []string{"title", "views"})   // insert, or update these columns on conflict
posts.Upsert(ctx, rows, []string{"slug"}, nil)                          // update every column except the keys
posts.InsertOrIgnore(ctx, rows)                                         // skip rows that violate a unique key
model, exists, err := posts.FirstOrNew(ctx, Post{Slug: "hi"}, Post{Title: "Hi"})   // not saved when missing
```

`uniqueBy` needs a unique index. MySQL ignores it and uses whichever unique key conflicts. PostgreSQL, MySQL and SQLite use their own `ON CONFLICT` form. SQL Server and Oracle have no such clause, so `Upsert` writes a `MERGE` (in batches, filling UUID keys and timestamps as `Create` would); `InsertOrIgnore` is refused there with `UNSUPPORTED_FEATURE`, because GORM would drop the clause and run a plain insert. Redshift has no upsert and is refused for both. The `MERGE` statements are checked by rendering tests, not against a live SQL Server or Oracle.

## Pruning models

Say which rows are stale and delete them in bulk (Eloquent's `Prunable`):

```go
func (Session) Prunable(q *orm.Query[Session]) *orm.Query[Session] {
    return q.Where("expires_at < ?", time.Now())
}

n, err := orm.New[Session](db).Prune(ctx)
```

`Prunable` deletes row by row, for good (soft-deleted rows included), firing model events and calling a `Pruning(ctx) error` hook if the model has one. `MassPrunable` deletes with one statement and no events. Register models with `orm.RegisterPrunable[Session]("sessions")` and `migro model:prune` (or `orm.PruneAll`) prunes them all.

## Primary keys that are strings

`Find`, `FindMany` and `Destroy` name the primary key column themselves, so string and UUID keys work. GORM alone would read a non-numeric string passed to `First` as raw SQL.
