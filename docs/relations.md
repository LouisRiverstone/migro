---
title: Relations
nav_order: 9
description: "One to one, one to many, belongs to, one of many, has one/many through, many to many with pivots, polymorphic relations; querying, aggregating, eager loading, writing and touching."
---

# Relations

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

Database tables are often related: a blog post has many comments, an order belongs to a user. migro's relations are **typed fields on the model**, the Go counterpart of Eloquent's relationship methods:

```go
type User struct {
    orm.BaseModel
    Name  string
    Posts orm.HasMany[Post]
}
```

Relation fields hide themselves from GORM, so they never become columns and never reach an `INSERT`. Names follow Laravel's conventions (`user_id`, `role_user`, `commentable_type`), and the `orm:"..."` tag overrides any of them ([Customizing keys](#customizing-keys)).

| Field type | Eloquent |
|---|---|
| `HasOne[T]` | `hasOne` |
| `HasMany[T]` | `hasMany` |
| `BelongsTo[T]` | `belongsTo` |
| `HasOneThrough[T, Through]` | `hasOneThrough` |
| `HasManyThrough[T, Through]` | `hasManyThrough` |
| `BelongsToMany[T]` | `belongsToMany` |
| `MorphOne[T]` | `morphOne` |
| `MorphMany[T]` | `morphMany` |
| `MorphTo` | `morphTo` |
| `MorphToMany[T]` | `morphToMany` |
| `MorphedByMany[T]` | `morphedByMany` |

