---
title: Coming from Laravel or Lucid
nav_order: 2.6
description: Eloquent, Lucid and artisan equivalents in migro.
---

# Coming from Laravel or Lucid

migro follows Eloquent's and Lucid's names, so most code translates one to one. Where Go forces a difference, it is listed below.

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Laravel's docs, page by page

The guide follows the Database and Eloquent sections of [Laravel's documentation](https://laravel.com/docs) (13.x), in the same order and with the same section names. Each page also says what has no counterpart, and what to do instead.

| Laravel page | migro page |
|---|---|
| Database: Getting Started | [Database: getting started](connecting.md), [Transactions](transactions.md), [Pool, replicas, sharding](scaling.md) |
| Database: Query Builder | [Query builder](query-builder.md) |
| Database: Pagination | [Pagination](pagination.md) |
| Database: Migrations | [Migrations](migrations.md), [Schema builder](schema-builder.md), [Schema dump](schema-dump.md) |
| Database: Seeding | [Seeding](seeding.md) |
| Eloquent: Getting Started | [Models](models.md), [Query scopes](query-scopes.md), [Events](events.md), [Strict mode](strict-mode.md) |
| Eloquent: Relationships | [Relations](relations.md) |
| Eloquent: Collections | [Collections](collections.md) |
| Eloquent: Mutators & Casting | [Mutators and casting](mutators-casting.md) |
| Eloquent: API Resources | [Resources](resources.md) |
| Eloquent: Serialization | [Serialization](serialization.md) |
| Eloquent: Factories | [Factories](factories.md) |
| Database: Redis, MongoDB | not part of migro |

## Models and queries

| Eloquent / Lucid | migro |
|---|---|
| `User::find(1)` | `users.Find(ctx, 1)` |
| `User::findOrFail(1)` | `users.FindOrFail(ctx, 1)` |
| `User::where('a', 1)->get()` | `users.Query().Where("a = ?", 1).Get(ctx)` |
| `->first()` / `->sole()` | `.First(ctx)` / `.Sole(ctx)` |
| `->with('posts')` | `.With("Posts")` |
| `->withCount('posts')` | `.WithCount("Posts")` |
| `->has('posts')` / `->whereHas(...)` | `.Has("Posts")` / `.WhereHas("Posts", fn)` |
| `->paginate(20)` | `.Paginate(ctx, page, 20)` |
| `->cursorPaginate()` | `.CursorPaginate(...)` |
| `->latest()` | `.Latest()` |
| `->lockForUpdate()` | `.LockForUpdate()` |
| `->withTrashed()` / `->onlyTrashed()` | `.WithTrashed()` / `.OnlyTrashed()` |
| `User::create([...])` | `users.CreateFrom(ctx, map[string]any{...})` |
| `firstOrCreate` / `updateOrCreate` | `FirstOrCreate` / `UpdateOrCreate` |
| `User::upsert(...)` | `users.Upsert(ctx, rows, uniqueBy...)` |
| `$user->save()` / `isDirty()` | `u.Save(ctx)` / `u.IsDirty("name")` |
| `$fillable` / `$hidden` / `$appends` | `Fillable()` / `Hidden()` / `Appends()` methods |
| global scopes | `GlobalScopes()` method; `WithoutGlobalScope(name)` |
| `Model::shouldBeStrict()` | `db.ShouldBeStrict(true)` |
| `$casts` | [casts](mutators-casting.md) (`orm.CastJSON()`, `CastHashed()`, ...) |
| API resources | [resources](resources.md) |

## Relations

`hasMany`, `belongsTo`, `belongsToMany`, `hasManyThrough` and the morph family are typed fields instead of methods:

```go
type Post struct {
	orm.BaseModel
	UserID uint
	Author orm.BelongsTo[User] `orm:"foreignKey:user_id"`
	Tags   orm.MorphToMany[Tag] `orm:"morph:taggable"`
}
```

Pivot writes (`attach`, `detach`, `sync`, `toggle`) are `Attach`, `Detach`, `Sync` and `Toggle`.

## Migrations and seeders

| artisan / ace | migro |
|---|---|
| `make:migration` | `migro make:migration` |
| `migrate`, `migrate:rollback`, `migrate:fresh --seed` | `migro migration:run`, `migration:rollback`, `migration:fresh --seed` (the `migrate:*` names also work) |
| `migrate:status` | `migro migration:status` |
| `db:seed --class=X` | `migro db:seed --class=X` |
| `schema:dump` | `migro schema:dump` |
| `make:model -mfs` | `migro make:model Name -m -f -s` |
| `db:show`, `db:table`, `model:show` | same names |

Migrations are Go files with `Up` and `Down`. The [schema builder](schema-builder.md) mirrors Laravel's `Blueprint` (`Increments`, `ForeignID().Constrained()`, `Timestamps()`).

## What differs on purpose

- **`ctx` everywhere.** Go has no request-scoped global; pass `context.Context`.
- **Errors, not exceptions.** Every call returns an `error` with a code. See [Errors](errors.md).
- **No magic attribute access.** Fields are Go struct fields, and unknown columns do not exist at runtime.
- **Observers register explicitly** on an `events.Bus`: `events.Observe[User](bus, &UserObserver{})`.
- **Mass assignment** only applies to `Fill`/`CreateFrom`. Assigning struct fields directly is always allowed.
