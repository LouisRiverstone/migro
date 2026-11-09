---
title: Transactions
nav_order: 10
description: Run any layer inside a transaction.
---

# Transactions

```go
err := db.Transaction(ctx, func(tx *migro.DB) error {
    if err := orm.New[User](tx).Create(ctx, &User{Email: "a@x.io"}); err != nil {
        return err // rolls back
    }
    return query.Table(tx, "audit").Insert(ctx, query.Row{"event": "user_created"})
})
```

`tx` works with every layer (schema, query, orm, migrate, seed, relations). Returning an error rolls back; returning nil commits.
