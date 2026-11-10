---
title: Collections
nav_order: 9.5
description: Working with sets of models — filtering, grouping, keying, loading relations, serializing.
---

# Collections

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

Every query that returns many models returns an `orm.Collection[T]`: `Get`, `All`, `FindMany`, `Paginate` (`Page.Data`) and a many relation's `Get`/`Items`. It is Eloquent's `Collection`, with helpers for the things you do with a list of models.

```go
users, err := orm.New[User](db).Query().Where("active = ?", true).Get(ctx)   // orm.Collection[User]

fmt.Println(users.Count(), users.First().Name)
```

A `Collection[T]` is a defined slice type (`type Collection[T any] []T`), so it is also a plain slice:

```go
for i := range users { fmt.Println(users[i].Email) }   // index, range, len, append
var list []User = users                                // assignable both ways, no conversion
users = append(users, extra)
```

Existing code that expected `[]User` keeps compiling.

{: .important }
Go copies structs by value. Iterate with an index (`&users[i]`) or use the callbacks below, which hand you pointers, when you need to modify a model or call `Save` or a lazy relation on it. `for _, u := range users` gives you a detached copy.

## Available methods

Methods that need a **column** or the **primary key** (`Pluck`, `KeyBy`, `Find`, …) require bound models and return `UNBOUND_MODEL` on a collection of hand-built structs. An unknown column is `INVALID_SCHEMA`.

