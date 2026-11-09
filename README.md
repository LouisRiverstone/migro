<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.png">
    <img src="docs/assets/logo.png" alt="migro: models and migrations for Go" width="460">
  </picture>
</p>

<p align="center">
  Lucid and Knex ergonomics, Laravel-style migrations, on top of <a href="https://gorm.io">GORM</a>.
</p>

<p align="center">
  <a href="https://github.com/LouisRiverstone/migro/actions/workflows/ci.yml"><img src="https://github.com/LouisRiverstone/migro/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/LouisRiverstone/migro"><img src="https://pkg.go.dev/badge/github.com/LouisRiverstone/migro.svg" alt="Go reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT license"></a>
  <a href="https://louisriverstone.github.io/migro/"><img src="https://img.shields.io/badge/docs-read-67c7dc.svg" alt="Documentation"></a>
</p>

---

**migro** adds what GORM leaves out: a schema builder, versioned migrations as Go files, seeders and factories, events, read replicas, sharding, and an artisan/ace-style CLI. Your GORM models and queries keep working unchanged.

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

Query with generic models:

```go
db, _ := migro.Open(migro.Config{Dialect: "postgres", DSN: dsn})

type Post struct {
	orm.BaseModel
	UserID uint
	Title  string
	Author orm.BelongsTo[User]    `gorm:"-" orm:"foreignKey:user_id"`
	Tags   orm.MorphToMany[Tag]   `gorm:"-" orm:"morph:taggable"`
}

posts := orm.New[Post](db)
page, _ := posts.Query().Where("title LIKE ?", "%go%").With("Author").With("Tags").WithCount("Tags").Paginate(ctx, 1, 20)

post, _ := posts.Find(ctx, 1)
post.Title = "Renamed"
_ = post.Save(ctx)                       // updates only the changed columns
_ = post.Tags.Sync(ctx, 1, 2)            // Eloquent-style pivot writes
```

## What you get

- **Schema builder** and **query builder** in the style of Knex
- **Models** in the style of Eloquent and Lucid: instance API, dirty tracking, mass assignment, typed relation fields (including `hasManyThrough` and polymorphic ones), eager/lazy loading and pivot writes
- **Migrations** like Laravel: timestamped files, batches, locking, `--pretend`, `migration:diff`
- **Seeders and factories** like Laravel/Lucid: `DatabaseSeeder` with `seed.Call`, states, sequences, relations and a built-in faker
- **Events**, **connection pool options**, **read replicas** and **sharding**

PostgreSQL, CockroachDB, Redshift, MySQL, MariaDB, SQLite, SQL Server and Oracle. PostgreSQL, MySQL and SQLite run in CI; the others are unit-tested but not yet verified against live servers.

## Documentation

**<https://louisriverstone.github.io/migro/>**: installation, a step-by-step tutorial, how it works, and a page for every feature.

## Contributing

Issues and pull requests are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) first.

## License

[MIT](LICENSE)
