---
title: Transactions
nav_order: 10
description: Run any layer inside a transaction.
---

# Transactions

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

You may use the `Transaction` method to run a set of operations within a database transaction. If an error is returned from the closure the transaction is rolled back; if it returns `nil` the transaction is committed.

## Running a transaction

```go
err := db.Transaction(ctx, func(tx *migro.DB) error {
    if err := orm.New[User](tx).Create(ctx, &User{Email: "a@x.io"}); err != nil {
        return err // rolls back
    }
    return query.Table(tx, "audit").Insert(ctx, query.Row{"event": "user_created"})
})
```

### Using every layer inside a transaction

`tx` works with every layer (schema, query, orm, migrate, seed, relations). Returning an error rolls back; returning nil commits.
