---
title: Schema diff
nav_order: 13
description: Generate migrations from the differences between models and the database.
---

# Schema diff

`migro migration:diff [name]` compares your GORM models with the live database and writes the difference as a new Go migration file. It treats every exported struct in `app/models` (`MIGRO_MODELS_DIR`) as a model; add `// migro:ignore` to a type's doc comment to skip it.

```
migro migration:diff add_profile_fields
migro migration:diff --rename=users.name=full_name     # a rename, not a drop + add
migro migration:diff cleanup --allow-destructive       # include DROP COLUMN / DROP INDEX
```

The file uses the schema builder (`t.String(...)`, `t.Index(...)`), so you can edit it before running `migration:run`. Behind it is `migrate/diff`, usable from code:

```go
plan, err := diff.Generate(ctx, db, diff.Options{
    Renames: map[string]string{"users.name": "full_name"}, // otherwise a rename is a drop + add
}, &User{}, &Post{})

for _, w := range plan.Warnings { log.Println("warning:", w) }
for _, op := range plan.Blocked { log.Printf("not applied (destructive): %s %s", op.Kind, op.Table) }

path, err := plan.WriteGoFile("database/migrations", "migrations", "sync_models", time.Now()) // a Go migration; "" when there is nothing to do
paths, err := plan.WriteFiles("migrations", "sync_models")                                     // or .up.sql / .down.sql files
```

What it detects: new tables, added/dropped/renamed columns, added/dropped indexes. Drops are listed in `plan.Blocked` and only written when `Options{AllowDestructive: true}`. It never drops tables.

What it does not do: detect column type or nullability changes (some are reported as warnings), or translate default expressions (reported as warnings). The `down` file for a dropped column is a comment, because the data cannot be restored. **Always read the generated SQL before applying it.**
