---
title: Relations
nav_order: 9
description: Defining, loading, querying and writing relations — one to one, one to many, many to many, has many through and polymorphic.
---

# Relations

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

Database tables are often related to one another: a blog post has many comments, an order belongs to a user. migro's relations are **typed fields on the model**, the Go counterpart of Eloquent's relationship methods and Lucid's decorators:

```go
type User struct {
    orm.BaseModel
    Name  string
    Posts orm.HasMany[Post]
}
```

You do not tag the field for GORM: relation fields hide themselves from it, so they never become columns and never reach an `INSERT`. Names follow Laravel's conventions (`user_id`, `role_user`, `commentable_type`), and the optional `orm:"..."` tag overrides any of them (see [Customizing keys](#customizing-keys)).

| Field type | Eloquent equivalent |
|---|---|
| `HasOne[T]` | `hasOne` |
| `HasMany[T]` | `hasMany` |
| `BelongsTo[T]` | `belongsTo` |
| `BelongsToMany[T]` | `belongsToMany` |
| `HasManyThrough[T, Through]` | `hasManyThrough` |
| `MorphOne[T]` | `morphOne` |
| `MorphMany[T]` | `morphMany` |
| `MorphTo` | `morphTo` |
| `MorphToMany[T]` | `morphToMany` |
| `MorphedByMany[T]` | `morphedByMany` |

