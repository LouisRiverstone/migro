---
title: Getting started
nav_order: 3
description: Build a small project from an empty folder to a migrated and seeded database.
---

# Getting started

This walks through a tiny blog: one `posts` table, a model, fake data, and a program that reads it. It uses SQLite so you need nothing else installed. Every command and output below was run for real.

You need Go 1.25+ and the `migro` command ([Installation](installation.md)).

## 1. Create the project

```
mkdir shop && cd shop
go mod init example.com/shop
go get github.com/LouisRiverstone/migro@latest
```

Tell `migro` which database to use. Create a `.env` file in the project root:

```
MIGRO_DIALECT=sqlite
MIGRO_DSN=shop.db
MIGRO_ENV=development
```

`migro` reads `.env` (real environment variables win). Add `.env` to `.gitignore`.

## 2. Generate the files

```
$ migro make:model Post -a
Created Model: app/models/post.go
Created Migration: database/migrations/20261108191926_create_posts_table.go
Created Factory: database/factories/post_factory.go
Created Seeder: database/seeders/post_seeder.go
```

`-a` creates the model plus its migration, factory and seeder. Without it, `make:model Post` creates only the model, and you can add `-m`, `-f` or `-s` for one at a time. The timestamp in your migration's file name will differ.

## 3. Describe the table

Open the migration. It was generated with a table name inferred from the file name (`create_posts_table` means `posts`). Fill in the columns:

```go
// database/migrations/20261108191926_create_posts_table.go
func (CreatePostsTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "posts", func(t *schema.Table) {
		t.BigIncrements("id")
		t.String("title").NotNull()
		t.Text("body")
		t.Boolean("published").NotNull().Default(false)
		t.Timestamps()
		t.Index("published")
	})
}

func (CreatePostsTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "posts")
}
```

Before running anything, you can see the SQL it would produce:

```
$ migro migration:run --pretend
-- 20261108191926_create_posts_table
CREATE TABLE "posts" (
  "id" integer primary key autoincrement,
  "title" varchar(255) NOT NULL,
  "body" text,
  "published" boolean DEFAULT 0 NOT NULL,
  "created_at" datetime,
  "updated_at" datetime
);
CREATE INDEX "posts_published_index" ON "posts" ("published");
```

Nothing was created. The SQL differs per database; that is the point of the schema builder.

## 4. Run the migration

```
$ migro migration:run
Migrating:  20261108191926_create_posts_table
Migrated:   20261108191926_create_posts_table (471µs)

$ migro migration:status
Ran?  Migration                                                    Batch
Yes   20261108191926_create_posts_table                            1
```

The first call is slower than the second: `migro` compiles your project's migrations into a runner each time (see [How it works](how-it-works.md)).

## 5. Fill in the model, factory and seeder

```go
// app/models/post.go
type Post struct {
	orm.BaseModel
	Title     string
	Body      string
	Published bool
}
```

```go
// database/factories/post_factory.go
func PostFactory() *seed.Factory[models.Post] {
	return seed.Define(func(f *seed.Faker) models.Post {
		return models.Post{
			Title:     f.Sentence(),
			Body:      f.Paragraph(),
			Published: f.Bool(),
		}
	})
}
```

```go
// database/seeders/post_seeder.go
import (
	"context"

	"example.com/shop/database/factories"
	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/seed"
)

func init() { seed.Register(&PostSeeder{}) }

type PostSeeder struct{}

func (PostSeeder) Run(ctx context.Context, db *migro.DB) error {
	_, err := factories.PostFactory().CreateMany(ctx, db, 10)
	return err
}
```

Then run it:

```
$ migro db:seed
Seeded:   PostSeeder
```

## 6. Use it from your application

```go
// cmd/shop/main.go
package main

import (
	"context"
	"fmt"
	"log"

	"example.com/shop/app/models"
	"github.com/LouisRiverstone/migro"
	_ "github.com/LouisRiverstone/migro/dialect/sqlite"
	"github.com/LouisRiverstone/migro/orm"
)

func main() {
	ctx := context.Background()
	db, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: "shop.db"})
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	posts := orm.New[models.Post](db)
	page, err := posts.Query().Where("published = ?", true).OrderBy("id", "desc").Paginate(ctx, 1, 3)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d published posts, showing %d:\n", page.Total, len(page.Data))
	for _, p := range page.Data {
		fmt.Println("-", p.Title)
	}
}
```

```
$ go mod tidy
$ go run ./cmd/shop
5 published posts, showing 3:
- Consectetur amet tempor tempor enim.
- ...
```

Your numbers and sentences will differ: the factory is random unless you call `.Seed(42)`.

## 7. Change the schema later

You never edit a migration that has already run. You add a new one:

```
$ migro make:migration add_slug_to_posts_table
Created Migration: database/migrations/20261108191945_add_slug_to_posts_table.go
```

Because the name is `add_<column>_to_<table>_table`, the stub is an `AlterTable` for `posts`. Fill in `Up` and `Down`:

```go
func (AddSlugToPostsTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.AlterTable(ctx, "posts", func(t *schema.Table) {
		t.String("slug", 160)
		t.Unique("slug")
	})
}

func (AddSlugToPostsTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.AlterTable(ctx, "posts", func(t *schema.Table) {
		t.DropUnique("posts_slug_unique")
		t.DropColumn("slug")
	})
}
```

```
$ migro migration:run
$ migro migration:rollback --step=1       # undo just the last migration
$ migro migration:status
```

## Where next

- [Schema builder](schema-builder.md): every column type, indexes, foreign keys.
- [Command line](command-line.md): every command and flag, production safety, several databases.
- [Models](models.md) and [Relations](relations.md) for the query side.
- [Migrations](migrations.md) for how batches, locking and `--pretend` work.
