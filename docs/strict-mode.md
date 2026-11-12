---
title: Strict mode and debugging
nav_order: 8.6
description: "Make lazy loading fail, listen to every query, record a query log and count queries in tests."
---

# Strict mode and debugging

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Preventing lazy loading

The usual N+1 bug is a loop that touches a relation:

```go
for _, post := range posts {
    author, _ := post.Author.Get(ctx)   // one query per post
}
```

Turn on strict mode and that line fails instead:

```go
db.ShouldBeStrict(true)   // Eloquent's Model::shouldBeStrict()

_, err := post.Author.Get(ctx)
// migro.CodeOf(err) == migro.LazyLoadingViolation
// "lazy loading Post.Author is disabled: eager load it with With("Author") or orm.Load"
```

Only the implicit load is refused. A relation loaded with `With`, `orm.Load` or `Collection.Load` reads normally, and `post.Author.Query()` still builds a query on request. A model that is not persisted yet has nothing to lazy load, so it is exempt.

Enable it in development and tests, and log in production:

```go
db.SetStrict(migro.Strict{
    LazyLoading: true,
    OnLazyLoad: func(model, relation string) { log.Printf("lazy load %s.%s", model, relation) },
})
```

When `OnLazyLoad` is set it is called and the load goes ahead, so a violation never breaks a request. Transactions and `WriteDB()` handles share the setting.

### What is always strict

Eloquent also has `preventSilentlyDiscardingAttributes` and `preventAccessingMissingAttributes`. migro needs neither switch: `Fill` fails with `MASS_ASSIGNMENT` for a column that is guarded or does not exist, and `GetAttribute` fails with `INVALID_SCHEMA` for a missing column.

## Listening to queries

```go
off := db.Listen(func(e migro.QueryEvent) {
    log.Printf("%s  %d rows  %s", e.SQL, e.Rows, e.Duration)
})
defer off()
```

`QueryEvent` carries `SQL` (with bindings inlined), `Rows`, `Duration` and `Err`. The listener runs on the goroutine that ran the statement, so keep it fast. Your own GORM logger keeps working: migro wraps it.

## The query log

```go
db.EnableQueryLog()
// ... run code ...
for _, q := range db.QueryLog() { fmt.Println(q.SQL) }
db.FlushQueryLog()
db.DisableQueryLog()
```

## Counting queries in tests

```go
import "github.com/LouisRiverstone/migro/dbtest"

dbtest.AssertQueryCount(t, db, 2, func() {
    posts, _ := orm.New[Post](db).Query().With("Author").Get(ctx)   // posts + authors
    _ = posts
})
```

See [Testing](testing.html) for the other assertions.
