---
title: Mutators and casting
nav_order: 9.2
description: Transform attributes on the way in and out — casts, mutators, accessors, hashed and encrypted columns, typed JSON columns.
---

# Mutators and casting

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

Accessors, mutators and attribute casting let you transform a value when you read it from a model or set it on one. In Laravel they are `Attribute::make`, `get…Attribute` and `$casts`. In migro they are all one thing, a **cast**, because Go has no magic attribute methods and the struct field already has the right type.

A cast has two directions:

- **`Set`** runs when a value *enters* the model through `Fill`, `Update`, `Make`, `CreateFrom` or `SetAttribute`. This is the mutator.
- **`Get`** runs when a value *leaves* through `ToMap`, `JSON` or `GetAttribute`. This is the accessor.

{: .important }
The struct field always holds the **stored** value. Plain assignment (`user.Email = "X"`) does not run a cast, and `Save` writes the field as it is. If a value must be transformed, set it with `SetAttribute` or `Fill`. This keeps dirty tracking exact: a cast can never make a clean model look dirty.

## Declaring casts

Implement `Casts()` on the model, keyed by **column name**:

```go
func (User) Casts() map[string]orm.Cast {
    return map[string]orm.Cast{
        "password":  orm.CastHashed(),
        "api_token": orm.CastEncrypted(tokenKey),
        "role":      orm.CastEnum("admin", "editor", "member"),
        "birthday":  orm.CastDate("2006-01-02"),
        "email": orm.CastFunc{OnSet: func(v any) (any, error) {
            return strings.ToLower(strings.TrimSpace(fmt.Sprint(v))), nil
        }},
    }
}
```

A cast is an interface with two methods; implement it yourself for anything the built-ins do not cover:

```go
type Cast interface {
    Get(raw any) (any, error)   // stored value -> output value
    Set(v any) (any, error)     // incoming value -> stored value
}
```

A cast that returns an error fails the operation with **`CAST_FAILED`** (a `*migro.Error` that names the column), before anything is written:

```go
_, err := users.CreateFrom(ctx, map[string]any{"role": "root", ...})
migro.CodeOf(err) // "CAST_FAILED"
```

## Custom mutators and accessors

`orm.CastFunc` builds a cast from one or two functions. A missing side passes the value through.

```go
// mutator: normalise on the way in
"email": orm.CastFunc{OnSet: func(v any) (any, error) {
    return strings.ToLower(fmt.Sprint(v)), nil
}}

// accessor: change what serialization shows
"name": orm.CastFunc{OnGet: func(raw any) (any, error) {
    return strings.ToUpper(raw.(string)), nil
}}
```

`OnGet` only affects **output** (`ToMap`, `JSON`, `GetAttribute`), never the struct field, so `user.Name` stays what the database holds.

