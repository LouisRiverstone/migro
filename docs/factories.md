---
title: Factories
nav_order: 14.5
description: "Define model factories, states, sequences and callbacks; make and create models; create related models through has-many, belongs-to and many-to-many."
---

# Factories

<details open markdown="block">
  <summary>Table of contents</summary>
  {: .text-delta }
1. TOC
{:toc}
</details>

## Introduction

When you test your application or seed your database, you need rows. Instead of writing each one by hand, define a **factory** per model: the default values of its fields, generated with a fake data source. Factories live in `database/factories` (`MIGRO_FACTORIES_DIR`):

```go
// database/factories/user_factory.go
package factories

func UserFactory() *seed.Factory[models.User] {
    return seed.Define(func(f *seed.Faker) models.User {
        return models.User{
            Name:     f.Name(),
            Email:    f.Email(),
            Password: "password", // hashed by the model's cast
        }
    }).
        State("unverified", func(u *models.User) { u.EmailVerifiedAt = nil })
}
```

The function passed to `seed.Define` is Laravel's `definition()`: it returns a model with its default values. `seed.Faker` is the fake data source:

| Method | Gives |
|---|---|
| `Name()`, `FirstName()`, `LastName()` | person names |
| `Email()` | an address on a reserved `example.*` domain, never a real one |
| `Username()` | a user name |
| `Word()`, `Words(n)`, `Sentence()`, `Paragraph()` | text |
| `Int(min, max)`, `Float(min, max)`, `Bool()` | numbers |
| `Pick(options...)` | one of the options |
| `UUID()` | a UUID |
| `Date(from, to)` | a time between two times |

The faker is **deterministic** for a given seed: `factory.Seed(42)` produces the same models on every run, which makes failing tests reproducible.

## Defining model factories

### Generating factories

```
migro make:factory User
migro make:factory User --model=example.com/app/models.User
migro make:model User -f        # the model and its factory
migro make:model User -a        # the model, its migration, factory and seeder
```

`make:factory` writes `database/factories/user_factory.go` with a `UserFactory()` function. `--model` names the model's import path and type; the default is `<module>/app/models.<Name>`.

#### Model and factory discovery conventions

Laravel finds a model's factory by name (`User::factory()`). In Go, the factory is a function you call: `factories.UserFactory()`. There is nothing to register.

### Factory states

A state is a named change applied on top of the defaults:

```go
func UserFactory() *seed.Factory[models.User] {
    return seed.Define(func(f *seed.Faker) models.User { ... }).
        State("suspended", func(u *models.User) { u.AccountStatus = "suspended" }).
        State("admin", func(u *models.User) { u.Role = "admin" })
}

users, err := factories.UserFactory().Apply("suspended", "admin").MakeMany(5)
```

#### "Trashed" state

Laravel gives soft-deleting models a `trashed()` state. Define it like any other state:

```go
State("trashed", func(u *models.User) { u.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true} })
```

### Factory callbacks

`AfterMaking` runs on each model once it is built, for both `Make` and `Create`. `After` runs after each model is saved, with the database:

```go
func UserFactory() *seed.Factory[models.User] {
    return seed.Define(...).
        AfterMaking(func(u *models.User) { u.Email = strings.ToLower(u.Email) }).
        After(func(ctx context.Context, db *migro.DB, u *models.User) error {
            return orm.New[models.Profile](db).Create(ctx, &models.Profile{UserID: u.ID})
        })
}
```

Callbacks can be added on a copy for a single use: `factories.UserFactory().After(...)`.

## Creating models using factories

### Instantiating models

`Make` builds a model without saving it; `MakeMany` builds several:

```go
user, err := factories.UserFactory().Make()
users, err := factories.UserFactory().MakeMany(3)
```

#### Applying states

```go
users, err := factories.UserFactory().Apply("suspended").MakeMany(5)
```

#### Overriding attributes