A relation is only usable on a [bound model](models.md#bound-models) (one that came from `Find`, `Get`, `First`, a relation or a factory). Calling a relation method on a struct you built by hand returns `UNBOUND_MODEL`.

## Defining relationships

### One to one

A `User` has one `Phone`. Put a `HasOne` on the owner; the foreign key lives on the related table.

```go
type User struct {
    orm.BaseModel
    Name  string
    Phone orm.HasOne[Phone]
}

type Phone struct {
    orm.BaseModel
    UserID uint        // phones.user_id
    Number string
}
```

migro infers the foreign key from the owner's name: `User` → `user_id`. It also assumes `user_id` matches the owner's `id`. Override either with `foreignKey` and `localKey`.

Read it with `user.Phone.Item()` once loaded (it is `nil` when there is no row), or `user.Phone.Get(ctx)` to load on demand:

```go
phone, err := user.Phone.Get(ctx)    // *Phone, nil when missing
```

**Defining the inverse.** Add a `BelongsTo` on the related model:

```go
type Phone struct {
    orm.BaseModel
    UserID uint
    User   orm.BelongsTo[User]
}
```

### One to many

A `Post` has many `Comment`s:

```go
type Post struct {
    orm.BaseModel
    Title    string
    Comments orm.HasMany[Comment]
}

type Comment struct {
    orm.BaseModel
    PostID uint        // comments.post_id
    Body   string
}
```

```go
comments, err := post.Comments.Get(ctx)       // []Comment, loaded on first use and cached
post.Comments.Items()                          // the same slice, no query
```

The default foreign key is `<owner snake name>_id`.

### One to many (inverse) / belongs to

To reach the parent from the child, use `BelongsTo`:

```go
type Comment struct {
    orm.BaseModel
    PostID uint
    Post   orm.BelongsTo[Post]
}

post, err := comment.Post.Get(ctx)
```

For `BelongsTo` the foreign key is derived from the **field name**, not the related type: `Post` → `post_id`. A field called `Author` would look for `author_id`, so point it somewhere else explicitly:

```go
Author orm.BelongsTo[User] `orm:"foreignKey:user_id"`
```

`relatedKey` changes which column of the related table the foreign key references (default: its primary key).

### Many to many

A `User` has many `Role`s and a `Role` has many `User`s. Three tables are involved: `users`, `roles` and the pivot `role_user`.

```go
type User struct {
    orm.BaseModel
    Roles orm.BelongsToMany[Role]
}

type Role struct {
    orm.BaseModel
    Name  string
    Users orm.BelongsToMany[User]
}
```

Defaults: the pivot is named after the two models in alphabetical order (`role_user`) and has `user_id` (the owner) and `role_id` (the related model). Override them:

```go
Roles orm.BelongsToMany[Role] `orm:"pivotTable:user_roles;pivotForeignKey:member_id;pivotRelatedForeignKey:role_id"`
```

**Pivot columns.** Extra columns on the pivot are not loaded unless you list them. They appear in `Extras()` as `pivot_<column>`:

```go
Roles orm.BelongsToMany[Role] `orm:"pivotColumns:assigned_by,expires_at;pivotTimestamps:true"`

roles, _ := user.Roles.Get(ctx)
roles[0].Extra("pivot_assigned_by")        // "root"
```

`pivotTimestamps:true` fills `created_at` / `updated_at` on the pivot rows migro writes.

### Has many through

A `Country` has many `Post`s *through* `User`: countries → users → posts. The "through" model sits in the middle.

```go
type Country struct {
    orm.BaseModel
    Users orm.HasMany[User]
    Posts orm.HasManyThrough[Post, User]
}
```

Defaults: `users.country_id` points at the country and `posts.user_id` points at the user. Override with `foreignKey` (column on the through table), `throughForeignKey` (column on the target table), `localKey` and `throughKey`:

```go
Posts orm.HasManyThrough[Post, User] `orm:"foreignKey:country_id;throughForeignKey:author_id"`
```

Through relations are read-only: there is no `Create`/`Attach`.

## Polymorphic relationships

A polymorphic relation lets a child belong to more than one kind of parent through a single association. migro stores two columns, `<name>_type` and `<name>_id`, and you name the association with `orm:"morph:<name>"`. A missing `morph:` name fails with `INVALID_SCHEMA` and tells you which field.

### Polymorphic one to one

An `Image` can belong to a `Post` or a `User`.

```go
type Image struct {
    orm.BaseModel
    URL           string
    ImageableID   uint      // images.imageable_id
    ImageableType string    // images.imageable_type
    Imageable     orm.MorphTo `orm:"morph:imageable"`
}

type Post struct {
    orm.BaseModel
    Image orm.MorphOne[Image] `orm:"morph:imageable"`
}

type User struct {
    orm.BaseModel
    Image orm.MorphOne[Image] `orm:"morph:imageable"`
}
```

### Polymorphic one to many

Same shape, with `MorphMany`. A `Post` and a `Video` both have comments:

```go
type Comment struct {
    orm.BaseModel
    Body            string
    CommentableID   uint
    CommentableType string
    Commentable     orm.MorphTo `orm:"morph:commentable"`
}

type Post struct {
    orm.BaseModel
    Comments orm.MorphMany[Comment] `orm:"morph:commentable"`
}

type Video struct {
    orm.BaseModel
    Comments orm.MorphMany[Comment] `orm:"morph:commentable"`
}
```

Every query on `post.Comments` adds `commentable_type = 'post'` next to `commentable_id`, so a post never sees a video's comments.

### Morph to (the inverse)

`MorphTo` reads the type and id on the child and returns the right parent. Its value is `any`, a pointer to the registered model:

```go
target, err := comment.Commentable.Get(ctx)      // *Post or *Video, nil when the parent is gone
switch t := target.(type) {
case *Post:
    fmt.Println(t.Title)
case *Video:
    fmt.Println(t.Title)
}
```

`MorphTo` can only resolve aliases in the [morph map](#the-morph-map). An unknown one returns `UNKNOWN_MORPH_TYPE`. Its name defaults to the field name, so `Commentable` needs no tag at all.

### Polymorphic many to many

A `Post` and a `Video` both have many `Tag`s through one `taggables` pivot (`tag_id`, `taggable_id`, `taggable_type`).

```go
type Post struct {
    orm.BaseModel
    Tags orm.MorphToMany[Tag] `orm:"morph:taggable"`
}

type Video struct {
    orm.BaseModel
    Tags orm.MorphToMany[Tag] `orm:"morph:taggable"`
}

// The inverse: one MorphedByMany per kind of owner.
type Tag struct {
    orm.BaseModel
    Name   string
    Posts  orm.MorphedByMany[Post]  `orm:"morph:taggable"`
    Videos orm.MorphedByMany[Video] `orm:"morph:taggable"`
}
```

The pivot defaults to `<name>s` (`taggables`). All pivot writes keep `taggable_type` in the `WHERE`, so a post's `Sync` never touches a video's links.

### The morph map

The value stored in a `*_type` column is an alias, not a Go type name. Register the aliases once, at start-up (Laravel's `Relation::morphMap`):

```go
orm.MorphMap(map[string]any{"post": Post{}, "video": Video{}})
```

Types missing from the map use their snake_case name (`Post` → `post`). Registering aliases decouples your data from your type names, so a rename does not corrupt existing rows.

## Customizing keys

Every default can be overridden on the field's `orm:"..."` tag. Separate keys with `;`.

| Key | Applies to | Meaning |
|---|---|---|
| `foreignKey` | has one/many, belongs to, has many through | The foreign key column |
| `localKey` | has one/many, morph one/many, many to many | The owner's key the foreign key points at (default: its primary key) |
| `relatedKey` | belongs to, many to many | The related model's key the foreign key points at |
| `pivotTable` | many to many | Pivot table name |
| `pivotForeignKey` | many to many | Pivot column that holds the owner key |
| `pivotRelatedForeignKey` | many to many | Pivot column that holds the related key |
| `pivotColumns` | many to many | Extra pivot columns to load, comma separated |
| `pivotTimestamps` | many to many | `true` to maintain `created_at`/`updated_at` on the pivot |
| `morph` | polymorphic | Name of the `<name>_type` / `<name>_id` pair |
| `throughKey` | has many through | The through model's key referenced by the target |
| `throughForeignKey` | has many through | Column on the target pointing at the through model |

Defaults at a glance:

| Relation | Defaults |
|---|---|
| `HasOne`, `HasMany` | `<owner>_id` on the related table, owner key `id` |
| `BelongsTo` | `<field>_id` on the owner, related key `id` |
| `BelongsToMany` | pivot `<a>_<b>` sorted, `<owner>_id` and `<related>_id` |
| `HasManyThrough` | `<owner>_id` on Through, `<through>_id` on the target |
| `MorphOne`, `MorphMany` | `<name>_type` and `<name>_id` on the related table |
| `MorphToMany`, `MorphedByMany` | pivot `<name>s`, `<name>_type`, `<name>_id` and `<related>_id` |

## Querying relations

### Lazy loading

`Get(ctx)` loads the relation the first time and caches it. `Items()` (many) and `Item()` (one) read the cache without touching the database; `Loaded()` tells you which state you are in.

```go
posts, err := user.Posts.Get(ctx)       // query
posts, err = user.Posts.Get(ctx)        // cached

user.Posts.Loaded()                      // true
user.Posts.Items()                       // []Post
user.Profile.Item()                      // *Profile, or nil
```

Lazy loading runs one query per model. Loading a relation for many models in a loop is the classic N+1 problem; use [eager loading](#eager-loading) instead.

> Go copies structs by value and a copy is detached from its database binding. Keep pointers (`&rows[0]`, not `row := rows[0]`) when you rely on lazy relations.

### Constraining the relation query

`Query()` returns a normal `Query[T]` already limited to this owner, so you can keep chaining:

```go
q, err := user.Posts.Query()
titles, err := orm.Pluck[Post, string](ctx, q.Where("published = ?", true), "title")

recent, err := q.OrderBy("created_at", "desc").Limit(5).Get(ctx)
```

### Existence queries

Select models based on whether (and how) a relation exists. They compile to a subquery.

```go
users := orm.New[User](db).Query()

users.Has("Posts")                                  // at least one post
users.DoesntHave("Posts")                           // none
users.WhereHas("Posts", func(q *gorm.DB) *gorm.DB { // at least one that matches
    return q.Where("published = ?", true)
})
users.WhereDoesntHave("Roles", scope)
```

Supported: `BelongsTo`, `HasOne`/`HasMany`, `BelongsToMany`, `HasManyThrough` and the morph relations. `MorphTo` is not (the target table is not known).

### Counting related models

`WithCount` adds the number of related rows without loading them. The value lands in `Extras()` as `<relation>_count`:

```go
users, err := orm.New[User](db).Query().WithCount("Posts", "Roles").Get(ctx)
users[0].Extra("posts_count")      // 12
users[0].Extra("roles_count")      // 2
```

Supported for everything except `BelongsTo`, `HasManyThrough` and `MorphTo`.

## Eager loading

Eager loading replaces N+1 with one query per relation (two for pivot and through relations), never one per row:

```go
users, err := orm.New[User](db).Query().
    With("Profile").
    With("Posts.Comments").                              // nested: posts, then their comments
    With("Roles", func(q *gorm.DB) *gorm.DB {             // constrained: applies to the last relation
        return q.Where("name <> ?", "banned")
    }).
    Get(ctx)
```

- **Nested** relations use dot notation. Loading the same relation twice in one query (`With("Posts.Comments").With("Posts.Tags")`) loads `Posts` once.
- **Constraints** are a `func(*gorm.DB) *gorm.DB` that receives the relation query.
- **Lazy eager loading** loads onto a model you already hold (Eloquent's `$user->load(...)`):

  ```go
  err := orm.Load(ctx, db, user, "Roles", "Posts.Comments")
  ```

- Rows loaded through a many-to-many relation carry the pivot columns in `Extras()` as `pivot_<column>`.

## Inserting and updating related models

### Has one / has many

`Create`, `Save` and `CreateMany` set the foreign key (and the morph type for polymorphic relations) for you:

```go
// Build from attributes; mass-assignment rules apply
post, err := user.Posts.Create(ctx, map[string]any{"title": "Hi"})

// Save a model you already have: sets user_id, then saves
p := &Post{Title: "Hello"}
err = user.Posts.Save(ctx, p)

// Several at once, in a transaction
posts, err := user.Posts.CreateMany(ctx, []map[string]any{
    {"title": "One"}, {"title": "Two"},
})

// Polymorphic: also sets commentable_type and commentable_id
comment, err := post.Comments.Create(ctx, map[string]any{"body": "Nice"})
```

`HasOne` and `MorphOne` have `Create` and `Save`; `HasMany` and `MorphMany` add `CreateMany`.

### Belongs to: associate / dissociate

`Associate` points the foreign key at a parent, `Dissociate` clears it. Neither saves the child, so call `Save` afterwards (as with Eloquent):

```go
err = comment.User.Associate(&author)
err = comment.Save(ctx)

err = comment.User.Dissociate()     // the column must be nullable
err = comment.Save(ctx)
```

### Many to many writes

```go
// Attach: link by key or by model; links that already exist are skipped
err = user.Roles.Attach(ctx, 1, role)
err = user.Roles.AttachWith(ctx, map[string]any{"assigned_by": "root"}, 3)

// Detach: with no arguments, removes every link
err = user.Roles.Detach(ctx, 1)

// Sync: make the links exactly this list
res, err := user.Roles.Sync(ctx, 1, 2)                 // res.Attached, res.Detached
res, err = user.Roles.SyncWithoutDetaching(ctx, 4)     // only adds

// Toggle: attach what is missing, detach what is present
res, err = user.Roles.Toggle(ctx, 1, 5)

// Change extra columns of an existing link
err = user.Roles.UpdateExistingPivot(ctx, 1, map[string]any{"assigned_by": "ana"})
```

`Sync` and `Toggle` run in a transaction. `MorphToMany` has the same methods; `MorphedByMany` supports `Attach`, `Detach` and `Sync`. Pivot writes mark the relation as not loaded, so the next `Get` reads fresh data.

## Batch loaders

Before relation fields existed, migro offered key-based loaders: `orm.HasManyThroughLoader`, `orm.MorphToManyLoader`, `orm.MorphedByManyLoader` and `orm.MorphRegistry`. They still work, need no struct fields and return maps keyed by the parent key you pass. Use them for models you cannot change.

```go
rel := orm.HasManyThroughLoader[Story, Citizen]{ForeignKey: "country_id", ThroughForeignKey: "citizen_id"}
byCountry, err := rel.Load(ctx, db, []any{1, 2, 3})        // map[any][]Story
```

Very large key lists are not chunked; split them if your database caps the size of an `IN` list. Composite keys are not supported in relations.

## Has one through

`HasOneThrough[T, Through]` reaches a single `T` through an intermediate model, with the same keys as `HasManyThrough`:

```go
type Mechanic struct {
    orm.BaseModel
    Car   orm.HasOne[Car]
    Owner orm.HasOneThrough[Owner, Car] `orm:"foreignKey:mechanic_id;throughForeignKey:car_id"`
}
```

It serializes as an object (or `null`), not an array.

## Choosing one of many

A `HasOne` or `MorphOne` can pick a single row out of a larger set by tagging it with `ofMany`:

```go
type User struct {
    orm.BaseModel
    LatestOrder   orm.HasOne[Order] `orm:"foreignKey:user_id;ofMany:latest"`
    FirstOrder    orm.HasOne[Order] `orm:"foreignKey:user_id;ofMany:oldest"`
    BiggestOrder  orm.HasOne[Order] `orm:"foreignKey:user_id;ofMany:max:total"`
    CheapestOrder orm.HasOne[Order] `orm:"foreignKey:user_id;ofMany:min:total"`
}
```

`latest` and `oldest` compare the primary key, or the column you add (`ofMany:latest:created_at`). `max:col` and `min:col` compare that column. Eager loading stays one query for all owners; ties pick an arbitrary row.

## Default models

Tag a single relation `withDefault` and a missing row answers with an empty, unsaved model instead of `nil` (Eloquent's `withDefault`):

```go
Author orm.BelongsTo[User] `orm:"foreignKey:author_id;withDefault"`

post.Author.Item().Name   // "" rather than a nil dereference
post.Author.Item().Exists() // false
```

The default appears once the relation has been loaded, and is left out of `ToMap` and JSON.

## Constraining the pivot

Two more keys on many-to-many fields:

| Key | Meaning |
|---|---|
| `pivotWhere` | `col=value` pairs, comma separated. Every read honours them (loading, counts, `Has`, the relation query), and `Attach` stores them. |
| `pivotAs` | Prefix for pivot values in `Extras`. Default `pivot`, so `pivot_assigned_by`. |

```go
Approvals orm.BelongsToMany[Role] `orm:"pivotTable:role_user;pivotColumns:status;pivotWhere:status=approved;pivotAs:link"`
// role.Extra("link_status") == "approved"
```

`Sync` and `Detach` only touch pivot rows that match `pivotWhere`, so another relation over the same pivot table keeps its rows. A custom pivot model (`Using`) is not supported.

## Pivot models

When the pivot rows have a key or hooks of their own, declare the pivot struct (Eloquent's `using`):

```go
type RoleUser struct {
    orm.UUIDKey
    UserID uint
    RoleID uint
    Level  string `gorm:"default:read"`
}

func (User) PivotModels() map[string]any { return map[string]any{"Roles": RoleUser{}} }
```

`Attach`, `AttachWith`, `Sync` and `Toggle` then create each row through that struct, so its `BeforeCreate` hooks run, a UUID key is generated and column defaults apply. The struct must have a column for both pivot keys and for every attribute you pass. Reads and `Detach` are unchanged.

## Touching parents

List belongs-to relations in `Touches` and the parent's `updated_at` moves whenever the child is created, saved, deleted or restored through the repository or the instance methods:

```go
func (Comment) Touches() []string { return []string{"Post"} }
```

Bulk query updates do not touch, as in Eloquent. The touch cascades: when the touched parent declares its own `Touches`, its parent is touched too (comment → post → blog). Each parent is touched once per save, so a cycle ends.

## Saving several, and pushing

```go
err := user.Posts.SaveMany(ctx, []Post{ {Title: "a"}, {Title: "b"} })   // links and saves each, in one transaction
err = orm.Push(ctx, user)                                              // saves user and every loaded relation
```

`Push` saves loaded belongs-to parents first and copies their key onto the model, saves the model, then links and saves the loaded children, all in one transaction.

## Enforcing the morph map

Without a map, a polymorphic model stores its Go type name in `*_type` columns, and renaming the type later orphans every row. `orm.EnforceMorphMap` is `MorphMap` that also refuses models missing from the map, with `UNKNOWN_MORPH_TYPE`:

```go
orm.EnforceMorphMap(map[string]any{"post": Post{}, "video": Video{}})
```