For an accessor that combines columns (Laravel's `fullName`), use `Appends()` — see [Serialization](serialization.md#appending-values).

## Setting and reading one attribute

```go
err := user.SetAttribute("password", "s3cret")   // runs the cast; ignores Fillable / Guarded
v, err := user.GetAttribute("birthday")          // "1990-05-17", through the cast
```

`SetAttribute` is the programmatic `$model->column = value`: it bypasses [mass assignment](models.md#mass-assignment) rules (you are setting it deliberately) but not the cast. An unknown column returns an error.

## Built-in casts

| Constructor | Set (in) | Get (out) |
|---|---|---|
| `CastBool()` | `"1"`, `"true"`, `"on"`, `"yes"`, non-zero numbers → `true` | same coercion |
| `CastInt()` | numeric strings and floats → `int64` | same |
| `CastFloat()` | numeric strings and integers → `float64` | same |
| `CastDate(layout)` | string → `time.Time` (parsed with *layout*) | `time.Time` → string |
| `CastJSON()` | any value → JSON text | JSON text → `map`/`slice`/scalar |
| `CastEnum(values…)` | rejects values outside the list | unchanged |
| `CastHashed()` | bcrypt hash | unchanged (the hash) |
| `CastEncrypted(key)` | AES-GCM ciphertext | plaintext |

Unparseable input (`"maybe"` for a bool, `"17/05/1990"` for a `2006-01-02` date) is a `CAST_FAILED` error, never a silent zero. `nil` passes through every built-in.

### Dates

```go
Birthday *time.Time
// ...
"birthday": orm.CastDate("2006-01-02")

u, _ := users.CreateFrom(ctx, map[string]any{"birthday": "1990-05-17"})
u.Birthday            // time.Time, 1990-05-17 00:00 UTC
orm.ToMap(u)          // {"birthday": "1990-05-17"}
```

The layout is a Go layout (`2006-01-02`), not a PHP format. To format *all* times in one serialization call instead, use [`DateFormat`](serialization.md#formatting-dates).

### JSON text columns

`CastJSON` is for a **string** column that holds JSON, when you want maps and slices at the edges:

```go
Meta string            // column: text / json
"meta": orm.CastJSON()

user.Fill(map[string]any{"meta": map[string]any{"tags": []string{"a"}}})  // stored as '{"tags":["a"]}'
user.GetAttribute("meta")                                                  // map[string]any{"tags": []any{"a"}}
```

If you know the shape, prefer the typed column below.

### Enums

`CastEnum` gives a string column the validation Go's type system does not:

```go
"role": orm.CastEnum("admin", "editor", "member")

users.CreateFrom(ctx, map[string]any{"role": "root"})  // CAST_FAILED, nothing inserted
```

It checks the value on every `Fill`/`Update`/`SetAttribute`. Pair it with a database default (`gorm:"default:member"` and `t.String("role").Default("member")`) for the value you leave out. For a database-level guarantee use `t.Enum("role", ...)` in the [migration](schema-builder.md).

## Hashed columns

`CastHashed()` bcrypt-hashes a string when it is set, so a plain password never reaches the database:

```go
Password string
func (User) Hidden() []string { return []string{"password"} }   // never serialize the hash
"password": orm.CastHashed()

u, _ := users.CreateFrom(ctx, map[string]any{"email": "ada@example.com", "password": "s3cret"})
// users.password = "$2a$10$..."

orm.CompareHash(u.Password, "s3cret")   // true
orm.CompareHash(u.Password, "wrong")    // false
```

- Every hash is salted, so hashing the same text twice gives different strings.
- A value that already is a bcrypt hash is kept as it is. Re-filling a loaded model with its own hash (a common form round-trip) does not hash it twice. A string that only *looks* like a hash is hashed.
- `CastHashedCost(n)` sets the bcrypt cost. The default is bcrypt's default (10); raise it as hardware allows, and lower it in tests (`bcrypt.MinCost`) so the suite stays fast.
- It hashes text (`string` or `[]byte`); anything else is `CAST_FAILED`.
- It protects the stored value, not the model in memory: after `user.Password = "plain"` the field holds plain text until you go through `SetAttribute`/`Fill`.

## Encrypted columns

`CastEncrypted(key)` encrypts with AES-GCM and decrypts when the value is read for output:

```go
var tokenKey = []byte(os.Getenv("TOKEN_KEY"))     // 16, 24 or 32 bytes
APIToken string                                    // column: text
"api_token": orm.CastEncrypted(tokenKey)

user.SetAttribute("api_token", "tok-123")
user.Save(ctx)
// users.api_token = "k3f...==" (base64 of nonce + ciphertext)

v, _ := user.GetAttribute("api_token")   // "tok-123"
```

- The key is passed to the constructor, not set globally. Read it from configuration once and keep it in a variable or a function.
- A fresh random nonce is used on every write: encrypting the same text twice gives different ciphertext, and you **cannot** search or compare encrypted columns in SQL.
- GCM authenticates: a wrong key or a modified value is a `CAST_FAILED` error, never garbage text.
- A key whose size is not 16, 24 or 32 bytes fails every use with `CAST_FAILED`.
- **Rotating the key makes existing rows unreadable.** To rotate, read with the old key, then write with the new one (two models, or a migration that re-encrypts).
- The stored value is base64 text, so the column must be `text`, and you should list the column in `Hidden()` unless you really want decrypted values in JSON.
- The struct field (`user.APIToken`) holds the ciphertext; only `GetAttribute` and serialization decrypt.

## Typed JSON columns

For a JSON column with a known shape, use `orm.JSONColumn[T]`. The field holds a `T`; the database holds its JSON. No cast is needed.

```go
type UserSettings struct {
    Theme         string `json:"theme"`
    Notifications bool   `json:"notifications"`
}

type User struct {
    orm.BaseModel
    Settings orm.JSONColumn[UserSettings]
}

// migration
t.JSON("settings").Nullable()
```

```go
user.Settings.Data.Theme = "dark"       // plain Go, fully typed
user.IsDirty("settings")                // true
user.Save(ctx)                          // UPDATE users SET settings = '{"theme":"dark",...}'
```

- It works with any `T`: structs, `map[string]any`, slices. A zero `T` that marshals to `null` (a nil map or slice) is stored as `NULL`; a struct is always stored as an object.
- Loading **resets** the value first, so fields from a previous row never leak into the next.
- Serialization emits the nested value (`"settings": {"theme": "dark"}`), not a `{"Data": ...}` wrapper.
- Set it through mass assignment with the typed value: `user.Update(ctx, map[string]any{"settings": orm.JSONColumn[UserSettings]{Data: s}})`.
- Column type: `JSONB` on PostgreSQL, `JSON` on MySQL, `TEXT` elsewhere (as GORM sees it; the migration's `t.JSON` decides the real type).
- You cannot filter on keys with portable SQL. Use your database's JSON operators through `Where` when you need to.

## Writing your own cast

```go
// stores money as integer cents, shows it as a decimal string
type moneyCast struct{}

func (moneyCast) Set(v any) (any, error) {
    f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
    if err != nil { return nil, err }
    return int64(math.Round(f * 100)), nil
}

func (moneyCast) Get(raw any) (any, error) {
    cents, ok := raw.(int64)
    if !ok { return nil, fmt.Errorf("money: %T", raw) }
    return fmt.Sprintf("%.2f", float64(cents)/100), nil
}
```

Return plain `error`s; migro wraps them as `CAST_FAILED` with the column name. Return a `*migro.Error` yourself to keep a different code.

## How casts interact with the rest of the ORM

| Operation | Cast applied |
|---|---|
| `Fill`, `Update`, `Make`, `CreateFrom`, `SetAttribute` | `Set` |
| `ToMap`, `ToMapWith`, `JSON`, `Collection.ToMaps`, `GetAttribute` | `Get` |
| `Save`, `Create`, `CreateMany` | none: the field is written as it is |
| `Query().Where(...)`, `Update(map)` on a query | none: you are writing SQL values |
| Dirty tracking (`IsDirty`, `Dirty`, `GetOriginal`) | none: compares stored values |
| Factories and seeders using struct literals | none |

The last row matters for hashed and encrypted columns: a seeder that writes `User{Password: "x"}` stores `x` in plain text. Seed through `CreateFrom` or `SetAttribute`.

## Testing casts

Casts are plain values, so unit tests need no database (`orm/cast_test.go`). The integration suite (`integration/casting_test.go`) checks what really reaches the table: a hash instead of the password, ciphertext instead of the token, an enum rejected before the `INSERT`, dates round-tripped, and typed JSON saved through `Save`. It runs on SQLite, PostgreSQL and MySQL.
