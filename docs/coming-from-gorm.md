---
title: Coming from GORM
nav_order: 2.5
description: Adopt migro in an existing GORM project without rewriting anything.
---

# Coming from GORM

migro sits on top of GORM. Your models, `*gorm.DB` queries and hooks keep working, so you adopt it one model at a time.

## Step 1: open through migro

```go
db, err := migro.Open(migro.Config{Dialect: "postgres", DSN: dsn})
// db.Gorm is the *gorm.DB you already use
users := db.Gorm.Where("active = ?", true).Find(&list)   // unchanged
```

Nothing else changes. Migrations, seeders and the CLI work from this point.

## Step 2: move schema changes to migrations

Instead of `AutoMigrate`, describe the database in versioned files. If the database already exists, let migro write the first migration for you:

```
migro migration:diff baseline            # compares your models with the live schema
migro migration:run
```

Or go the other way and generate models from tables you already have:

```
migro make:model Post --from-table=posts
```

The generated struct embeds `orm.BaseModel` (or `orm.SoftDeleteModel`) when the table has `id`, `created_at`, `updated_at` (and `deleted_at`). Otherwise it lists every column. Review the Go types: `decimal` becomes `float64`, JSON becomes `json.RawMessage`.

## Step 3: opt a model in

Embed `orm.BaseModel` where you had `gorm.Model`, and query through `orm.New`:

| GORM | migro |
|---|---|
| `gorm.Model` | `orm.BaseModel` (`orm.SoftDeleteModel` for `DeletedAt`) |
| `db.First(&u, 1)` | `users.Find(ctx, 1)` |
| `db.Where("a = ?", 1).Find(&list)` | `users.Query().Where("a = ?", 1).Get(ctx)` |
| `db.Preload("Posts").Find(&list)` | `users.Query().With("Posts").Get(ctx)` |
| `db.Create(&u)` | `users.Create(ctx, &u)` |
| `db.Save(&u)` | `u.Save(ctx)`, which updates only changed columns |
| `db.Unscoped().Find(&list)` | `users.Query().WithTrashed().Get(ctx)` |
| `db.Transaction(func(tx *gorm.DB) error {...})` | `db.Transaction(ctx, func(tx *migro.DB) error {...})` |
| `db.Clauses(clause.OnConflict{...}).Create(&rows)` | `users.Upsert(ctx, rows, uniqueBy...)` |
| `db.Session(&gorm.Session{DryRun: true})` | `query.ToSQL(ctx)` |

## What changes in practice

- **Relations are fields.** Declare `Posts orm.HasMany[Post]` instead of a `[]Post` slice. Load them with `With`, or lazily with `u.Posts.Get(ctx)`.
- **Context is explicit.** Terminal methods take `ctx` as the first argument.
- **Errors carry codes.** Check `migro.CodeOf(err) == migro.NotFound` instead of `errors.Is(err, gorm.ErrRecordNotFound)`. See [Errors](errors.md).
- **Mass assignment is opt-in.** `CreateFrom` and `Fill` only accept columns listed in `Fillable()` when you define it.
- **Turn on strict mode** while you migrate: `db.ShouldBeStrict(true)` fails on lazy loading, which is how you find N+1 queries. See [Strict mode](strict-mode.md).

## Staying on GORM where it fits

You do not have to convert everything. A raw `db.Gorm.Raw(...)` or a hook-heavy model can stay as it is. Mixed code is the intended setup.
