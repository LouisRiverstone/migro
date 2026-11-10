---
title: Resources
nav_order: 9.4
description: API resources — shape model responses by hand, conditional fields, nested resources, collections, pagination.
---

# Resources

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

When you build an API you need a layer between your models and the JSON you return. A **resource** is that layer: a function that turns a model into exactly the fields you decide, independent of the table. It is the counterpart of Laravel's `JsonResource` and `ResourceCollection`.

Resources differ from [serialization](serialization.md) in one way that matters: a resource lists what it **includes**, so a new column on the table never appears in your API until you add it.

## Generating resources

There is no generator; a resource is one variable. Put it next to the model, or in a package of its own for the API layer:

```go
var UserResource = orm.NewResource(func(u *User, r *orm.R) {
    r.Set("id", u.ID)
    r.Set("name", u.Name)
    r.Set("email", u.Email)
})
```

`NewResource[T]` takes a function that receives the model and a builder, `*orm.R`. The resource itself is safe to share between goroutines.

## Concept overview

A resource turns one model into a `map[string]any`, and a few methods wrap that map for a response:

| Method | Returns |
|---|---|
| `Make(&model)` | the wrapped item: `{"data": {...}}` |
| `Collection(models)` | the wrapped list: `{"data": [...]}` |
| `Paginated(page, baseURL?)` | the wrapped list plus `meta` (and `links`) |
| `Map(&model)` | the **bare** item, for nesting inside another resource |
| `List(models)` | the **bare** list, for nesting |

```go
user, _ := orm.New[User](db).Find(ctx, 1)

body, err := UserResource.Make(user)
json.NewEncoder(w).Encode(body)
```

```json
{ "data": { "id": 1, "name": "Ana", "email": "ana@example.com" } }
```

`Make` takes a pointer. `Make`, `Collection` and `Paginated` return `(any, error)`, ready for `json.Marshal`; an error from any field (see `Fail`) is returned instead of a partial body.

## Writing resources

### Setting fields

```go
r.Set("id", u.ID)                       // always included
r.Set("joined", u.CreatedAt.Format("2006-01-02"))
r.Set("posts_count", u.Extra("posts_count"))
```

The resource is plain Go: compute, format and rename however you like. It can read anything on the model, including [casts](mutators-casting.md) via `GetAttribute`.

### Conditional attributes

```go
r.When(u.Role == "admin", "role", u.Role)                       // key only if true
r.MergeWhen(isOwner, map[string]any{"email": u.Email, "phone": u.Phone})
r.Merge(map[string]any{"links": map[string]any{"self": "/users/1"}})
```

- `When(cond, key, value)` adds one field when `cond` is true; otherwise the key is **absent**, not `null`.
- `Merge(fields)` copies several fields; `MergeWhen(cond, fields)` does so conditionally.

Because the function runs per model, conditions can use anything in scope: the model, a closure over the request's user, a feature flag.

### Conditional relationships

`WhenLoaded` includes a relation **only if it was eager loaded**. A resource never triggers a lazy query by accident, so it cannot cause an N+1:

```go
var UserResource = orm.NewResource(func(u *User, r *orm.R) {
    r.Set("id", u.ID)
    r.WhenLoaded("Posts", func() (any, error) {
        return PostResource.List(u.Posts.Items())
    })
})
```

```go
orm.New[User](db).Query().Get(ctx)                  // no "posts" key
orm.New[User](db).Query().With("Posts").Get(ctx)    // "posts": [...]
```

The argument is the Go **field name** (`"Posts"`); the output key is its snake_case (`posts`). A loaded but empty relation is `[]`, not absent. `r.Loaded("Posts")` returns the bool if you need to branch yourself.

### Nested resources

Use `Map` for one model and `List` for many; they return the bare data so the parent controls the wrapping:

```go
var PostResource = orm.NewResource(func(p *Post, r *orm.R) {
    r.Set("id", p.ID)
    r.Set("title", p.Title)
    r.WhenLoaded("Comments", func() (any, error) { return CommentResource.List(p.Comments.Items()) })
    r.WhenLoaded("User", func() (any, error) { return UserResource.Map(p.User.Item()) })
})
```

`Map(nil)` is an error rather than a panic, so check `Item()` for `nil` (a loaded, empty `BelongsTo`) when the relation is optional.

### Reporting errors

If a field needs something that can fail, call `r.Fail(err)`. The first error wins and `Make`/`Collection`/`Paginated` return it:

```go
if url, err := signer.Sign(u.AvatarPath); err != nil {
    r.Fail(err)
} else {
    r.Set("avatar_url", url)
}
```

## Resource collections

```go
users, _ := orm.New[User](db).Query().Get(ctx)
body, err := UserResource.Collection(users)       // {"data": [ {...}, {...} ]}
```

`Collection` takes a `[]User` or an `orm.Collection[User]`.

### Pagination

Pass a `Page` straight from `Paginate`:

```go
page, _ := orm.New[User](db).Query().OrderBy("id", "asc").Paginate(ctx, 2, 15)
body, err := UserResource.Paginated(page)
```

```json
{
  "data": [ ... ],
  "meta": { "current_page": 2, "last_page": 4, "per_page": 15, "total": 52, "from": 16, "to": 30 }
}
```

`from` and `to` are the 1-based positions of the first and last item on the page, and both are `0` for an empty page.

Give a base URL to also get Laravel-style `links`:

```go
body, _ := UserResource.Paginated(page, "https://api.example.com/users?sort=name")
```

```json
"links": {
  "first": "https://api.example.com/users?page=1&sort=name",
  "last":  "https://api.example.com/users?page=4&sort=name",
  "prev":  "https://api.example.com/users?page=1&sort=name",
  "next":  "https://api.example.com/users?page=3&sort=name"
}
```

`prev` is `null` on the first page and `next` is `null` on the last. Existing query parameters are kept and only `page` is set.

## Data wrapping

By default the payload sits under `"data"`. Change or remove that per use:

```go
UserResource.Wrap("users").Collection(users)            // {"users": [...]}
UserResource.WithoutWrapping().Make(user)               // {...}   the bare item
UserResource.WithoutWrapping().Collection(users)        // [ {...}, {...} ]   a bare array
```

These builders return **copies**, so `UserResource` itself stays wrapped in `data` everywhere else.

A bare array has no room for siblings. When the response needs `meta`, `links` or `Additional` keys next to a list, the list keeps its wrapper even with `WithoutWrapping`: `Paginated` always answers `{"data": [...], "meta": {...}}`.

### Adding meta data

`Additional` adds top-level keys next to the payload, like Laravel's `additional()`:

```go
body, _ := UserResource.Additional(map[string]any{"version": 2}).Collection(users)
// {"data": [...], "version": 2}
```

Calls accumulate, and a later key overrides an earlier one. On a `WithoutWrapping` item the keys merge into the item itself.

## A complete handler

```go
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
    page, _ := strconv.Atoi(r.URL.Query().Get("page"))
    users, err := orm.New[User](h.db).Query().
        With("Posts").
        OrderBy("id", "asc").
        Paginate(r.Context(), max(page, 1), 15)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    body, err := UserResource.Paginated(users, "https://api.example.com/users")
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(body)
}
```

## Testing resources

A resource is a function of a model, so assert on its output. `orm/resource_test.go` covers the builder methods, `Fail` and the wrapping rules without a database; `integration/resources_test.go` covers `WhenLoaded` against real eager loads, nested resources, pagination meta and links, and empty pages.