`Merge` changes some fields on top of the defaults (Laravel's `make(['name' => ...])` and `state([...])`):

```go
user, err := factories.UserFactory().Merge(func(u *models.User) { u.Name = "Abigail Otwell" }).Make()
```

`Merge`, `Apply`, `With`, `For`, `Seed` and `Sequence` return **copies**, so a factory you share between tests is never changed by one of them.

### Persisting models

`Create` builds and saves a model; `CreateMany` saves several, all or nothing:

```go
user, err := factories.UserFactory().Create(ctx, db)
users, err := factories.UserFactory().CreateMany(ctx, db, 3)
user, err = factories.UserFactory().Merge(func(u *models.User) { u.Name = "Abigail" }).Create(ctx, db)
```

Created models are bound to the database, so `user.Posts.Create(...)` and `user.Save(ctx)` work on them.

### Sequences

A sequence alternates values across the models it builds: model `i` gets `fns[i % len(fns)]`:

```go
users, err := factories.UserFactory().Sequence(
    func(i int, u *models.User) { u.Admin = true },
    func(i int, u *models.User) { u.Admin = false },
).CreateMany(ctx, db, 10) // five admins, five not
```

The index `i` is the position of the model in the batch, so one function can compute the value (Laravel's closure sequence with `$sequence->index`):

```go
factories.UserFactory().Sequence(func(i int, u *models.User) {
    u.Email = fmt.Sprintf("user%d@example.com", i)
})
```

## Factory relationships

Relationships are declared on the factory, by name, with the function that creates them. Then `With` (has many) and `For` (belongs to) use them.

### Has many relationships

```go
func UserFactory() *seed.Factory[models.User] {
    return seed.Define(...).
        Relation("posts", func(ctx context.Context, db *migro.DB, u *models.User, n int, f *seed.Faker) error {
            _, err := factories.PostFactory().
                Merge(func(p *models.Post) { p.UserID = u.ID }).
                CreateMany(ctx, db, n)
            return err
        })
}

user, err := factories.UserFactory().With("posts", 3).Create(ctx, db)
```

The callback receives the saved parent, so the children can use its key, or any of its fields (Laravel's state closure that receives the parent). Inside the callback the parent is a plain struct: call `orm.Bind(db, u)` first to use its relation fields, as in `u.Posts.CreateMany(...)`.

### Belongs to relationships

`Parent` declares how to create a model's parent; `For` uses it, so each model gets a new parent before it is saved:

```go
func PostFactory() *seed.Factory[models.Post] {
    return seed.Define(...).
        Parent("user", func(ctx context.Context, db *migro.DB, p *models.Post, f *seed.Faker) error {
            user, err := factories.UserFactory().Create(ctx, db)
            if err != nil {
                return err
            }
            p.UserID = user.ID
            return nil
        })
}

posts, err := factories.PostFactory().For("user").CreateMany(ctx, db, 3) // three posts, three users
```

To give every post the same existing parent, set the key instead (Laravel's `for($user)` and `recycle()`):

```go
posts, err := factories.PostFactory().Merge(func(p *models.Post) { p.UserID = user.ID }).CreateMany(ctx, db, 3)
```

### Many to many relationships

Create the related models in a `Relation` callback and attach them through the relation field. `AttachWith` also sets pivot columns (Laravel's `hasAttached`):

```go
Relation("roles", func(ctx context.Context, db *migro.DB, u *models.User, n int, f *seed.Faker) error {
    roles, err := factories.RoleFactory().CreateMany(ctx, db, n)
    if err != nil {
        return err
    }
    orm.Bind(db, u)
    for _, r := range roles {
        if err := u.Roles.AttachWith(ctx, map[string]any{"active": true}, r.ID); err != nil {
            return err
        }
    }
    return nil
})

user, err := factories.UserFactory().With("roles", 3).Create(ctx, db)
```

To attach roles you already have to every user, attach them in an `After` callback.

### Polymorphic relationships

Morph-many relations work like has-many: in the callback, set the child's `<name>_type` and `<name>_id` from the parent (or create through the parent's `MorphMany` field after `orm.Bind`). A morph-to parent is a `Parent` callback that sets both columns. Polymorphic many-to-many relations attach like many-to-many.

### Defining relationships within factories

The definition function builds a model without a database, so it cannot create a parent itself. Use `Parent` and `For`, which run with the database before the model is saved, or set a key you already know.
