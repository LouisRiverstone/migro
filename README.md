<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.png">
    <img src="docs/assets/logo.png" alt="migro: models and migrations for Go" width="460">
  </picture>
</p>

<p align="center">
  Eloquent and Lucid models, Knex-style schema builder, Laravel-style migrations and seeders, on top of <a href="https://gorm.io">GORM</a>.
</p>

<p align="center">
  <a href="https://github.com/LouisRiverstone/migro/actions/workflows/ci.yml"><img src="https://github.com/LouisRiverstone/migro/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/LouisRiverstone/migro"><img src="https://pkg.go.dev/badge/github.com/LouisRiverstone/migro.svg" alt="Go reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT license"></a>
  <a href="https://louisriverstone.github.io/migro/"><img src="https://img.shields.io/badge/docs-read-67c7dc.svg" alt="Documentation"></a>
</p>

---

**migro** adds what GORM leaves out: Eloquent/Lucid-style models with typed relations, a schema builder, versioned migrations as Go files, `DatabaseSeeder`-style seeders and factories, events, read replicas, sharding, and an artisan/ace-style CLI. Your GORM models and queries keep working unchanged.

## Install

Go 1.25+.

```
go get github.com/LouisRiverstone/migro@latest
go install github.com/LouisRiverstone/migro/cmd/migro@latest
```

## A quick look

Generate and run a migration, with the app running or not:

```
migro make:migration create_posts_table
migro migration:run
migro migration:rollback --step=1
```

```go
// database/migrations/20260101120000_create_posts_table.go
func (CreatePostsTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "posts", func(t *schema.Table) {
		t.BigIncrements("id")
		t.ForeignID("user_id").Constrained().OnDelete("CASCADE")
		t.String("title").NotNull().Index()
		t.Timestamps()
	})
}
```

Declare models with typed relations (names follow Laravel's conventions):

```go
type Post struct {
	orm.BaseModel                                            // ID, CreatedAt, UpdatedAt (SoftDeleteModel adds DeletedAt)
	UserID uint
	Title  string

	Author   orm.BelongsTo[User] `orm:"foreignKey:user_id"`
	Comments orm.MorphMany[Comment] `orm:"morph:commentable"`
	Tags     orm.MorphToMany[Tag] `orm:"morph:taggable"`
}

func (Post) Fillable() []string { return []string{"user_id", "title"} }   // mass-assignment allow-list
```

Query, load and write like Eloquent/Lucid:

```go
db, _ := migro.Open(migro.Config{Dialect: "postgres", DSN: dsn})
posts := orm.New[Post](db)

page, _ := posts.Query().
	Where("title LIKE ?", "%go%").
	WhereHas("Tags", func(q *gorm.DB) *gorm.DB { return q.Where("name = ?", "orm") }).
	With("Author").With("Comments.User").WithCount("Tags").   // eager loading, never one query per row
	Latest().Paginate(ctx, 1, 20)

post, _ := posts.CreateFrom(ctx, map[string]any{"user_id": 1, "title": "Hello"})
post.Title = "Renamed"
post.IsDirty("title")                                       // true
_ = post.Save(ctx)                                          // UPDATE of the changed columns only
_, _ = post.Comments.Create(ctx, map[string]any{"body": "Nice"})  // fills commentable_type / commentable_id
_, _ = post.Tags.Sync(ctx, 1, 2)                            // pivot writes: Attach, Detach, Sync, Toggle
author, _ := post.Author.Get(ctx)                           // lazy load
```

Seed it the Laravel way, with a `DatabaseSeeder` that calls the others in foreign-key order:

```go
type DatabaseSeeder struct{}

func init() { seed.Register(&DatabaseSeeder{}) }

func (DatabaseSeeder) Run(ctx context.Context, db *migro.DB) error {
	return seed.Call(ctx, db, &CountrySeeder{}, &UserSeeder{}, &PostSeeder{})   // one transaction
}

// in PostSeeder: factories with states, sequences, relations and parents
users, _ := UserFactory().Sequence(
	func(i int, u *User) { u.Role = "admin" },
	func(i int, u *User) { u.Role = "editor" },
).For("country").With("posts", 3).CreateMany(ctx, db, 10)
```

```
migro db:seed                       # runs DatabaseSeeder
migro migration:fresh --seed        # rebuild the database and seed it
```

## What you get

- **Schema builder** and **query builder** in the style of Knex
- **Models** in the style of Eloquent and Lucid: instance API (`Save`, `Delete`, `Restore`, `Refresh`), dirty tracking, mass assignment, `Hidden`/`Appends` serialization, aggregates and `Chunk`
- **Scopes, casts, resources and collections**: local and global query scopes, hashed/encrypted/enum/date/JSON casts, API resources with pagination, and `Collection[T]` helpers (`Filter`, `KeyBy`, `GroupBy`, `Load`, `Fresh`)
- **Relations** as typed fields: has-one/many, belongs-to, many-to-many with pivot writes, `hasManyThrough`, and the polymorphic family (`MorphTo`, `MorphMany`, `MorphToMany`, ...), with `With`, `WithCount`, `Has`/`WhereHas` and lazy loading
- **Migrations** like Laravel: timestamped files, batches, locking, `--pretend`, `migration:diff`
- **Seeders and factories** like Laravel/Lucid: `DatabaseSeeder` with `seed.Call`, `--class`, `--once`, states, sequences, `For`/`With`, `UpdateOrCreateMany` and a built-in faker
- **Events**, **connection pool options**, **read replicas** and **sharding**

PostgreSQL, CockroachDB, Redshift, MySQL, MariaDB, SQLite, SQL Server and Oracle. PostgreSQL, MySQL and SQLite run in CI; the others are unit-tested but not yet verified against live servers.

## Documentation

**<https://louisriverstone.github.io/migro/>**: installation, a step-by-step tutorial, how it works, and a page for every feature.

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) first.

## License

[MIT](LICENSE)
