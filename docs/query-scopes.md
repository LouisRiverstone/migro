---
title: Query scopes
nav_order: 9.1
description: Reusable query constraints — local scopes with arguments, global scopes, and switching them off.
---

# Query scopes

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

A scope is a reusable piece of a query. It is how you give a name to `published = true` so the rest of the application says `Published` instead of repeating the condition. migro has the two kinds Laravel and Lucid have:

- **Local scopes** are functions you apply on demand (Lucid's `scope()`, Eloquent's `scopePopular`).
- **Global scopes** are declared by the model and applied to every query on it (Eloquent's `addGlobalScope`).

Both are plain Go, so the compiler checks the model type and your editor completes the names.

## Local scopes

A local scope is any function from a query to a query, for the model it filters:

```go
func PublishedPosts(q *orm.Query[Post]) *orm.Query[Post] {
    return q.Where("published = ?", true)
}
```

Apply it with `Apply`:

```go
posts, err := orm.New[Post](db).Query().Apply(PublishedPosts).Latest().Get(ctx)
```

`Apply` takes any number of scopes and runs them in order, so scopes compose with each other and with the ordinary query methods:

```go
q := orm.New[Post](db).Query().
    Apply(PublishedPosts, PostsBy(1)).    // two scopes
    Where("created_at > ?", since).       // a plain clause
    OrderByDesc("created_at")
```

### Scopes with arguments

Return the scope from a function, which gives you a closure over the arguments (Eloquent's *dynamic scopes*):

```go
func PostsBy(userID uint) func(*orm.Query[Post]) *orm.Query[Post] {
    return func(q *orm.Query[Post]) *orm.Query[Post] { return q.Where("user_id = ?", userID) }
}

func TitleContains(term string) func(*orm.Query[Post]) *orm.Query[Post] {
    return func(q *orm.Query[Post]) *orm.Query[Post] { return q.Where("title LIKE ?", "%"+term+"%") }
}

orm.New[Post](db).Query().Apply(PostsBy(1), TitleContains("Hello")).Get(ctx)
```

### Where to put them

Keep scopes in the model's package, next to the model, as the example project does (`integration/models/scopes.go`). A scope is a function, so it is documented, tested and imported like any other.

### Scopes on a relation

The query a relation hands out is a normal `Query`, so scopes apply to it too:

```go
q, err := user.Posts.Query()
n, err := q.Apply(PublishedPosts).Count(ctx)
```

## Global scopes

A global scope is a constraint every query on the model gets without asking. Typical uses are multi-tenancy (`tenant_id = ?`), hiding archived or unpublished rows, and default ordering.

Implement `GlobalScopes` on the model. Each scope has a name and a function over `*gorm.DB`:

{% raw %}
```go
func (Post) GlobalScopes() []orm.NamedScope {
    return []orm.NamedScope{{
        Name: "not_archived",
        Fn:   func(g *gorm.DB) *gorm.DB { return g.Where("archived = ?", false) },
    }}
}
```
{% endraw %}

From then on an archived post does not exist as far as the repository is concerned:

```go
posts := orm.New[Post](db)

posts.All(ctx)                    // no archived posts
posts.Find(ctx, archivedID)       // NOT_FOUND
posts.FindMany(ctx, 1, 2, 3)      // archived ones are absent
posts.Query().Count(ctx)          // counts without them
```

### What a global scope applies to

| Entry point | Scoped |
|---|---|
| `Query()`, `All`, `Find`, `FindMany`, `FindBy` | yes |
| `Count`, `Exists`, `First`, `Paginate`, `Chunk`, `Pluck`, aggregates | yes |
| `Query().Update`, `Delete`, `Increment` | yes — rows outside the scope are not touched |
| Eager loading (`With("Posts")`) and lazy loading (`user.Posts.Get`) | yes, with the **related** model's scopes |
| `user.Posts.Query()` | yes |
| Existence queries (`Has`, `WhereHas`) and `WithCount` | no: they run as subqueries of the owner |
| Raw GORM through `db.Gorm` | no |

{: .note }
Instance methods (`user.Save`, `user.Delete`, `user.Refresh`) act on the row you already hold by its primary key. A scope decides what you can *find*, not what you can do to a model you have.

### Removing global scopes

Drop one by name, or all of them, for a single query:

```go
posts.Query().WithoutGlobalScope("not_archived").Get(ctx)   // include archived
posts.Query().WithoutGlobalScopes().Count(ctx)              // no global scope at all
```

This is decided when the query runs, so the order of the calls does not matter. An unknown name is ignored.

Soft deletes are separate: `WithTrashed()` and `OnlyTrashed()` control `deleted_at`; they do not touch your scopes.

### Scopes and errors

A global scope returning the `*gorm.DB` unchanged is a no-op. If it calls `g.AddError(...)`, the query fails with that error, wrapped as `QUERY_FAILED`.

## Choosing between them

| You want | Use |
|---|---|
| A named filter some queries need | a local scope |
| A filter with parameters | a local scope returning a closure |
| A rule that must never be forgotten (tenant, archived) | a global scope |
| The same rule, except for the admin screen | a global scope plus `WithoutGlobalScope` there |

## Testing scopes

Scopes are plain functions over a query, so test them against a real database like the rest of the ORM. The repository's `integration/scopes_test.go` covers stacking local scopes, every global-scope entry point above, and `WithoutGlobalScope`; it runs on SQLite, PostgreSQL and MySQL.
