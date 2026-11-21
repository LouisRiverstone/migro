---
title: Transactions
nav_order: 10
description: "Run any layer inside a transaction: commit and rollback, nesting, deadlock retries, manual transactions, models joining a transaction."
---

# Transactions

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

`db.Transaction` runs a set of operations in a database transaction (Laravel's `DB::transaction`). If the function returns an error or panics, the transaction is rolled back. If it returns nil, it is committed. You never call `Commit` or `Rollback` yourself.

```go
err := db.Transaction(ctx, func(tx *migro.DB) error {
    if err := orm.New[User](tx).Create(ctx, &User{Email: "a@x.io"}); err != nil {
        return err // rolls back
    }
    return query.Table(tx, "audit").Insert(ctx, query.Row{"event": "user_created"})
})
```

## Using every layer inside a transaction

`tx` is a `*migro.DB` like any other, so the schema builder, the query builder, models, relations, migrations and seeders all accept it. Pass `tx`, not `db`, to everything that should be part of the transaction: a call made with `db` inside the function runs on another connection, outside the transaction.

A transaction always runs on the primary, even when the connection has [read replicas](scaling.md#read-replicas).

### Nested transactions

Calling `tx.Transaction` inside a transaction opens a savepoint. An error from the inner function rolls back to the savepoint only, and the outer transaction can go on:

```go
err := db.Transaction(ctx, func(tx *migro.DB) error {
    if err := orm.New[Order](tx).Create(ctx, order); err != nil {
        return err
    }
    _ = tx.Transaction(ctx, func(tx2 *migro.DB) error {
        return sendToWarehouse(ctx, tx2, order) // a failure here keeps the order
    })
    return nil
})
```

### Handling deadlocks

Laravel's `DB::transaction($fn, attempts: 5)` retries a transaction that failed on a deadlock. In Go, retry it yourself, and only on the errors your database reports as retryable:

```go
func Retry(ctx context.Context, db *migro.DB, attempts int, fn func(tx *migro.DB) error) error {
    var err error
    for range attempts {
        if err = db.Transaction(ctx, fn); err == nil || !isDeadlock(err) {
            return err
        }
    }
    return err
}

// pgconn is github.com/jackc/pgx/v5/pgconn, mysql is github.com/go-sql-driver/mysql
func isDeadlock(err error) bool {
    var pg *pgconn.PgError
    if errors.As(err, &pg) {
        return pg.Code == "40P01" || pg.Code == "40001" // deadlock, serialization failure
    }
    var my *mysql.MySQLError
    return errors.As(err, &my) && (my.Number == 1213 || my.Number == 1205) // deadlock, lock wait timeout
}
```

The function runs again from the start, so keep it free of side effects outside the database.

## Manually using transactions

When the commit point is not the end of a function, use GORM's transaction directly and wrap it with `db.WithGorm`:

```go
gtx := db.Gorm.WithContext(ctx).Begin()
if gtx.Error != nil {
    return gtx.Error
}
tx := db.WithGorm(gtx)

if err := orm.New[User](tx).Create(ctx, user); err != nil {
    gtx.Rollback()
    return err
}
return gtx.Commit().Error
```

Prefer `db.Transaction` when you can: it cannot leak an open transaction.

## Moving a model into a transaction

A model loaded outside a transaction keeps the connection it came from. `UseTransaction` moves it into one (Lucid's `useTransaction`), so its own `Save`, `Delete` and relation calls run there:

```go
user, err := orm.New[User](db).Find(ctx, id)

err = db.Transaction(ctx, func(tx *migro.DB) error {
    user.UseTransaction(tx)
    user.Name = "Ana"
    return user.Save(ctx) // inside tx
})
```

## Locking rows

`LockForUpdate` and `SharedLock` lock the rows a query reads until the transaction ends (Laravel's pessimistic locking):

```go
err := db.Transaction(ctx, func(tx *migro.DB) error {
    account, err := orm.New[Account](tx).Query().Where("id = ?", id).LockForUpdate().First(ctx)
    if err != nil {
        return err
    }
    if account.Balance < amount {
        return ErrInsufficientFunds
    }
    account.Balance -= amount
    return account.Save(ctx)
})
```

See [Pessimistic locking](query-builder.md#pessimistic-locking) for what each database supports.

## Transactions and DDL

PostgreSQL, SQLite, SQL Server and Redshift roll back schema changes with the rest of a transaction. MySQL, MariaDB and Oracle commit DDL implicitly, so a `CREATE TABLE` inside a transaction commits everything before it. [Migrations](migrations.md) run each migration in a transaction where the database allows it.
