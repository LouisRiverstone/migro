---
title: Query builder
nav_order: 7
description: "Fluent queries: retrieving rows, chunking and streaming, aggregates, selects, raw expressions, joins, unions, where clauses, ordering, grouping, inserts, updates, deletes, locks and debugging."
---

# Query builder

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

migro has two fluent query builders:

| Builder | Start it with | Rows come back as | Use it for |
|---|---|---|---|
| **Table builder** | `query.Table(db, "users")` | `query.Row` (`map[string]any`) | tables without a model, quick scripts, Laravel's `DB::table()` |
| **Model query** | `orm.New[User](db).Query()` | `*User`, `orm.Collection[User]` | everything else: relations, scopes, casts, events, pagination |

Both are built on GORM, bind every value as a parameter, and quote identifiers for your dialect. The model query has the full range of this page; the table builder covers the basics, and each section says which one to use. Both are **mutable**: every method changes and returns the same builder. To reuse a base query, wrap it in a function:

```go
users := func() *query.Builder { return query.Table(db, "users") }
active := func() *orm.Query[User] { return orm.New[User](db).Query().Where("active = ?", true) }
```

{: .warning }
Values are bound, but **column names are not**: the database cannot take an identifier as a parameter. Never let user input choose a column, including the column of `OrderBy`. Check it against a list of allowed names first.

A model query that cannot be built (a bad operator, a feature your database lacks) records the error and returns it from the terminal call (`Get`, `First`, `Count`...) as a `*migro.Error`.

## Running database queries

#### Retrieving all rows from a table

```go
rows, err := query.Table(db, "users").Get(ctx) // []query.Row
for _, row := range rows {
    fmt.Println(row["name"])
}

users, err := orm.New[User](db).All(ctx)       // orm.Collection[User]
users, err = orm.New[User](db).Query().Where("active = ?", true).Get(ctx)
```

#### Retrieving a single row or column from a table

```go
row, err := query.Table(db, "users").Where("name = ?", "John").First(ctx)
if errors.Is(err, migro.ErrNotFound) { ... }

user, err := orm.New[User](db).Query().Where("name = ?", "John").First(ctx)
user, err = orm.New[User](db).Query().FirstWhere(ctx, "email", "john@example.com")
user, err = orm.New[User](db).Find(ctx, 3)       // by primary key
user, err = orm.New[User](db).FindOrFail(ctx, 3) // same, named like Laravel's
```

`First` and `Find` return a `NOT_FOUND` error, matched by `errors.Is(err, migro.ErrNotFound)`, when nothing matches.

To read one column of the first matching row (`value`), or to require exactly one row (`sole`, `soleValue`):

```go
email, err := orm.Value[User, string](ctx, orm.New[User](db).Query().Where("name = ?", "John"), "email")

user, err := orm.New[User](db).Query().Where("email = ?", e).Sole(ctx)
// NOT_FOUND when nothing matches, MULTIPLE_RECORDS_FOUND when more than one row does
```

#### Retrieving a list of column values

```go
titles, err := query.Table(db, "posts").Pluck(ctx, "title")                         // []any
titles, err := orm.Pluck[Post, string](ctx, orm.New[Post](db).Query(), "title")    // []string
```

Laravel's `pluck('title', 'name')`, keyed by another column, is `orm.Collection.KeyBy` on the full rows (see [Collections](collections.md)).

### Chunking results

To work through thousands of rows without loading them all at once, take them in chunks:

```go
err := orm.New[User](db).Query().Chunk(ctx, 100, func(users []User) error {
    for _, u := range users {
        ...
    }
    return nil // return an error to stop
})
```

When the callback **updates** the rows it reads, use `ChunkByID`, which pages by key instead of by offset, so changed rows never shift the next chunk:

