---
title: Advanced queries
nav_order: 8.5
description: "Where variants, grouping, unions, locks, full-text search, cursor pagination, streaming and relation aggregates."
---

# Advanced queries

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

`Model[T].Query()` returns a `*Query[T]`. Besides the basics in [Models](models.html), it covers most of what Eloquent's builder and Lucid's `ModelQueryBuilder` offer. A query that cannot be built (a bad operator, a feature your database lacks) records the error and returns it from the terminal call (`Get`, `First`, `Count`, ...) as a `*migro.Error`.

## Where clauses

```go
users := orm.New[User](db)

users.Query().WhereColumn("updated_at", ">", "created_at")      // compare two columns
users.Query().WhereLike("email", "%@example.com")               // WhereNotLike negates
users.Query().WhereDate("created_at", "=", "2026-01-31")
users.Query().WhereYear("created_at", ">=", 2026)               // WhereMonth, WhereDay
users.Query().WhereJSON("settings", "address.city", "=", "Rio") // a path inside a JSON column
users.Query().WhereRaw("lower(name) = ?", "ana")                // always pass values as arguments
```

Operators are checked against `= <> != < <= > >=`. JSON path segments must be names or array indexes (`items.0.sku`); anything else fails with `QUERY_FAILED`, because the path is part of the SQL text.

### Subqueries

```go
// users who have at least one post
users.Query().WhereExists(posts.Query().WhereColumn("posts.user_id", "=", "users.id"))
users.Query().WhereNotExists(...)
```

### Relation conditions

```go
users.Query().WhereRelation("Posts", "published = ?", true)  // WhereHas with a plain condition
posts.Query().WhereBelongsTo(user)                           // posts.user_id = user.id
posts.Query().WhereBelongsTo(user, "Editor")                 // name the relation when there are several
comments.Query().WhereHasMorph("Commentable", []string{"post", "video"}, nil)
```

`WhereBelongsTo` finds the belongs-to relation that points at the model you pass. It fails if there is none, or more than one and you did not name it.

## Full-text search

```go
// migration
t.FullText("title", "body")

// query
articles.Query().WhereFullText("go orm", "title", "body")
articles.Query().WhereFullTextBoolean("+go -orm", "title", "body")  // operator syntax
```

| Database | Search | Index created by `t.FullText` |
|---|---|---|
| MySQL, MariaDB | `MATCH ... AGAINST` | `FULLTEXT` index |
| PostgreSQL, CockroachDB | `to_tsvector('simple', ...) @@ plainto_tsquery` (`websearch_to_tsquery` in boolean mode) | GIN index on the same expression |
| SQLite, SQL Server, Oracle, Redshift | not supported | not supported |

On the unsupported databases both the query and the index fail with `UNSUPPORTED_FEATURE`. SQLite needs an FTS virtual table, and SQL Server a full-text catalog; neither can be described by a column list. PostgreSQL searches with the `simple` configuration (no stemming), and the index only helps a query that lists the same columns in the same order.

## Grouping

```go
posts.Query().Select("user_id").GroupBy("user_id").Having("COUNT(*) > ?", 3).Get(ctx)
posts.Query().Distinct("user_id").Get(ctx)
```

## Unions

```go
admins := users.Query().Where("role = ?", "admin")
staff := users.Query().Where("role = ?", "staff")
rows, err := admins.Union(staff).OrderBy("name", "asc").Limit(20).Get(ctx)   // UnionAll keeps duplicates
```

Each side keeps its own conditions and global scopes. The select lists must line up. The result is a query over the combined rows, so `OrderBy`, `Limit`, `Count` and `Paginate` apply to the union.

## Locks

```go
err := db.Transaction(ctx, func(tx *migro.DB) error {
    account, err := orm.New[Account](tx).Query().Where("id = ?", 1).LockForUpdate().First(ctx)
    ...
})
```

`LockForUpdate` adds `FOR UPDATE` and `SharedLock` a shared lock (`FOR SHARE`, or `LOCK IN SHARE MODE` on MySQL and MariaDB). Use them inside a transaction. SQLite ignores both, since it locks the whole database. SQL Server (which locks through table hints) and Oracle's shared lock fail with `UNSUPPORTED_FEATURE`.

## Single records

```go
users.Query().Where("email = ?", e).Sole(ctx)   // NOT_FOUND for none, MULTIPLE_RECORDS_FOUND for several
users.Query().FirstWhere(ctx, "email", e)
```

## Pagination

| Method | Cost | Use it for |
|---|---|---|
| `Paginate(ctx, page, perPage)` | a `COUNT` plus the page | numbered pages with a total |
| `SimplePaginate(ctx, page, perPage)` | the page only (reads one extra row) | "previous / next" links |
| `CursorPaginate(ctx, perPage, cursor)` | the page only, constant time at any depth | infinite scroll, APIs, large tables |

```go
page, err := posts.Query().OrderBy("created_at", "desc").CursorPaginate(ctx, 20, "")
// page.Data, page.NextCursor, page.PrevCursor
next, err := posts.Query().OrderBy("created_at", "desc").CursorPaginate(ctx, 20, page.NextCursor)
```

A cursor page filters on the position of the last row instead of skipping rows, so inserts between requests do not shift it. The query needs at least one `OrderBy`, and the primary key is added as a tie-breaker. Order columns must not be `NULL`. The cursor is an opaque string; one made for a different ordering is refused with `QUERY_FAILED`.

## Streaming

```go
for u, err := range users.Query().Cursor(ctx) { ... }          // one statement, one row at a time; no eager loading
for u, err := range users.Query().With("Posts").Lazy(ctx, 500) { ... }  // chunk by chunk, relations loaded
err := users.Query().Each(ctx, 500, func(u *User) error { ... })
err := users.Query().ChunkByID(ctx, 500, "id", func(rows []User) error { ... })
```

`Chunk` and `ChunkByID` page by key rather than offset, so they stay correct when the callback updates or deletes rows. Breaking out of a `range` loop releases the statement.

## Relation aggregates

```go
users.Query().
    WithCount("Posts").
    WithSum("Orders", "total").WithAvg("Orders", "total").
    WithMin("Orders", "total").WithMax("Orders", "total").
    WithExists("Posts").
    Get(ctx)

u.Extra("orders_sum_total")   // float64, 0 when there are no orders
u.Extra("orders_avg_total")   // float64, nil when there are no orders
u.Extra("posts_exists")       // bool

orm.LoadSum(ctx, db, user, "Orders", "total")   // the same for a model you already hold
orm.LoadCount(ctx, db, user, "Posts", "Comments")
```

Values land in `Extras`, one extra query per aggregate. They work on has-one/many, morph one/many and the many-to-many kinds (the column is read from the related table through the pivot). Column names are validated.

## Debugging

```go
sql, err := users.Query().Where("active = ?", true).Limit(5).ToSQL(ctx)
```

`ToSQL` renders the statement with its bindings inlined and does not run it. See [Strict mode and debugging](strict-mode.html) for the query log.
