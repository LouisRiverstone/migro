---
title: Models
nav_order: 8
description: Eloquent and Lucid style models on top of GORM.
---

# Models

A model is a GORM struct that embeds `orm.BaseModel` (or `orm.SoftDeleteModel`). `orm.New[T](db)` is the repository, the equivalent of `User::` in Eloquent or `User.` in Lucid. Rows it returns are *bound* to the database, which turns on the instance API (`user.Save(ctx)`) and lazy relations.

```go
type User struct {
    orm.SoftDeleteModel            // ID, CreatedAt, UpdatedAt, DeletedAt
    Email string
    Name  string
    Password string

    Posts   orm.HasMany[Post]      `gorm:"-"`   // see Relations
    Profile orm.HasOne[Profile]    `gorm:"-"`
}

func (User) Fillable() []string { return []string{"email", "name"} }   // $fillable
func (User) Hidden() []string   { return []string{"password"} }        // $hidden

users := orm.New[User](db)

user, err := users.CreateFrom(ctx, map[string]any{"email": "ada@example.com", "name": "Ada"})
user, err = users.Find(ctx, 1)             // NOT_FOUND when missing   (FindOrFail is the same)
many, err := users.FindMany(ctx, 1, 2, 3)

user.Name = "Ada Lovelace"
user.IsDirty("name")                       // true
user.Dirty()                               // map[name:Ada Lovelace]
user.GetOriginal("name")                   // "Ada"
err = user.Save(ctx)                       // UPDATE of the changed columns only; no query when clean
err = user.Update(ctx, map[string]any{"name": "Ada L."})   // fill + save
err = user.Refresh(ctx)                    // reload in place
fresh, err := orm.Fresh(ctx, user)         // a new instance, user untouched
err = user.Increment(ctx, "visits", 1)
err = user.Delete(ctx)                     // soft delete when the model has DeletedAt
user.Trashed()                             // true
err = user.Restore(ctx)
err = user.ForceDelete(ctx)
```

`orm.BaseModel` has no soft deletes; `orm.SoftDeleteModel` adds them. Models whose key is composite (a pivot-like table) cannot embed `BaseModel`; they still work as the related side of a relation and with `orm.Bind`.

## Bound models

A model is bound when it comes from `Find`, `Get`, `First`, `Paginate`, `Create*`, a relation or a factory. A struct you build by hand is not: use `users.Make(attrs)`, create it through the repository, or call `orm.Bind(db, &u)`. Calling `Save` on an unbound model returns `UNBOUND_MODEL`.

Go copies structs by value, and a copy is detached from the row it came from. Keep pointers (`&rows[0]`, not `row := rows[0]`) when you rely on the instance API or lazy relations after loading.

## Mass assignment

`Fill`, `Update`, `Make` and `CreateFrom` take a `map[string]any` keyed by column or field name. Add `Fillable()` (an allow-list) or `Guarded()` (a deny-list) to the model; with neither, everything is allowed. A key outside the rules fails with `MASS_ASSIGNMENT`; nothing is dropped silently.

## Serialization

```go
m, err := orm.ToMap(user)      // map[string]any, column names as keys
js, err := orm.JSON(user)
```

`Hidden()` removes columns, `Visible()` keeps only the listed ones, and `Appends() map[string]any` adds computed values. Relations you loaded are nested under their snake_case name (`posts`, `profile`), and extras such as `posts_count` are included.

## Queries

```go
page, err := users.Query().
    Where("email LIKE ?", "%@example.com").
    Latest().                             // ORDER BY created_at DESC (Oldest, OrderByDesc, OrderBy)
    With("Posts.Comments").               // eager loading, see Relations
    WithCount("Posts").
    Paginate(ctx, 1, 20)                  // Data, Total, PerPage, CurrentPage, LastPage

first, err := users.Query().Where("name = ?", "Ada").First(ctx)           // FirstOrFail is the same
ok, err := users.Query().Where("email = ?", e).Exists(ctx)
n, err := users.Query().WhereIn("id", ids).Count(ctx)

names, err := orm.Pluck[User, string](ctx, users.Query(), "name")         // generic: a function, not a method
email, err := orm.Value[User, string](ctx, users.Query().Where("id = ?", 1), "email")
total, err := users.Query().Sum(ctx, "visits")                             // Avg, Min, Max too

err = users.Query().Chunk(ctx, 500, func(batch []User) error { return nil }) // keyset by primary key
n, err = users.Query().Where("id = ?", 1).Increment(ctx, "visits", 1)
n, err = users.Query().Where("name = ?", "Old").Update(ctx, map[string]any{"name": "New"})
n, err = users.Query().Where("id = ?", 1).Delete(ctx)
```

Also: `WhereNotIn`, `WhereNull`, `WhereNotNull`, `WhereBetween`, `Not`, `OrWhere`, `When(cond, fn)`, `Tap(fn)`, `Scope(fn...)`, `WithTrashed()` and `OnlyTrashed()` (which assumes the column is `deleted_at`).

## Writing many rows

```go
err := users.CreateMany(ctx, rows)
rows, err := users.UpdateOrCreateMany(ctx, []string{"email"}, rows)   // upsert by key columns, one transaction
rows, err = users.FirstOrCreateMany(ctx, []string{"email"}, rows)     // leaves existing rows alone
n, err := users.Destroy(ctx, 1, 2, 3)
err = users.Truncate(ctx)
```

`UpdateOrCreateMany` never rewrites the primary key, `created_at` or `deleted_at`, so a soft-deleted row is not resurrected.

Lifecycle hooks (`BeforeCreate`, `AfterSave`, …) use GORM's own mechanisms. For a global event bus see [Events](events.md).