```go
err := orm.New[User](db).Query().Where("active = ?", false).ChunkByID(ctx, 100, "id", func(users []User) error {
    ids, err := orm.Collection[User](users).ModelKeys()
    if err != nil {
        return err
    }
    _, err = orm.New[User](db).Query().WhereIn("id", ids).Update(ctx, map[string]any{"active": true})
    return err
})
```

{: .note }
Changing the primary key of rows inside the callback can skip rows. When you add your own `OrWhere` conditions to a chunked query, wrap them in parentheses: `Where("(a = ? OR b = ?)", x, y)`, because the chunking adds its own conditions.

### Streaming results lazily

`Lazy` returns an iterator that fetches chunk by chunk and yields one model at a time, with eager-loaded relations. `Cursor` runs a single statement and yields rows as the driver reads them, which uses the least memory but cannot eager load. `Each` is the callback form of `Lazy`:

```go
for user, err := range orm.New[User](db).Query().With("Posts").Lazy(ctx, 500) {
    if err != nil {
        return err
    }
    ...
}

for user, err := range orm.New[User](db).Query().Cursor(ctx) { ... }

err := orm.New[User](db).Query().Each(ctx, 500, func(u *User) error { ... })
```

Breaking out of a `range` loop releases the statement.

### Aggregates

```go
n, err := query.Table(db, "users").Count(ctx)

q := orm.New[Order](db).Query()
n, err = q.Count(ctx)
total, err := q.Sum(ctx, "price")   // float64
avg, err := q.Avg(ctx, "price")     // float64
max, err := q.Max(ctx, "price")     // any: the column's own type
min, err := q.Min(ctx, "price")
```

Combine them with conditions:

```go
avg, err := orm.New[Order](db).Query().Where("finalized = ?", true).Avg(ctx, "price")
```

#### Determining if records exist

```go
ok, err := orm.New[Order](db).Query().Where("finalized = ?", true).Exists(ctx)
```

`Exists` counts with a `LIMIT 1`, so the intent reads better than `Count(ctx) > 0`.

## Select statements

#### Specifying a select clause

```go
rows, err := query.Table(db, "users").Select("name", "email AS user_email").Get(ctx)

users, err := orm.New[User](db).Query().Select("id", "name").Get(ctx)
users, err = orm.New[User](db).Query().Distinct("email").Get(ctx)
```

A model query that selects only some columns returns models with the others left at their zero value. Keep the primary key in the list if you are going to save or load relations.

## Raw expressions

Sometimes a query needs an SQL expression. Every builder method that takes a string fragment sends it as written, so the warning above applies to it in full: **the text is SQL**, only the `?` arguments are bound.

#### Raw where clauses

```go
orm.New[User](db).Query().WhereRaw("lower(name) = ?", "ana")
orm.New[User](db).Query().Where("price > IF(state = 'TX', ?, 100)", 200)
orm.New[User](db).Query().OrWhereRaw("votes > ?", 100)
```

`Where` itself accepts a fragment, so it doubles as Laravel's `whereRaw`.

#### Raw selects

```go
rows, err := query.Table(db, "orders").
    Select("department", "SUM(price) AS total_sales").
    GroupBy("department").
    Having("SUM(price) > ?", 2500).
    Get(ctx)
```

#### Raw grouping and having

```go
orm.New[Order](db).Query().Select("DATE(created_at) AS day", "COUNT(*) AS n").GroupByRaw("DATE(created_at)")
orm.New[Order](db).Query().GroupBy("customer_id").Having("SUM(price) > ?", 2500)
```

`GroupBy` quotes each name as an identifier; `GroupByRaw` sends an expression as written.

#### Raw ordering

`OrderBy` takes a column and a direction (`asc` or `desc`, anything else is an error). For an expression, drop to a GORM scope:

```go
orm.New[Post](db).Query().Scope(func(g *gorm.DB) *gorm.DB { return g.Order("updated_at - created_at DESC") })
```

## Joins

`Join` (table builder) and `Joins` (model query) take the join clause as written, with bound arguments:

