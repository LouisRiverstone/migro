---
title: Serialization
nav_order: 9.3
description: Converting models and collections to maps and JSON — hidden attributes, appended values, dates, loaded relations.
---

# Serialization

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

When you build an API you convert models to maps and JSON. migro's serialization (Eloquent's `toArray`/`toJson`) lets you control which attributes appear, add computed ones, and format dates, without writing the conversion by hand. For a fully hand-shaped response use a [resource](resources.md).

{: .note }
Serialization works on **bound** models (rows that came from a query, a relation or a repository). On a struct you built by hand it returns `UNBOUND_MODEL`.

## Serializing models

```go
user, _ := orm.New[User](db).Find(ctx, 1)

m, err := orm.ToMap(user)      // map[string]any, column names as keys
js, err := orm.JSON(user)      // the same as JSON bytes
```

The output follows the **database column names** (`created_at`, `country_id`), not the Go field names — the shape your tables have, which is also what a front end usually expects.

What `ToMap` includes, in order:

1. Every column the model exposes (see below), with its [cast's `Get`](mutators-casting.md) applied.
2. Query extras such as `posts_count` from `WithCount` and `pivot_*` columns.
3. Values from `Appends()`.
4. Every **loaded** relation, nested under its snake_case name.

## Serializing collections

```go
users, _ := orm.New[User](db).Query().Get(ctx)

maps, err := users.ToMaps()    // []map[string]any
js, err := users.JSON()        // a JSON array
```

Both accept the same options as a single model (below), applied to every element. See [Collections](collections.md).

## Hiding attributes

Implement `Hidden()` to leave columns out, the model-level `$hidden`:

```go
func (User) Hidden() []string { return []string{"password", "api_token"} }
```

Or implement `Visible()` for an allow-list: only those columns are serialized.

```go
func (User) Visible() []string { return []string{"id", "name", "email"} }
```

`Visible` wins over `Hidden` when a model has both. Prefer `Hidden` for "everything except secrets" and `Visible` for "only these".

Hidden and visible lists only affect **columns**. Appended values, extras and relations are not subject to them (hide a relation by not loading it).

### Temporarily changing visibility

`MakeVisible` and `MakeHidden` adjust a **single call** (Eloquent's `makeVisible`/`makeHidden`); the model's own lists are untouched:

```go
orm.ToMap(user)                                        // no password
orm.ToMapWith(user, orm.MakeVisible("password"))       // password included, this call only
orm.ToMapWith(user, orm.MakeHidden("email", "name"))   // two more columns hidden
orm.JSONWith(user, orm.MakeVisible("api_token"))
```

- `MakeVisible` reveals a hidden column. On a model with `Visible()` it adds the column to the allow-list.
- `MakeHidden` hides a column. On a model with `Visible()` it removes it.
- If both name the same column, **hidden wins**.
- Options apply to the model you serialize, not to its nested relations (except `DateFormat`).

## Appending values

`Appends()` adds computed values that are not columns, the equivalent of `$appends` plus an accessor:

```go
func (u User) Appends() map[string]any {
    return map[string]any{
        "full_name": u.FirstName + " " + u.LastName,
        "is_admin":  u.Role == "admin",
    }
}
```

```json
{ "id": 1, "first_name": "Ada", "last_name": "Lovelace", "full_name": "Ada Lovelace", "is_admin": false }
```

Appended keys overwrite a column of the same name. The method runs on every serialization, so keep it cheap and free of queries.

## Formatting dates

By default times come out as `time.Time` in the map, which `encoding/json` renders as RFC 3339 (`"2026-02-01T12:00:00Z"`). `DateFormat` renders every time with a Go layout, including the ones in loaded relations:

```go
orm.ToMapWith(user, orm.DateFormat("2006-01-02"))
// {"created_at": "2026-02-01", "posts": [{"created_at": "2026-02-03", ...}]}
```

Pointer times (`*time.Time`, like a nullable `birthday`) are formatted too, and `nil` stays `nil`. To format **one** column, use [`CastDate`](mutators-casting.md#dates) instead; a cast runs before `DateFormat`, so a column with a cast keeps the cast's format.

## Relationships

Loaded relations are nested automatically, using the same rules recursively; unloaded ones are absent — serialization never runs a query:

```go
users, _ := orm.New[User](db).Query().With("Posts.Comments").WithCount("Roles").Get(ctx)
m, _ := orm.ToMap(&users[0])
```

```json
{
  "id": 1, "name": "Ana", "roles_count": 2,
  "posts": [
    { "id": 1, "title": "Hello DB", "comments": [ { "id": 1, "body": "nice" } ] }
  ]
}
```

- A single relation (`HasOne`, `BelongsTo`, `MorphOne`, `MorphTo`) is an object, or `null` when it is loaded but empty. A many relation is an array, `[]` when empty.
- Each nested model applies **its own** `Hidden`, `Visible`, `Appends` and casts.
- The key is the snake_case of the **field name** (`Comments` → `comments`, `OrderItems` → `order_items`).

## JSON columns and nested values

A [`JSONColumn[T]`](mutators-casting.md#typed-json-columns) serializes as its nested value, so `Settings` becomes `"settings": {"theme": "dark"}`. Values from `CastJSON` come out as decoded maps and slices.

## Serializing to your own types

`ToMap` returns `map[string]any`, so you can decode it into a struct with any decoder, or marshal it with your own JSON settings:

```go
m, _ := orm.ToMap(user)
enc := json.NewEncoder(w)
enc.SetIndent("", "  ")
enc.Encode(m)
```

## Choosing between serialization and resources

| Use | When |
|---|---|
| `ToMap` / `JSON` | Internal tools, quick endpoints, debugging — the table's shape is fine |
| A [resource](resources.md) | A public API whose shape you want to control, version and keep stable |

Serialization starts from "everything except what is hidden", so adding a column to the table adds it to the output. A resource starts from "nothing except what I list", so it cannot leak a new column by accident.

## Testing serialization

`integration/serialization_test.go` covers hidden columns, `MakeVisible`/`MakeHidden` being per call, hidden winning, `DateFormat` at the root and in relations, extras, and collection output as a JSON array.

## Renaming keys

`SerializeAs` renames columns and relations in `ToMap` and JSON (Lucid's `serializeAs`):

```go
func (User) SerializeAs() map[string]string {
    return map[string]string{"created_at": "createdAt", "posts": "articles"}
}
```
