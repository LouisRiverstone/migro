---
title: Relations
nav_order: 9
description: Lucid-style relation fields, eager and lazy loading, pivot writes, polymorphic relations.
---

# Relations

Relations are typed fields on the model, like Lucid's decorators. Tag them `gorm:"-"` so GORM does not treat them as columns. Names follow Laravel's conventions (`user_id`, `role_user`, `commentable_type`); the optional `orm:"..."` tag overrides them with column names.

```go
type User struct {
    orm.SoftDeleteModel
    CountryID uint

    Country  orm.BelongsTo[Country]  `gorm:"-"`
    Profile  orm.HasOne[Profile]     `gorm:"-"`
    Posts    orm.HasMany[Post]       `gorm:"-"`
    Roles    orm.BelongsToMany[Role] `gorm:"-"`                           // pivot role_user
    Comments orm.MorphMany[Comment]  `gorm:"-" orm:"morph:commentable"`
}

type Post struct {
    orm.BaseModel
    UserID uint
    Author orm.BelongsTo[User] `gorm:"-" orm:"foreignKey:user_id"`        // field name Author would default to author_id
    Tags   orm.MorphToMany[Tag] `gorm:"-" orm:"morph:taggable"`            // pivot taggables
}
```

| Field type | Eloquent | Defaults |
|---|---|---|
| `HasOne[T]`, `HasMany[T]` | `hasOne`, `hasMany` | `<owner>_id` on T, owner key `id` |
| `BelongsTo[T]` | `belongsTo` | `<field>_id` on the owner |
| `BelongsToMany[T]` | `belongsToMany` | pivot `<a>_<b>` sorted, `<owner>_id` and `<related>_id` |
| `HasManyThrough[T, Through]` | `hasManyThrough` | `<owner>_id` on Through, `<through>_id` on T |
| `MorphOne[T]`, `MorphMany[T]` | `morphOne`, `morphMany` | `orm:"morph:name"` gives `name_type` and `name_id` on T |
| `MorphTo` | `morphTo` | `orm:"morph:name"` (defaults to the field name) |
| `MorphToMany[T]`, `MorphedByMany[T]` | `morphToMany`, `morphedByMany` | pivot `<name>s`, `orm:"morph:name"` |

Tag keys: `foreignKey`, `localKey`, `relatedKey`, `pivotTable`, `pivotForeignKey`, `pivotRelatedForeignKey`, `pivotColumns` (comma list), `pivotTimestamps`, `morph`, `throughForeignKey`, `throughKey`.

A relation declared without `gorm:"-"` (or a missing `morph:` name) fails with `INVALID_SCHEMA` and says which field.

## The morph map

The value stored in a `*_type` column is an alias. Register the aliases once, at start-up (Laravel's `Relation::morphMap`):

```go
orm.MorphMap(map[string]any{"post": Post{}, "video": Video{}})
```

Types missing from the map use their snake_case name (`Post` → `post`). `MorphTo` can only resolve registered aliases; an unknown one returns `UNKNOWN_MORPH_TYPE`.

## Loading

```go
users, err := orm.New[User](db).Query().
    With("Profile").
    With("Posts.Comments").                                   // nested
    With("Roles", func(q *gorm.DB) *gorm.DB {                  // constrained: applies to the last relation
        return q.Where("name <> ?", "banned")
    }).
    WithCount("Posts", "Roles").                               // user.Extra("posts_count")
    Get(ctx)

users[0].Posts.Items()          // []Post, empty until loaded
users[0].Profile.Value()        // *Profile or nil
users[0].Posts.Loaded()
```

Eager loading is one query per relation (two for pivot and through), never one per row. Loading the same relation twice in one query (`With("Posts.Comments").With("Posts.Tags")`) loads `Posts` once. `orm.Load(ctx, db, &user, "Roles")` loads onto a model you already have.

Lazy loading loads on first use and caches:

```go
posts, err := user.Posts.Get(ctx)
q, err := user.Posts.Query()                                  // a Query[Post] already limited to this user
titles, err := orm.Pluck[Post, string](ctx, q.Where("published = ?", true), "title")
target, err := comment.Commentable.Get(ctx)                    // any: *Post or *Video
```

Rows loaded through a many-to-many relation carry the pivot columns in `Extras()` as `pivot_<column>` (list the extra ones in `pivotColumns`).

## Existence queries

```go
users.Query().Has("Posts")                          // at least one post
users.Query().DoesntHave("Posts")
users.Query().WhereHas("Posts", func(q *gorm.DB) *gorm.DB { return q.Where("published = ?", true) })
users.Query().WhereDoesntHave("Roles", scope)
```

They compile to a subquery. `BelongsTo`, `HasOne`/`HasMany`, `BelongsToMany`, `HasManyThrough` and the morph relations are supported; `MorphTo` is not (the target table is not known). `WithCount` supports everything except `BelongsTo`, `HasManyThrough` and `MorphTo`.

## Writing through a relation

```go
post, err := user.Posts.Create(ctx, map[string]any{"title": "Hi"})   // sets user_id, mass-assignment rules apply
err = user.Posts.Save(ctx, &post)                                      // sets user_id, then saves
posts, err := user.Posts.CreateMany(ctx, []map[string]any{...})        // atomic
comment, err := post.Comments.Create(ctx, attrs)                       // also sets commentable_type / _id

err = comment.User.Associate(&author)   // sets the foreign key (save the comment yourself)
err = comment.User.Dissociate()         // clears it (the column must be nullable)

err = user.Roles.Attach(ctx, 1, role)                     // keys or models; skips links that exist
err = user.Roles.AttachWith(ctx, map[string]any{"assigned_by": "root"}, 3)
res, err := user.Roles.Sync(ctx, 1, 2)                    // SyncResult{Attached, Detached}
res, err = user.Roles.SyncWithoutDetaching(ctx, 4)
res, err = user.Roles.Toggle(ctx, 1, 5)
err = user.Roles.UpdateExistingPivot(ctx, 1, map[string]any{"assigned_by": "ana"})
err = user.Roles.Detach(ctx, 1)                           // no keys: detach everything
```

`MorphToMany` has the same pivot methods, and every one of them keeps `taggable_type` in the `WHERE`, so a post's `Sync` never touches a video's links. Pivot writes mark the relation as not loaded.

## Batch loaders

Before relation fields existed, migro offered key-based loaders: `orm.HasManyThroughLoader`, `orm.MorphToManyLoader`, `orm.MorphedByManyLoader` and `orm.MorphRegistry`. They still work, return maps keyed by the parent key you pass, and need no struct fields; use them for models you cannot change.

```go
rel := orm.HasManyThroughLoader[Story, Citizen]{ForeignKey: "country_id", ThroughForeignKey: "citizen_id"}
byCountry, err := rel.Load(ctx, db, []any{1, 2, 3})        // map[any][]Story
```

Very large key lists are not chunked; split them if your database caps the size of an `IN` list. Composite keys are not supported in relations.