#### Inner join clause

```go
rows, err := query.Table(db, "users").
    Select("users.*", "contacts.phone", "orders.price").
    Join("JOIN contacts ON contacts.user_id = users.id").
    Join("JOIN orders ON orders.user_id = users.id").
    Get(ctx)
```

#### Left join and right join clauses

```go
query.Table(db, "users").Join("LEFT JOIN posts ON posts.user_id = users.id")
query.Table(db, "users").Join("RIGHT JOIN posts ON posts.user_id = users.id")
```

#### Cross join clause

```go
query.Table(db, "sizes").Join("CROSS JOIN colors")
```

#### Advanced join clauses

Put the extra conditions in the clause, and bind the values:

```go
query.Table(db, "users").Join("JOIN contacts ON contacts.user_id = users.id AND contacts.user_id > ?", 5)
```

#### Subquery joins

```go
orm.New[User](db).Query().Joins(
    "JOIN (SELECT user_id, MAX(created_at) AS last_post_created_at FROM posts WHERE is_published = ? GROUP BY user_id) latest_posts ON latest_posts.user_id = users.id",
    true,
)
```

For relations, prefer [`With`, `WhereHas` and the aggregates](relations.md#querying-relations): they need no join at all.

#### Lateral joins

Write the `LATERAL` join in the clause, on the databases that support it (PostgreSQL, MySQL 8.0.14+); SQL Server's equivalent is `CROSS APPLY`.

## Unions

```go
admins := orm.New[User](db).Query().Where("role = ?", "admin")
staff := orm.New[User](db).Query().Where("role = ?", "staff")

rows, err := admins.Union(staff).OrderBy("name", "asc").Limit(20).Get(ctx)
rows, err = admins.UnionAll(staff).Get(ctx) // keeps duplicates
```

Each side keeps its own conditions and global scopes, and the select lists must line up. The result is a query over the combined rows, so `OrderBy`, `Limit`, `Count` and `Paginate` apply to the union.

## Basic where clauses

### Where clauses

`Where` takes an SQL fragment with `?` placeholders, a map, or a struct:

```go
q := orm.New[User](db).Query()

q.Where("votes = ?", 100).Where("age > ?", 35)
q.Where("votes >= ?", 100)
q.Where("votes <> ?", 100)
q.Where("name LIKE ?", "T%")
q.Where(map[string]any{"status": 1, "subscribed": true}) // status = 1 AND subscribed = true
q.Where(&User{Name: "John"})                             // non-zero fields only
```

Calls are joined with `AND`.

{: .note }
MySQL and MariaDB compare a string to a number by turning the string into a number, so `'abc' = 0` is true there. Convert values to the column's type before you bind them.

### Or where clauses

```go
q.Where("votes > ?", 100).OrWhere("name = ?", "John")
```

To group an `OR`, put it in parentheses, so it does not escape the rest of the query, nor a global scope:

```go
// WHERE votes > 100 OR (name = 'Abigail' AND votes > 50)
q.Where("votes > ?", 100).OrWhere("(name = ? AND votes > ?)", "Abigail", 50)
```

### Where not clauses

```go
q.Not("clearance = ? OR price < ?", true, 10)   // NOT (clearance = true OR price < 10)
q.Not(map[string]any{"status": "banned"})
```

### Where any, all and none clauses

Laravel's `whereAny(['name', 'email'], 'like', $v)` is a grouped `OR`:

```go
q.Where("(name LIKE ? OR email LIKE ? OR phone LIKE ?)", "Example%", "Example%", "Example%") // whereAny
q.Where("title LIKE ? AND content LIKE ?", "%Laravel%", "%Laravel%")                          // whereAll
q.Not("title LIKE ? OR content LIKE ?", "%PHP%", "%PHP%")                                     // whereNone
```

### JSON where clauses

`WhereJSON` compares a value at a path inside a JSON column, on every dialect:

```go
q.WhereJSON("settings", "address.city", "=", "Rio")
q.WhereJSON("options", "items.0.sku", "=", "A-100")
```

Path segments must be names or array indexes. Anything else fails with `QUERY_FAILED`, because the path is part of the SQL text. Laravel's `whereJsonContains` and `whereJsonLength` have no builder method; use your database's JSON functions in a `WhereRaw`.

### Additional where clauses

**WhereLike / WhereNotLike**

```go
q.WhereLike("email", "%@example.com")
q.WhereNotLike("email", "%@spam.io")
```

Case sensitivity follows the column's collation, as with a plain `LIKE`.

**WhereIn / WhereNotIn**

```go
q.WhereIn("id", []int{1, 2, 3})
q.WhereNotIn("id", []int{1, 2, 3})
query.Table(db, "users").WhereIn("id", []int{1, 2, 3})
```

For "where the id is in another table", a relation reads better: `WhereHas` (see [Relations](relations.md#querying-relationship-existence)), or a raw subquery, `q.Where("id IN (SELECT user_id FROM comments WHERE approved = ?)", true)`.

**WhereBetween**

```go
q.WhereBetween("votes", 1, 100)
q.Not("votes BETWEEN ? AND ?", 1, 100)            // whereNotBetween
q.Where("patients.weight BETWEEN minimum_allowed_weight AND maximum_allowed_weight") // whereBetweenColumns
```

**WhereNull / WhereNotNull**

```go
q.WhereNull("updated_at")
q.WhereNotNull("updated_at")
```

**WhereDate / WhereMonth / WhereDay / WhereYear**

```go
q.WhereDate("created_at", "=", "2026-01-31")
q.WhereMonth("created_at", "=", 12)
q.WhereDay("created_at", "=", 31)
q.WhereYear("created_at", ">=", 2026)
```

For `wherePast`, `whereToday` and the like, compare with the time yourself: `q.Where("due_at < ?", time.Now())`.

**WhereColumn**

```go
q.WhereColumn("updated_at", ">", "created_at")
q.WhereColumn("first_name", "=", "last_name")
```

Operators are checked against `= <> != < <= > >=`.

### Logical grouping

Parentheses in the fragment group conditions:

```go
// WHERE name = 'John' AND (votes > 100 OR title = 'Admin')
q.Where("name = ?", "John").Where("(votes > ? OR title = ?)", 100, "Admin")
```

## Advanced where clauses

### Where exists clauses

`WhereExists` takes another model query, typically correlated with `WhereColumn`:

```go
// users who have at least one order
users := orm.New[User](db).Query().WhereExists(
    orm.New[Order](db).Query().WhereColumn("orders.user_id", "=", "users.id"),
)
users.WhereNotExists(...)
```

### Subquery where clauses

```go
q.Where("(SELECT title FROM memberships WHERE memberships.user_id = users.id ORDER BY start_date DESC LIMIT 1) = ?", "Pro")
q.Where("amount < (SELECT AVG(amount) FROM incomes)")
```

### Full text where clauses

```go
articles.Query().WhereFullText("go orm", "title", "body")
articles.Query().WhereFullTextBoolean("+go -orm", "title", "body") // operator syntax
```

Create the index in a migration with `t.FullText("title", "body")`.

| Database | Search | Index created by `t.FullText` |
|---|---|---|
| MySQL, MariaDB | `MATCH ... AGAINST` | `FULLTEXT` index |
| PostgreSQL, CockroachDB | `to_tsvector('simple', ...) @@ plainto_tsquery` (`websearch_to_tsquery` in boolean mode) | GIN index on the same expression |
| SQLite, SQL Server, Oracle, Redshift | not supported | not supported |

On the unsupported databases, both the query and the index fail with `UNSUPPORTED_FEATURE`. SQLite needs an FTS virtual table and SQL Server a full-text catalog; neither can be described by a column list. PostgreSQL searches with the `simple` configuration (no stemming), and the index only helps a query that lists the same columns in the same order.

### Vector similarity clauses

migro has no vector type or similarity clauses. On PostgreSQL with pgvector, write the operator in a raw fragment: `q.Scope(func(g *gorm.DB) *gorm.DB { return g.Order(gorm.Expr("embedding <=> ?", vec)) })`.

## Ordering, grouping, limit and offset

### Ordering

```go
q.OrderBy("name", "desc").OrderBy("email", "asc")
q.OrderByDesc("created_at")
q.Latest()            // ORDER BY created_at DESC
q.Latest("published_at")
q.Oldest()            // ORDER BY created_at ASC
```

For random order, use a scope with your database's function: `g.Order("RANDOM()")` on PostgreSQL and SQLite, `g.Order("RAND()")` on MySQL. Laravel's `reorder()`, which removes earlier orderings, has no counterpart: build the query again without them.

### Grouping

```go
orm.New[User](db).Query().Select("account_id", "COUNT(*) AS n").GroupBy("account_id").Having("COUNT(*) > ?", 100)
query.Table(db, "users").GroupBy("first_name", "status").Having("account_id > ?", 100)
```

### Limit and offset

```go
q.Offset(10).Limit(5)
query.Table(db, "users").Offset(10).Limit(5)
```

For pages of results, use [pagination](pagination.md) instead.

## Conditional clauses

`When` applies part of a query only when a condition holds:

```go
role := r.URL.Query().Get("role")
users, err := orm.New[User](db).Query().
    When(role != "", func(q *orm.Query[User]) *orm.Query[User] { return q.Where("role = ?", role) }).
    When(sortByVotes, func(q *orm.Query[User]) *orm.Query[User] { return q.OrderByDesc("votes") }).
    Get(ctx)
```

For an "else" branch, write a second `When` with the opposite condition.

## Insert statements

```go
err := query.Table(db, "users").Insert(ctx,
    query.Row{"email": "picard@example.com", "votes": 0},
    query.Row{"email": "janeway@example.com", "votes": 0},
)

err = orm.New[User](db).Create(ctx, &User{Email: "kayla@example.com"})
err = orm.New[User](db).CreateMany(ctx, []User{ {Email: "a@x.io"}, {Email: "b@x.io"} })
```

`InsertOrIgnore` skips rows that would break a unique index (Laravel's `insertOrIgnore`):

```go
err := orm.New[User](db).InsertOrIgnore(ctx, []User{ {ID: 1, Email: "sisko@example.com"} })
```

It is not available on SQL Server, Oracle and Redshift (`UNSUPPORTED_FEATURE`); use `Upsert` there.

#### Auto-incrementing IDs

`Create` fills the model's primary key after the insert, so there is no separate `insertGetId`:

```go
u := &User{Email: "john@example.com"}
err := orm.New[User](db).Create(ctx, u)
fmt.Println(u.ID)
```

### Upserts

`Upsert` inserts rows, and updates the listed columns of those that already exist (by the `uniqueBy` columns):

```go
err := orm.New[Flight](db).Upsert(ctx, []Flight{
    {Departure: "Oakland", Destination: "San Diego", Price: 99},
    {Departure: "Chicago", Destination: "New York", Price: 150},
}, []string{"departure", "destination"}, []string{"price"})
```

PostgreSQL, CockroachDB and SQLite use `ON CONFLICT`, MySQL and MariaDB `ON DUPLICATE KEY UPDATE`, SQL Server and Oracle `MERGE`. Every database except SQL Server needs a primary key or unique index on the `uniqueBy` columns. MySQL and MariaDB ignore `uniqueBy` and use the table's own unique indexes.

## Update statements

```go
affected, err := query.Table(db, "users").Where("id = ?", 1).Update(ctx, query.Row{"votes": 1})

affected, err = orm.New[User](db).Query().Where("active = ?", false).Update(ctx, map[string]any{"status": "inactive"})
```

#### Update or insert

```go
flight, err := orm.New[Flight](db).UpdateOrCreate(ctx,
    Flight{Departure: "Oakland", Destination: "San Diego"}, // match
    Flight{Price: 99, Discounted: true},                    // set
)
```

`UpdateOrCreateMany(ctx, keys, rows)` does the same for many rows.

### Updating JSON columns

Update a JSON column as a whole: load the model, change its `orm.JSONColumn[T]` value and save it (see [Mutators and casting](mutators-casting.md#array-and-json-casting)). Laravel's `options->enabled` path updates have no builder form.

### Increment and decrement

```go
n, err := orm.New[User](db).Query().Where("id = ?", 1).Increment(ctx, "votes", 1)
n, err = orm.New[User](db).Query().Where("id = ?", 1).Decrement(ctx, "votes", 5)
```

To change other columns in the same statement, use `Update` with an expression:

```go
q.Update(ctx, map[string]any{"votes": gorm.Expr("votes + ?", 1), "name": "John"})
```

## Delete statements

```go
affected, err := query.Table(db, "users").Where("votes > ?", 100).Delete(ctx)
affected, err = orm.New[User](db).Query().Where("votes > ?", 100).Delete(ctx)

err = orm.New[User](db).Truncate(ctx) // TRUNCATE TABLE (DELETE FROM on SQLite)
```

`Update` and `Delete` **without a `Where` return an error** instead of touching every row. `Truncate` is the explicit way to empty a table. A model `Delete` on a soft-deleting model sets `deleted_at` instead of removing the rows (see [Models](models.md#soft-deleting)).

## Pessimistic locking

`SharedLock` keeps the selected rows from being modified until the transaction commits; `LockForUpdate` also keeps other transactions from locking or reading them for update:

```go
err := db.Transaction(ctx, func(tx *migro.DB) error {
    sender, err := orm.New[Account](tx).Query().Where("id = ?", senderID).LockForUpdate().First(ctx)
    if err != nil {
        return err
    }
    if sender.Balance < amount {
        return ErrInsufficientFunds
    }
    sender.Balance -= amount
    return sender.Save(ctx)
})
```

`LockForUpdate` adds `FOR UPDATE`; `SharedLock` adds `FOR SHARE`, or `LOCK IN SHARE MODE` on MySQL and MariaDB. Use them inside a transaction. SQLite ignores both, since a write locks the whole database. SQL Server (which locks through table hints) and Oracle's shared lock fail with `UNSUPPORTED_FEATURE`.

## Reusable query components

A scope is a function from a query to a query; `Apply` applies several (see [Query scopes](query-scopes.md)):

```go
func Destination(d string) func(*orm.Query[Flight]) *orm.Query[Flight] {
    return func(q *orm.Query[Flight]) *orm.Query[Flight] {
        return q.When(d != "", func(q *orm.Query[Flight]) *orm.Query[Flight] { return q.Where("destination = ?", d) })
    }
}

flights, err := orm.New[Flight](db).Query().Apply(Destination(dest), Active).OrderBy("price", "asc").Get(ctx)
```

`Tap` runs a function on the query without breaking the chain (Laravel's `tap`); Laravel's `pipe`, which returns something other than a query, is a plain function call in Go:

```go
q := orm.New[Flight](db).Query().Tap(func(q *orm.Query[Flight]) { q.Where("active = ?", true) })
page, err := Paginate(r, q) // your own helper
```

## Debugging

`ToSQL` renders the statement with its bindings inlined, without running it (Laravel's `toRawSql`):

```go
sql, err := orm.New[User](db).Query().Where("votes > ?", 100).Limit(5).ToSQL(ctx)
fmt.Println(sql) // SELECT * FROM "users" WHERE votes > 100 LIMIT 5
```

To see every statement that runs, use `db.Listen` or the query log ([Strict mode and debugging](strict-mode.md)).