| Method | Returns |
|---|---|
| [`Count`](#count-isempty-first-last), [`IsEmpty`](#count-isempty-first-last), [`First`](#count-isempty-first-last), [`Last`](#count-isempty-first-last) | size and ends |
| [`Each`](#each-filter-reject) | walks with an index and a pointer |
| [`Filter`](#each-filter-reject), [`Reject`](#each-filter-reject) | a smaller collection |
| [`MapTo`](#mapto-and-reduce), [`Reduce`](#mapto-and-reduce) | another type / one value |
| [`Pluck`](#pluck-and-pluckas), [`PluckAs`](#pluck-and-pluckas) | one column's values |
| [`ModelKeys`](#modelkeys-find-contains) | the primary keys |
| [`Find`](#modelkeys-find-contains), [`Contains`](#modelkeys-find-contains) | lookup by primary key |
| [`Only`](#only-and-except), [`Except`](#only-and-except) | subset by primary key |
| [`KeyBy`](#keyby-groupby-unique), [`GroupBy`](#keyby-groupby-unique), [`Unique`](#keyby-groupby-unique) | index / buckets / distinct |
| [`Load`](#loading-relations), [`LoadMissing`](#loading-relations) | eager-load relations |
| [`Fresh`](#fresh) | reload from the database |
| [`ToMaps`](#serialization), [`JSON`](#serialization) | serialize |

### Count, IsEmpty, First, Last

```go
users.Count()      // 3
users.IsEmpty()    // false
users.First()      // *User, or nil when empty
users.Last()       // *User, or nil
```

`First` and `Last` return a pointer **into the collection**, so editing it edits the element.

### Each, Filter, Reject

```go
err := users.Each(func(i int, u *User) error {
    return u.Update(ctx, map[string]any{"synced": true})   // return an error to stop the walk
})

admins := users.Filter(func(u *User) bool { return u.Role == "admin" })
others := users.Reject(func(u *User) bool { return u.Role == "admin" })
```

`Filter` and `Reject` return a **new** collection. Its models are copies that stay bound to the database, so `Save`, `Refresh` and lazy relations work on them, and they keep their dirty state. Editing a copy does not change the original collection.

### MapTo and Reduce

Go methods cannot add type parameters, so these two are functions:

```go
emails := orm.MapTo(users, func(u *User) string { return u.Email })               // []string
total  := orm.Reduce(orders, 0.0, func(sum float64, o *Order) float64 { return sum + o.Total })
```

### Pluck and PluckAs

```go
ids, err := users.Pluck("id")                          // []any
names, err := orm.PluckAs[User, string](users, "name") // []string
```

`Pluck` takes a **column name** (`id`, `created_at`). `PluckAs` asserts each value to the type you ask for; a mismatch is `CAST_FAILED` rather than a panic. To pluck in SQL instead of in memory, use `orm.Pluck(ctx, query, col)`.

### ModelKeys, Find, Contains

```go
keys, err := users.ModelKeys()          // the primary keys, in order
u, err := users.Find(2)                 // *User, or nil — no error when absent
has, err := users.Contains("2")         // true
```

Keys compare **by printed value**, so `2`, `uint(2)` and `"2"` all match. `Find` searches the collection in memory; it is not `Model.Find`.

### Only and Except

```go
some, err := users.Only(1, 3)      // the models with those keys
rest, err := users.Except(1, 3)    // everything else
```

### KeyBy, GroupBy, Unique

```go
byID, err := users.KeyBy("id")                   // map[any]User
byID[uint(1)].Name                               // keys keep the field's Go type

byAuthor, err := posts.GroupBy("user_id")        // map[any]orm.Collection[Post]
byAuthor[uint(1)].Count()

firstPerAuthor, err := posts.Unique("user_id")   // the first post of each author
```

- Map keys keep the **Go type of the field** (`uint` for `ID`, `string` for `Slug`), so index with `uint(1)`, not `1`.
- Pointer columns (`CategoryID *uint`) key by the dereferenced value, and `NULL` rows are skipped by `KeyBy` and `GroupBy`.
- `KeyBy` keeps the **last** model for a repeated key; the map holds plain copies. `GroupBy` buckets are bound collections like `Filter` results.
- `Unique` keeps the first model for each distinct value (`NULL` counts as one value).

## Loading relations

`Load` eager-loads relations onto every model with **one query per relation**, the same engine as `With`:

```go
users, _ := orm.New[User](db).Query().Get(ctx)         // decided later that we need posts
err := users.Load(ctx, "Posts.Comments", "Roles")       // 3 queries total, not 3 per user
```

`LoadMissing` loads a relation only onto the models that do not have it yet, and never replaces one already loaded:

```go
err := users.LoadMissing(ctx, "Posts")
```

Both take the same dotted paths as `With`. On an empty collection they do nothing. Use them when the decision to load comes after the query (for example, a branch in a service), and `With` when you know up front.

## Fresh

`Fresh` reloads the same rows from the database and returns **new** models, in the **original order**; rows deleted in the meantime are dropped:

```go
latest, err := users.Fresh(ctx)
```

The originals are untouched (compare with `Refresh`, which updates one model in place). Soft-deleted rows and rows hidden by a [global scope](query-scopes.md) count as gone.

## Serialization

```go
maps, err := users.ToMaps()                              // []map[string]any
js, err := users.JSON()                                  // a JSON array
js, err = users.JSON(orm.MakeHidden("email"), orm.DateFormat("2006-01-02"))
```

Same options as [serialization](serialization.md#temporarily-changing-visibility). For API shapes use a [resource](resources.md): `UserResource.Collection(users)`.

## Collections and relationships

A many relation hands out a collection too, so the helpers work on related models:

```go
posts, _ := user.Posts.Get(ctx)                  // orm.Collection[Post]
published := user.Posts.Items().Filter(func(p *Post) bool { return p.Published })
```

`Items()` reads what is loaded without a query; `Get(ctx)` loads on first use.

## Choosing between a collection and a query

Filtering in memory is right for a few hundred rows you already have. For anything larger, or anything the database does well, put the condition in the query ([scopes](query-scopes.md) help) and keep the collection for what SQL cannot do:

| Prefer | When |
|---|---|
| `Where` / a scope | the set is large, or you only need part of it |
| `Filter` | the rows are already loaded and the test is Go code |
| `orm.Pluck` (query) | you need one column of a large table |
| `Collection.Pluck` | you already hold the models |
| `Chunk` | walking a big table without holding it all |

## Testing

`integration/collections_test.go` covers return types across `Get`, `All`, `FindMany`, `Paginate` and relations, `Filter`/`Reject` keeping models bound and saveable, every column helper, `Load` versus `LoadMissing` (identity of already-loaded rows), and `Fresh` keeping order while dropping deleted rows.
