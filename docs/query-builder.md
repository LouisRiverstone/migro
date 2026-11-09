---
title: Query builder
nav_order: 7
description: Fluent queries for tables without a model.
---

# Query builder

For tables without a model. Builders are mutable: each method changes and returns the same `*Builder`.

```go
users := func() *query.Builder { return query.Table(db, "users") }

rows, err := users().Where("age > ?", 18).OrderBy("name", "asc").Limit(20).Get(ctx) // []map[string]any
row, err := users().Where("id = ?", 1).First(ctx)       // errors.Is(err, migro.ErrNotFound) if missing
n, err := users().WhereIn("id", []int{1, 2, 3}).Count(ctx)
err = users().Insert(ctx, query.Row{"name": "Ada"}, query.Row{"name": "Alan"})
affected, err := users().Where("id = ?", 1).Update(ctx, query.Row{"name": "Ada L"})
affected, err = users().Where("id = ?", 2).Delete(ctx)
page, err := users().OrderBy("id", "asc").Paginate(ctx, 1, 25) // Data, Total, PerPage, CurrentPage, LastPage
```

Also available: `Select`, `OrWhere`, `Not`, `WhereNull`, `WhereNotNull`, `WhereBetween`, `Join("JOIN posts ON …")`, `GroupBy`, `Having`, `Offset`, `Pluck`.

Values are always bound as parameters. `Update` and `Delete` without a `Where` return an error rather than touching every row. `OrderBy` only accepts `asc` or `desc`.