A relation works on a [bound model](models.md#bound-models): one that came from `Find`, `Get`, `First`, a relation or a factory. On a struct you built by hand, relation methods return `UNBOUND_MODEL` until you call `orm.Bind(db, &m)`.

## Defining relationships

Each relation field offers the same reading methods:

| Method | Does |
|---|---|
| `Get(ctx)` | loads the relation the first time, then returns the cached result |
| `Load(ctx)` | loads it again |
| `Items()` (many) / `Item()` (one) | reads the cache, without a query |
| `Loaded()` | whether it has been loaded |
| `Query()` | a [query](query-builder.md) already limited to this owner, to add constraints |

Eloquent reads a loaded relation as a property (`$user->posts`) and builds a query with the method (`$user->posts()`). In migro, `Get` and `Items` are the property, `Query` is the method.

### One to one / has one

A `User` has one `Phone`. Put a `HasOne` on the owner; the foreign key lives on the related table:

```go
type User struct {
    orm.BaseModel
    Name  string
    Phone orm.HasOne[Phone]
}

type Phone struct {
    orm.BaseModel
    UserID uint // phones.user_id
    Number string
}

phone, err := user.Phone.Get(ctx) // *Phone, nil when there is no row
```

The foreign key is inferred from the owner's name: `User` gives `user_id`, which is assumed to hold the owner's `id`. Override them with `foreignKey` and `localKey`:

```go
Phone orm.HasOne[Phone] `orm:"foreignKey:owner_id;localKey:uuid"`
```

#### Defining the inverse of the relationship

Add a `BelongsTo` on the related model:

```go
type Phone struct {
    orm.BaseModel
    UserID uint
    User   orm.BelongsTo[User]
}

owner, err := phone.User.Get(ctx)
```

### One to many / has many

A `Post` has many `Comment`s:

```go
type Post struct {
    orm.BaseModel
    Title    string
    Comments orm.HasMany[Comment]
}

type Comment struct {
    orm.BaseModel
    PostID uint // comments.post_id
    Body   string
}

comments, err := post.Comments.Get(ctx) // orm.Collection[Comment]
for _, c := range comments { ... }
```

The foreign key defaults to `<owner snake name>_id`. Because the relation is also a query, constraints can be added:

```go
q, err := post.Comments.Query()
comment, err := q.Where("title = ?", "foo").First(ctx)
```

#### Automatically hydrating parent models on children

Eloquent's `chaperone()` sets the parent on each child, so `$comment->post` inside a loop over `$post->comments` costs no query. In migro, the parent is the value you are looping from: use it directly instead of loading `comment.Post`.

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

For `BelongsTo` the foreign key comes from the **field name**, not the related type: `Post` gives `post_id`. A field called `Author` looks for `author_id`, so point it elsewhere explicitly; `relatedKey` sets the column of the parent it refers to (default: its primary key):

```go
Author orm.BelongsTo[User] `orm:"foreignKey:user_id;relatedKey:id"`
```

#### Default models

Tag a single relation `withDefault` and a missing row answers with an empty, unsaved model instead of `nil` (Eloquent's `withDefault`, the Null Object pattern):

```go
Author orm.BelongsTo[User] `orm:"foreignKey:author_id;withDefault"`

post.Author.Item().Name     // "" rather than a nil dereference
post.Author.Item().Exists() // false
```

The default appears once the relation is loaded, and is left out of `ToMap` and JSON. It works on `BelongsTo`, `HasOne`, `HasOneThrough` and `MorphOne`. Laravel's `withDefault(['name' => 'Guest'])`, with attributes, is a check you write: `if !a.Exists() { a.Name = "Guest" }`.

#### Querying belongs to relationships

`WhereBelongsTo` finds the children of a model without spelling the foreign key:

```go
posts, err := orm.New[Post](db).Query().WhereBelongsTo(user).Get(ctx)    // posts.user_id = user.id
posts, err = orm.New[Post](db).Query().WhereBelongsTo(user, "Author").Get(ctx) // when there are several
```

It finds the belongs-to relation that points at the model you pass, and fails if there is none, or more than one and you did not name it.

### Has one of many

A `HasOne` or `MorphOne` can pick a single row out of a larger set with `ofMany` (Eloquent's `latestOfMany`, `oldestOfMany` and `ofMany`):

```go
type User struct {
    orm.BaseModel
    Orders        orm.HasMany[Order]
    LatestOrder   orm.HasOne[Order] `orm:"foreignKey:user_id;ofMany:latest"`
    OldestOrder   orm.HasOne[Order] `orm:"foreignKey:user_id;ofMany:oldest"`
    LargestOrder  orm.HasOne[Order] `orm:"foreignKey:user_id;ofMany:max:price"`
    CheapestOrder orm.HasOne[Order] `orm:"foreignKey:user_id;ofMany:min:price"`
}
```

`latest` and `oldest` compare the primary key, or the column you add (`ofMany:latest:created_at`), which must be sortable. `max:col` and `min:col` compare that column. Eager loading stays one query for every owner. Ties pick an arbitrary row.

{: .note }
PostgreSQL has no `MAX` for `uuid` columns, so `ofMany:latest` on a model with a UUID key needs a column to compare (`ofMany:latest:created_at`).

Eloquent's advanced form, several columns plus a constraint (`ofMany(['published_at' => 'max', 'id' => 'max'], fn ...)`), has no tag. Use a relation query: `q, _ := product.Prices.Query(); q.Where("published_at <= ?", now).Latest("published_at").First(ctx)`.

### Has one through

A `Mechanic` reaches the `Owner` of the car they repair, through `Car`:

```go
type Mechanic struct {
    orm.BaseModel
    Car   orm.HasOne[Car]
    Owner orm.HasOneThrough[Owner, Car]
}

owner, err := mechanic.Owner.Get(ctx)
```

Tables: `mechanics`, `cars` (`mechanic_id`), `owners` (`car_id`). It serializes as an object, or `null`.

#### Key conventions

The keys are the same as for [has many through](#key-conventions-1).

### Has many through

An `Application` has many `Deployment`s through its `Environment`s:

```go
type Application struct {
    orm.BaseModel
    Environments orm.HasMany[Environment]
    Deployments  orm.HasManyThrough[Deployment, Environment]
}

deployments, err := app.Deployments.Get(ctx)
```

Tables: `applications`, `environments` (`application_id`), `deployments` (`environment_id`). Through relations are read-only: there is no `Create` or `Attach` on them.

#### Key conventions

| Tag key | Meaning | Default |
|---|---|---|
| `foreignKey` | the column on the through table that points at the owner | `application_id` |
| `throughForeignKey` | the column on the target table that points at the through model | `environment_id` |
| `localKey` | the owner's key | `id` |
| `throughKey` | the through model's key | `id` |

```go
Deployments orm.HasManyThrough[Deployment, Environment] `orm:"foreignKey:app_id;throughForeignKey:env_id"`
```

## Scoped relationships

A relation narrowed by a condition is a second field over the same tables, or a function over the relation's query:

```go
func FeaturedPosts(ctx context.Context, u *User) (orm.Collection[Post], error) {
    q, err := u.Posts.Query()
    if err != nil {
        return nil, err
    }
    return q.Where("featured = ?", true).Get(ctx)
}
```

To create through it with the condition already set (Eloquent's `withAttributes`), pass the attribute: `user.Posts.Create(ctx, map[string]any{"title": "x", "featured": true})`. For many-to-many relations, `pivotWhere` scopes the pivot and also fills it on `Attach` ([Filtering queries via pivot columns](#filtering-queries-via-intermediate-table-columns)).

## Many to many relationships

A `User` has many `Role`s and a `Role` has many `User`s.

#### Table structure

Three tables: `users`, `roles`, and the pivot `role_user`, named after the two models in alphabetical order, with `user_id` and `role_id` columns.

#### Model structure

```go
type User struct {
    orm.BaseModel
    Roles orm.BelongsToMany[Role]
}

roles, err := user.Roles.Get(ctx)
q, err := user.Roles.Query()
sorted, err := q.OrderBy("name", "asc").Get(ctx)
```

Override the pivot table and its columns on the tag:

```go
Roles orm.BelongsToMany[Role] `orm:"pivotTable:user_roles;pivotForeignKey:member_id;pivotRelatedForeignKey:role_id"`
```

#### Defining the inverse of the relationship

The inverse is another `BelongsToMany`, on the other model:

```go
type Role struct {
    orm.BaseModel
    Name  string
    Users orm.BelongsToMany[User]
}
```

### Retrieving intermediate table columns

Only the two keys are read from the pivot by default. List other columns with `pivotColumns`; they appear in each model's `Extras()` as `pivot_<column>`:

```go
Roles orm.BelongsToMany[Role] `orm:"pivotColumns:active,created_by;pivotTimestamps:true"`

roles, _ := user.Roles.Get(ctx)
roles[0].Extra("pivot_active")
roles[0].Extra("pivot_created_at")
```

`pivotTimestamps:true` maintains `created_at` and `updated_at` on the pivot rows migro writes, and loads them. The pivot table needs both columns.

#### Customizing the pivot attribute name

`pivotAs` changes the prefix (Eloquent's `as('subscription')`):

```go
Podcasts orm.BelongsToMany[Podcast] `orm:"pivotAs:subscription;pivotTimestamps:true"`

podcasts[0].Extra("subscription_created_at")
```

### Filtering queries via intermediate table columns

`pivotWhere` limits the relation to pivot rows with given values (Eloquent's `wherePivot` and `withPivotValue`). Every read honours it (loading, counts, `Has`, the relation query), and `Attach` stores the values:

```go
Approved orm.BelongsToMany[Role] `orm:"pivotTable:role_user;pivotColumns:status;pivotWhere:status=approved"`
```

`Sync` and `Detach` only touch pivot rows that match, so another relation over the same pivot table keeps its rows. For a one-off filter (`wherePivotIn`, `wherePivotNull`, `wherePivotBetween`), add a condition on the pivot table to the relation query:

```go
q, _ := user.Roles.Query()
active, err := q.Where("role_user.expires_at IS NULL").Get(ctx)
```

### Ordering queries via intermediate table columns

```go
q, _ := user.Badges.Query()
badges, err := q.Where("rank = ?", "gold").Scope(func(g *gorm.DB) *gorm.DB {
    return g.Order("badge_user.created_at DESC") // orderByPivotDesc
}).Get(ctx)
```

### Defining custom intermediate table models

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

`Attach`, `AttachWith`, `Sync` and `Toggle` then create each row through that struct, so its `BeforeCreate` hooks run, a UUID key is generated and column defaults apply. The struct needs a column for both pivot keys and for every attribute you pass. Reads and `Detach` are unchanged, and a pivot model cannot be combined with `pivotWhere`.

## Polymorphic relationships

A polymorphic relation lets a child belong to more than one kind of parent through a single association: a `Comment` can belong to a `Post` or a `Video`. The child stores two columns, `<name>_type` and `<name>_id`, and you name the association with `orm:"morph:<name>"`. A missing `morph:` name fails with `INVALID_SCHEMA`, naming the field.

### One to one (polymorphic)

#### Table structure

`posts`, `users`, and `images` with `imageable_id` and `imageable_type`.

#### Model structure

```go
type Image struct {
    orm.BaseModel
    URL           string
    ImageableID   uint
    ImageableType string
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

#### Retrieving the relationship

```go
image, err := post.Image.Get(ctx)

parent, err := image.Imageable.Get(ctx) // *Post or *User, nil when the parent is gone
switch p := parent.(type) {
case *Post:
    fmt.Println(p.Title)
case *User:
    fmt.Println(p.Name)
}
```

#### Key conventions

`MorphTo`'s name defaults to the field name, so `Imageable` needs no tag; the columns are `<name>_type` and `<name>_id`. `MorphTo` resolves only aliases in the [morph map](#custom-polymorphic-types); an unknown one returns `UNKNOWN_MORPH_TYPE`.

### One to many (polymorphic)

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

comments, err := post.Comments.Get(ctx)
parent, err := comment.Commentable.Get(ctx)
```

Every query on `post.Comments` adds `commentable_type = 'post'` next to `commentable_id`, so a post never sees a video's comments.

### One of many (polymorphic)

`ofMany` works on `MorphOne` too:

```go
LatestImage orm.MorphOne[Image] `orm:"morph:imageable;ofMany:latest"`
BestImage   orm.MorphOne[Image] `orm:"morph:imageable;ofMany:max:likes"`
```

### Many to many (polymorphic)

#### Table structure

`posts`, `videos`, `tags`, and the pivot `taggables` with `tag_id`, `taggable_id` and `taggable_type`.

#### Model structure

```go
type Post struct {
    orm.BaseModel
    Tags orm.MorphToMany[Tag] `orm:"morph:taggable"`
}

type Video struct {
    orm.BaseModel
    Tags orm.MorphToMany[Tag] `orm:"morph:taggable"`
}
```

#### Defining the inverse of the relationship

One `MorphedByMany` per kind of owner:

```go
type Tag struct {
    orm.BaseModel
    Name   string
    Posts  orm.MorphedByMany[Post]  `orm:"morph:taggable"`
    Videos orm.MorphedByMany[Video] `orm:"morph:taggable"`
}
```

#### Retrieving the relationship

```go
tags, err := post.Tags.Get(ctx)
posts, err := tag.Posts.Get(ctx)
videos, err := tag.Videos.Get(ctx)
```

The pivot defaults to `<name>s` (`taggables`). Pivot writes keep `taggable_type` in the `WHERE`, so a post's `Sync` never touches a video's links.

### Custom polymorphic types

The `*_type` column stores an alias, not a Go type name. Register the aliases once, at startup (Laravel's `Relation::morphMap`):

```go
orm.MorphMap(map[string]any{"post": Post{}, "video": Video{}})
```

Types missing from the map use their snake_case name (`Post` is `post`). `orm.EnforceMorphMap` also refuses models missing from the map, with `UNKNOWN_MORPH_TYPE` (Laravel's `enforceMorphMap`):

```go
orm.EnforceMorphMap(map[string]any{"post": Post{}, "video": Video{}})
```

{: .warning }
Aliases decouple your data from your type names. Adding a map to an application that already has rows means converting the existing `*_type` values to the new aliases.

## Dynamic relationships

Laravel's `resolveRelationUsing` defines a relation at run time. In Go, relations are struct fields, known at compile time. For a model you cannot change, use the [batch loaders](#batch-loaders), which need no field.

## Querying relations

The query a relation hands out is a normal [query](query-builder.md), limited to the owner, so you can keep chaining:

```go
q, err := user.Posts.Query()
active, err := q.Where("active = ?", true).Get(ctx)
```

#### Chaining or where clauses after relationships

The owner's condition is part of the query, so an `OrWhere` can escape it:

```go
q.Where("active = ?", 1).OrWhere("votes >= ?", 100)
// WHERE user_id = ? AND active = 1 OR votes >= 100   -- any user's popular posts
```

Group the `OR` in parentheses to keep it inside:

```go
q.Where("(active = ? OR votes >= ?)", 1, 100)
// WHERE user_id = ? AND (active = 1 OR votes >= 100)
```

### Relationship methods vs. lazy loading

`Get` loads the relation the first time and caches it; `Items` and `Item` read the cache. Loading a relation inside a loop over many models runs one query per model, the N+1 problem. Use [eager loading](#eager-loading) instead, and turn on [strict mode](strict-mode.md) in development so a lazy load fails loudly.

{: .note }
Go copies structs by value, and a copy is detached from its database binding. Keep pointers (`&rows[0]`, not `row := rows[0]`) when you rely on lazy relations.

### Querying relationship existence

Select models by whether a relation has rows. These compile to an `EXISTS` subquery:

```go
users := orm.New[User](db).Query()

users.Has("Posts")              // at least one post

users.WhereHas("Posts", func(q *gorm.DB) *gorm.DB {
    return q.Where("content LIKE ?", "code%")
})
users.WhereRelation("Posts", "published = ?", true) // inline: WhereHas with one condition
```

Laravel's nested `has('posts.comments')` is a `WhereHas` whose condition is an `EXISTS` on the inner table. Laravel's `has('comments', '>=', 3)`, with a count, is a subquery in a `Where`: `Where("(SELECT COUNT(*) FROM posts WHERE posts.user_id = users.id) >= ?", 3)`.

Existence queries work on `BelongsTo`, `HasOne`, `HasMany`, `BelongsToMany`, `HasManyThrough` and the morph relations, but not on `MorphTo`, whose target table is not known. The relations must be in the same database.

### Querying relationship absence

```go
users.DoesntHave("Posts")
users.WhereDoesntHave("Posts", func(q *gorm.DB) *gorm.DB { return q.Where("banned = ?", true) })
```

### Querying morph to relationships

`WhereHasMorph` filters by a `MorphTo` relation, for the types you name:

```go
comments := orm.New[Comment](db).Query().WhereHasMorph("Commentable", []string{"post", "video"},
    func(q *gorm.DB) *gorm.DB { return q.Where("title LIKE ?", "code%") })
```

The types are morph aliases. Laravel's `whereMorphedTo($post)` is a plain condition: `Where("commentable_type = ? AND commentable_id = ?", "post", post.ID)`.

## Aggregating related models

### Counting related models

`WithCount` adds the number of related rows without loading them. The value lands in `Extras()` as `<relation>_count`:

```go
posts, err := orm.New[Post](db).Query().WithCount("Comments", "Tags").Get(ctx)
posts[0].Extra("comments_count") // 12
```

#### Deferred count loading

For a model you already hold:

```go
err := orm.LoadCount(ctx, db, post, "Comments", "Tags")
```

#### Relationship counting and custom select statements

Combine `WithCount` with `Select` freely; the count is a separate query, so it cannot be lost by your select list.

`WithCount` works on every relation except `BelongsTo`, `HasManyThrough` and `MorphTo`. Laravel's constrained counts (`withCount(['comments' => fn ...])`) and aliases (`comments as pending_count`) have no form; count through the relation's query instead: `q, _ := post.Comments.Query(); n, _ := q.Where("approved = ?", false).Count(ctx)`.

### Other aggregate functions

```go
users, err := orm.New[User](db).Query().
    WithSum("Orders", "total").
    WithAvg("Orders", "total").
    WithMin("Orders", "total").
    WithMax("Orders", "total").
    WithExists("Posts").
    Get(ctx)

u := users[0]
u.Extra("orders_sum_total") // float64, 0 when there are no orders
u.Extra("orders_avg_total") // float64, nil when there are no orders
u.Extra("posts_exists")     // bool
```

The names follow Laravel's `<relation>_<function>_<column>`. Deferred versions load onto a model you hold: `orm.LoadSum`, `LoadAvg`, `LoadMin`, `LoadMax`, `LoadExists`.

Aggregates run one extra query each. They work on has-one, has-many, morph-one, morph-many and the many-to-many kinds (the column is read from the related table, through the pivot). Column names are validated.

### Counting related models on morph to relationships

`MorphTo` has no counts, since its target depends on each row. Load the parents, group them by type, and count on each group with `Collection.Load` or `orm.LoadCount`.

## Eager loading

Reading a relation in a loop runs one query per model:

```go
books, _ := orm.New[Book](db).All(ctx)
for i := range books {
    author, _ := books[i].Author.Get(ctx) // 1 query for the books + 1 per book
}
```

`With` loads the relation for every row at once: one query for the books and one for all their authors:

```go
books, err := orm.New[Book](db).Query().With("Author").Get(ctx)
for _, b := range books {
    fmt.Println(b.Author.Item().Name) // no query
}
```

#### Eager loading multiple relationships

```go
books, err := orm.New[Book](db).Query().With("Author").With("Publisher").Get(ctx)
```

#### Nested eager loading

Dot notation loads relations of relations:

```go
books, err := orm.New[Book](db).Query().With("Author.Contacts").Get(ctx)
```

Loading the same relation twice (`With("Author.Contacts").With("Author.Publisher")`) loads `Author` once.

#### Nested eager loading morph to relationships

A `MorphTo` is loaded with one query per type found. Nested relations of a morph target load like any other: `With("Parentable")`, then `Collection.Load` on the targets you group by type.

#### Eager loading specific columns

Pass a constraint that selects the columns, and keep the keys the relation needs:

```go
books, err := orm.New[Book](db).Query().With("Author", func(q *gorm.DB) *gorm.DB {
    return q.Select("id", "name")
}).Get(ctx)
```

#### Eager loading by default

Eloquent's `$with` has no counterpart: write a function that returns your usual query, `func Books() *orm.Query[Book] { return orm.New[Book](db).Query().With("Author") }`.

### Constraining eager loads

A function after the relation name constrains what is loaded:

```go
users, err := orm.New[User](db).Query().With("Posts", func(q *gorm.DB) *gorm.DB {
    return q.Where("title LIKE ?", "%code%").Order("created_at DESC")
}).Get(ctx)
```

With dot notation, the constraint applies to the last relation of the path.

#### Constraining eager loads with relationship existence

To load only the matching children and keep only the parents that have one (Eloquent's `withWhereHas`), use the same condition twice:

```go
cond := func(q *gorm.DB) *gorm.DB { return q.Where("featured = ?", true) }
users, err := orm.New[User](db).Query().WhereHas("Posts", cond).With("Posts", cond).Get(ctx)
```

### Lazy eager loading

To load relations onto models you already have (Eloquent's `load`):

```go
err := orm.Load(ctx, db, book, "Author", "Publisher.Address")

books, _ := orm.New[Book](db).All(ctx)
err = books.Load(ctx, "Author")         // one query for every book
err = books.LoadMissing(ctx, "Author")  // only where it is not loaded yet
```

### Automatic eager loading

Eloquent's `automaticallyEagerLoadRelationships` has no counterpart. Load relations explicitly with `With` or `Collection.Load`, and let strict mode tell you when you forgot.

### Preventing lazy loading

```go
db.ShouldBeStrict(true)

_, err := post.Author.Get(ctx) // LAZY_LOADING_VIOLATION when Author was not eager loaded
```

Log instead of failing, in production:

```go
db.SetStrict(migro.Strict{
    LazyLoading: true,
    OnLazyLoad:  func(model, relation string) { slog.Warn("lazy load", "model", model, "relation", relation) },
})
```

See [Strict mode and debugging](strict-mode.md).

## Inserting and updating related models

### The save method

`Save` sets the foreign key on a model and saves it; `SaveMany` does it for several, in one transaction:

```go
comment := &Comment{Body: "A new comment."}
err := post.Comments.Save(ctx, comment)

err = post.Comments.SaveMany(ctx, []Comment{ {Body: "One"}, {Body: "Two"} })
```

Polymorphic relations set the `*_type` column too. Saved children are not added to a relation already loaded in memory; call `post.Comments.Load(ctx)` to read them back.

#### Recursively saving models and relationships

`orm.Push` saves a model and every loaded relation, in one transaction (Eloquent's `push`):

```go
post.Comments.Items()[0].Body = "Edited"
err := orm.Push(ctx, post)
```

It saves loaded belongs-to parents first and copies their key onto the model, saves the model, then links and saves the loaded children.

### The create method

`Create` builds the related model from a map, through mass assignment, and saves it; `CreateMany` creates several in one transaction:

```go
comment, err := post.Comments.Create(ctx, map[string]any{"body": "A new comment."})

comments, err := post.Comments.CreateMany(ctx, []map[string]any{
    {"body": "A new comment."},
    {"body": "Another new comment."},
})
```

`HasOne` and `MorphOne` have `Create` and `Save`; `HasMany` and `MorphMany` add `CreateMany` and `SaveMany`. Mass assignment rules apply to `Create` ([Models](models.md#mass-assignment)). To create without events, put `events.WithoutEvents(ctx)` in the context.

### Belongs to relationships

`Associate` points the foreign key at a parent, `Dissociate` clears it. Neither saves the child, so call `Save` afterwards:

```go
err := user.Account.Associate(account)
err = user.Save(ctx)

err = user.Account.Dissociate() // the column must be nullable
err = user.Save(ctx)
```

### Many to many relationships

#### Attaching and detaching

```go
err := user.Roles.Attach(ctx, roleID)
err = user.Roles.Attach(ctx, 1, 2, role)                               // keys or models
err = user.Roles.AttachWith(ctx, map[string]any{"expires": expires}, roleID) // pivot columns

err = user.Roles.Detach(ctx, roleID)
err = user.Roles.Detach(ctx)                                           // every link
```

Links that already exist are skipped by `Attach`. `Detach` removes pivot rows only, never the roles.

#### Syncing associations

`Sync` makes the links exactly the list you give; the result tells you what changed:

```go
res, err := user.Roles.Sync(ctx, 1, 2, 3)
res.Attached, res.Detached

res, err = user.Roles.SyncWithoutDetaching(ctx, 1, 2, 3) // only adds
```

Laravel's per-key pivot values (`sync([1 => ['expires' => true]])`) and `syncWithPivotValues` are a `Sync`, then `UpdateExistingPivot` for the keys that need values.

#### Toggling associations

```go
res, err := user.Roles.Toggle(ctx, 1, 2, 3) // attach the missing, detach the present
```

`Sync` and `Toggle` run in a transaction, which covers Laravel's `syncOrFail` and `toggleOrFail`.

#### Updating a record on the intermediate table

```go
err := user.Roles.UpdateExistingPivot(ctx, roleID, map[string]any{"active": false})
```

`MorphToMany` has the same methods; `MorphedByMany` supports `Attach`, `Detach` and `Sync`. Pivot writes mark the relation as not loaded, so the next `Get` reads fresh data.

## Touching parent timestamps

When a child changes, the parent's `updated_at` can move with it. List the belongs-to relations in `Touches`:

```go
func (Comment) Touches() []string { return []string{"Post"} }
```

The parent is touched whenever the child is created, saved, deleted or restored through the repository or the instance methods. Mass updates through a query do not touch, as in Eloquent. The touch cascades: when the touched parent has its own `Touches`, its parent moves too (comment, post, blog). Each parent is touched once per save, so a cycle ends.

## Customizing keys

Every default can be overridden on the field's `orm:"..."` tag. Separate keys with `;`.

| Key | Applies to | Meaning |
|---|---|---|
| `foreignKey` | has one/many, belongs to, through | the foreign key column |
| `localKey` | has one/many, morph one/many, many to many, through | the owner's key the foreign key points at (default: its primary key) |
| `relatedKey` | belongs to, many to many | the related model's key the foreign key points at |
| `pivotTable` | many to many | the pivot table |
| `pivotForeignKey` | many to many | the pivot column with the owner key |
| `pivotRelatedForeignKey` | many to many | the pivot column with the related key |
| `pivotColumns` | many to many | extra pivot columns to load, comma separated |
| `pivotTimestamps` | many to many | `true` to maintain `created_at` and `updated_at` on the pivot |
| `pivotWhere` | many to many | `col=value` pairs, comma separated, that scope the pivot |
| `pivotAs` | many to many | the prefix of pivot values in `Extras` (default `pivot`) |
| `morph` | polymorphic | the name of the `<name>_type` and `<name>_id` pair |
| `throughKey` | through | the through model's key referenced by the target |
| `throughForeignKey` | through | the column on the target pointing at the through model |
| `ofMany` | has one, morph one | `latest`, `oldest`, `max:col`, `min:col` |
| `withDefault` | belongs to, has one, has one through, morph one | an empty model instead of `nil` |

Defaults at a glance:

| Relation | Defaults |
|---|---|
| `HasOne`, `HasMany` | `<owner>_id` on the related table, owner key `id` |
| `BelongsTo` | `<field>_id` on the owner, related key `id` |
| `BelongsToMany` | pivot `<a>_<b>` in alphabetical order, `<owner>_id` and `<related>_id` |
| `HasOneThrough`, `HasManyThrough` | `<owner>_id` on the through table, `<through>_id` on the target |
| `MorphOne`, `MorphMany` | `<name>_type` and `<name>_id` on the related table |
| `MorphToMany`, `MorphedByMany` | pivot `<name>s`, `<name>_type`, `<name>_id` and `<related>_id` |

## Batch loaders

Before relation fields existed, migro offered key-based loaders: `orm.HasManyThroughLoader`, `orm.MorphToManyLoader`, `orm.MorphedByManyLoader` and `orm.MorphRegistry`. They still work, need no struct field, and return maps keyed by the parent keys you pass. Use them for models you cannot change:

```go
rel := orm.HasManyThroughLoader[Story, Citizen]{ForeignKey: "country_id", ThroughForeignKey: "citizen_id"}
byCountry, err := rel.Load(ctx, db, []any{1, 2, 3}) // map[any][]Story
```

Very large key lists are not chunked; split them if your database caps the size of an `IN` list. Composite keys are not supported in relations.
