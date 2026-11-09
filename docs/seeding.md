---
title: Seeders and factories
nav_order: 14
description: Laravel and Lucid style seeders, DatabaseSeeder, factories.
---

# Seeders and factories

## Factories

```go
users := seed.Define(func(f *seed.Faker) User {
    return User{Name: f.Name(), Email: f.Email()}
}).
    State("admin", func(u *User) { u.Role = "admin" }).
    Relation("posts", func(ctx context.Context, db *migro.DB, u *User, n int, f *seed.Faker) error {
        for range n {
            if err := db.Gorm.Create(&Post{UserID: u.ID, Title: f.Sentence()}).Error; err != nil { return err }
        }
        return nil
    })

u, err := users.Make()                                             // built, not saved
list, err := users.CreateMany(ctx, db, 10)                         // saved; all-or-nothing
admin, err := users.Apply("admin").With("posts", 3).Create(ctx, db)
fixed, err := users.Merge(func(u *User) { u.Name = "Fixed" }).Seed(42).Make() // reproducible
```

More Laravel-style helpers:

```go
users.Sequence(                                                  // model i gets fns[i % len]
    func(i int, u *User) { u.Role = "admin" },
    func(i int, u *User) { u.Role = "editor" },
).MakeMany(4)

users.Parent("team", func(ctx context.Context, db *migro.DB, u *User, f *seed.Faker) error {
    team, err := teams.Create(ctx, db)
    u.TeamID = team.ID
    return err
})
users.For("team").CreateMany(ctx, db, 3)                          // each user gets a new team first

users.AfterMaking(func(u *User) { u.Email = strings.ToLower(u.Email) })   // runs for Make and Create
```

Models returned by `Create` / `CreateMany` are bound to the database, so `user.Posts.Create(...)` and `user.Save(ctx)` work on them. Inside a `Relation` callback the model is a plain struct: call `orm.Bind(db, u)` first to use relation fields.

`Merge`, `Apply`, `With`, `Seed`, `Sequence` and `For` return copies, so a shared factory is never mutated. `After(fn)` registers a hook that runs after each create.

The faker is deterministic for a given seed. It offers `Name`, `FirstName`, `LastName`, `Email` (reserved `example.*` domains), `Username`, `Word`, `Words`, `Sentence`, `Paragraph`, `Int`, `Float`, `Bool`, `Pick`, `UUID` and `Date`.

## Seeders

A seeder is a type with a `Run` method, registered like a migration. `migro make:seeder User` creates it:

```go
// database/seeders/user_seeder.go
func init() { seed.Register(&UserSeeder{}) }

type UserSeeder struct{}

func (UserSeeder) Run(ctx context.Context, db *migro.DB) error {
    // idempotent, like Lucid's updateOrCreateMany
    users, err := orm.New[User](db).UpdateOrCreateMany(ctx, []string{"email"}, []User{
        {Email: "ana@example.com", Name: "Ana"},
    })
    if err != nil { return err }
    _, err = users[0].Posts.Create(ctx, map[string]any{"title": "Hello"})   // relations work in seeders
    return err
}

// optional: limit where it runs
func (UserSeeder) Environments() []string { return []string{"development", "staging"} }
```

### The DatabaseSeeder

Registered seeders run in name order, which ignores foreign keys. Laravel's answer is a `DatabaseSeeder` that calls the others; migro does the same with `seed.Call`:

```go
type DatabaseSeeder struct{}

func init() { seed.Register(&DatabaseSeeder{}) }

func (DatabaseSeeder) Run(ctx context.Context, db *migro.DB) error {
    return seed.Call(ctx, db, &CountrySeeder{}, &UserSeeder{}, &PostSeeder{})
}
```

`seed.Call` stops at the first error and names the seeder that failed. Because the whole `DatabaseSeeder` runs in one transaction, a failure in the third seeder rolls back the first two.

```
migro db:seed                      # DatabaseSeeder if registered, otherwise every seeder by name
migro db:seed --class=UserSeeder   # one
migro db:seed --once               # skip seeders that already ran (tracked in migro_seeds)
migro migration:fresh --seed       # rebuild the database and seed it
```

From code: `r := seed.NewRunner(db, seed.Once()); r.AddRegistered(); r.Seed(ctx)`, or `r.RunOnly(ctx, "UserSeeder")`, `r.Run(ctx)` (all, by name), or `r.Add(seed.Seeder{Name: "...", Run: ...})` for ad-hoc ones.

- Each top-level seeder runs in its own transaction (a failure rolls back that seeder).
- Without `--once` they run every time (like Lucid), so make them idempotent.
- A seeder with no `Environments` runs everywhere **except production**; list `"production"` to allow it there.
- The environment comes from `MIGRO_ENV`, then `APP_ENV`, then `development`.
- `seed.WithoutEvents()` mutes model events while seeding (Laravel's `WithoutModelEvents`).

`migro make:factory User` creates `database/factories/user_factory.go` with a `UserFactory()` function; `migro make:model User -a` creates the model, its migration, factory and seeder together.
