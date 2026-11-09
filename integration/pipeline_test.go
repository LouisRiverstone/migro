// Package integration_test drives the whole pipeline — SQL and Go migrations, then seed files —
// against one realistic database (unique and composite indexes, 1:1, 1:N, N:M, self-referencing and
// polymorphic relations, soft deletes, cascading foreign keys) and asserts on what ends up in it.
package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/integration/database/factories"
	_ "github.com/LouisRiverstone/migro/integration/database/migrations"
	_ "github.com/LouisRiverstone/migro/integration/database/seeders"
	. "github.com/LouisRiverstone/migro/integration/models"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
	"github.com/LouisRiverstone/migro/seed"
	"gorm.io/gorm"
)

// ---- harness ----------------------------------------------------------------------

// allTables are the application tables, ordered so that dropping them in reverse never violates a key.
var allTables = []string{
	"countries", "roles", "tags", "users", "profiles", "role_user", "categories", "posts", "videos",
	"comments", "taggables", "products", "orders", "order_items",
}

// open returns a database with foreign keys enforced. SQLite ignores them unless asked, so the
// default backend gets the pragma in its DSN; live servers (MIGRO_TEST_DIALECT) enforce them already.
func open(t *testing.T) *migro.DB {
	t.Helper()
	if os.Getenv("MIGRO_TEST_DIALECT") != "" {
		return testdb.Open(t)
	}
	db, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: filepath.Join(t.TempDir(), "pipeline.db") + "?_pragma=foreign_keys(1)"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func migrator(t *testing.T, db *migro.DB) *migrate.Migrator {
	t.Helper()
	m := migrate.New(db)
	if err := m.AddRegistered(); err != nil {
		t.Fatal(err)
	}
	return m
}

// seedRunner holds every registered seeder; tests pick one with RunOnly, as `db:seed --class=` does.
func seedRunner(t *testing.T, db *migro.DB, env string, opts ...seed.Option) *seed.Runner {
	t.Helper()
	r := seed.NewRunner(db, append([]seed.Option{seed.WithEnvironment(env)}, opts...)...)
	if err := r.AddRegistered(); err != nil {
		t.Fatal(err)
	}
	return r
}

// migrated returns a database with every migration applied and no data.
func migrated(t *testing.T) (*migro.DB, *migrate.Migrator) {
	t.Helper()
	db := open(t)
	m := migrator(t, db)
	if _, err := m.Latest(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db, m
}

// seeded returns a fully migrated and seeded database.
func seeded(t *testing.T) *migro.DB {
	t.Helper()
	db, _ := migrated(t)
	if _, err := seedRunner(t, db, "testing").RunOnly(context.Background(), "DatabaseSeeder"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return db
}

func count(t *testing.T, db *migro.DB, table string, where ...any) int64 {
	t.Helper()
	var n int64
	q := db.Gorm.Table(table)
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	if err := q.Count(&n).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func mustFail(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected a constraint error, got none", what)
	}
}

func mustExec(t *testing.T, db *migro.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Gorm.Exec(sql, args...).Error; err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// ---- tests ------------------------------------------------------------------------

func TestMigrationsBuildTheWholeSchema(t *testing.T) {
	db, m := migrated(t)
	ctx := context.Background()

	for _, tbl := range allTables {
		if !db.Gorm.Migrator().HasTable(tbl) {
			t.Errorf("table %s missing", tbl)
		}
	}
	st, err := m.Status(ctx)
	if err != nil || len(st) != 16 {
		t.Fatalf("status: %+v %v", st, err)
	}
	for _, s := range st {
		if !s.Applied || s.Batch != 1 {
			t.Errorf("%s should be applied in batch 1: %+v", s.Name, s)
		}
	}

	// named indexes: from SQL files, from the Go builder, and from the later SQL migration
	for table, indexes := range map[string][]string{
		"users":     {"users_country_id_index", "users_deleted_at_index"},
		"role_user": {"role_user_role_id_index"},
		"posts":     {"posts_user_published_idx"},
		"comments":  {"comments_commentable_type_commentable_id_index", "comments_user_id_index"},
		"taggables": {"taggables_taggable_type_taggable_id_index"},
		"orders":    {"orders_status_placed_at_index"},
	} {
		for _, idx := range indexes {
			if !db.Gorm.Migrator().HasIndex(table, idx) {
				t.Errorf("index %s on %s missing", idx, table)
			}
		}
	}
	for _, col := range []struct{ table, column string }{
		{"users", "deleted_at"}, {"users", "phone"}, {"profiles", "bio"}, {"posts", "published"}, {"order_items", "unit_price"},
	} {
		if ok, _ := schema.New(db).HasColumn(ctx, col.table, col.column); !ok {
			t.Errorf("column %s.%s missing", col.table, col.column)
		}
	}
}

func TestConstraintsRejectBadData(t *testing.T) {
	db := seeded(t)
	g := db.Gorm

	cases := map[string]error{
		"unique email":                  g.Exec("INSERT INTO users (id, country_id, email, name) VALUES (90, 1, 'ana@example.com', 'dup')").Error,
		"unique country code":           g.Exec("INSERT INTO countries (id, code, name) VALUES (90, 'BR', 'dup')").Error,
		"user without country":          g.Exec("INSERT INTO users (id, country_id, email, name) VALUES (91, 99, 'x@example.com', 'x')").Error,
		"second profile (1:1)":          g.Exec("INSERT INTO profiles (id, user_id) VALUES (90, 1)").Error,
		"profile of a ghost user":       g.Exec("INSERT INTO profiles (id, user_id) VALUES (91, 99)").Error,
		"duplicate pivot row":           g.Exec("INSERT INTO role_user (user_id, role_id) VALUES (1, 1)").Error,
		"pivot to a ghost role":         g.Exec("INSERT INTO role_user (user_id, role_id) VALUES (1, 99)").Error,
		"duplicate post slug":           g.Exec("INSERT INTO posts (id, user_id, slug, title, published) VALUES (90, 2, 'hello-db', 'other', TRUE)").Error,
		"composite unique (user,title)": g.Exec("INSERT INTO posts (id, user_id, slug, title, published) VALUES (91, 1, 'new-slug', 'Hello DB', TRUE)").Error,
		"post with ghost category":      g.Exec("INSERT INTO posts (id, user_id, category_id, slug, title, published) VALUES (92, 1, 99, 's', 't', TRUE)").Error,
		"duplicate order line":          g.Exec("INSERT INTO order_items (order_id, line_no, product_id, quantity, unit_price) VALUES (1, 1, 2, 1, 1)").Error,
		"order line of ghost order":     g.Exec("INSERT INTO order_items (order_id, line_no, product_id, quantity, unit_price) VALUES (99, 1, 1, 1, 1)").Error,
		"duplicate taggable":            g.Exec("INSERT INTO taggables (tag_id, taggable_id, taggable_type) VALUES (1, 1, 'post')").Error,
		"null in NOT NULL column":       g.Exec("INSERT INTO products (id, sku, name, price) VALUES (90, 'SKU-N', NULL, 1)").Error,
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) { mustFail(t, name, err) })
	}

	// the same title is fine for another user, and the same morph id for another type
	mustExec(t, db, "INSERT INTO posts (id, user_id, slug, title, published) VALUES (93, 2, 'hello-again', 'Hello DB', TRUE)")
	mustExec(t, db, "INSERT INTO taggables (tag_id, taggable_id, taggable_type) VALUES (1, 1, 'video')")

	// defaults filled in by the schema
	mustExec(t, db, "INSERT INTO products (id, sku, name, price) VALUES (91, 'SKU-D', 'Defaults', 5)")
	var stock int
	g.Table("products").Select("stock").Where("id = 91").Scan(&stock)
	if stock != 0 {
		t.Errorf("stock default: %d", stock)
	}
	mustExec(t, db, "INSERT INTO orders (id, user_id, total) VALUES (91, 1, 1)")
	var status string
	g.Table("orders").Select("status").Where("id = 91").Scan(&status)
	if status != "pending" {
		t.Errorf("status default: %q", status)
	}
}

func TestReferentialActions(t *testing.T) {
	db := seeded(t)

	t.Run("RESTRICT blocks deleting a referenced parent", func(t *testing.T) {
		mustFail(t, "delete country in use", db.Gorm.Exec("DELETE FROM countries WHERE id = 1").Error)
		mustFail(t, "delete user with orders", db.Gorm.Exec("DELETE FROM users WHERE id = 1").Error)
		mustFail(t, "delete product sold", db.Gorm.Exec("DELETE FROM products WHERE id = 1").Error)
		if count(t, db, "users", "id = 1") != 1 {
			t.Fatal("restricted delete must leave the row")
		}
	})

	t.Run("SET NULL detaches posts from a deleted category", func(t *testing.T) {
		mustExec(t, db, "DELETE FROM categories WHERE id = 3") // leaf: post 2 lives here
		if n := count(t, db, "posts", "id = 2 AND category_id IS NULL"); n != 1 {
			t.Fatalf("post 2 should survive with a NULL category, got %d", n)
		}
	})

	t.Run("CASCADE walks a self-referencing tree", func(t *testing.T) {
		mustExec(t, db, "DELETE FROM categories WHERE id = 1") // Tech -> Databases
		if n := count(t, db, "categories"); n != 0 {
			t.Fatalf("whole tree should be gone, %d left", n)
		}
	})

	t.Run("CASCADE through several levels", func(t *testing.T) {
		mustExec(t, db, "DELETE FROM orders WHERE id = 2") // no restricted children
		mustExec(t, db, "DELETE FROM orders WHERE id = 1")
		if n := count(t, db, "order_items"); n != 0 {
			t.Fatalf("order_items should cascade, %d left", n)
		}
		mustExec(t, db, "DELETE FROM users WHERE id = 1")
		for table, where := range map[string]string{
			"profiles": "user_id = 1", "role_user": "user_id = 1", "posts": "user_id = 1", "comments": "user_id = 1",
		} {
			if n := count(t, db, table, where); n != 0 {
				t.Errorf("%s of the deleted user should cascade, %d left", table, n)
			}
		}
		if count(t, db, "users") != 2 {
			t.Fatal("only one user should be removed")
		}
	})
}

func TestSeedsLoadExpectedData(t *testing.T) {
	db := seeded(t)
	want := map[string]int64{
		"countries": 3, "roles": 3, "tags": 4, "users": 3, "profiles": 2, "role_user": 4,
		"categories": 3, "posts": 3, "videos": 1, "comments": 4, "taggables": 4,
		"products": 2, "orders": 2, "order_items": 3,
	}
	for table, n := range want {
		if got := count(t, db, table); got != n {
			t.Errorf("%s: %d rows, want %d", table, got, n)
		}
	}
	// a ';' inside a quoted string or a comment must not split the statement
	var bio string
	db.Gorm.Table("profiles").Select("bio").Where("id = 1").Scan(&bio)
	if bio != "Writes; a lot" {
		t.Errorf("quoted semicolon survived as %q", bio)
	}
	// money round-trips exactly
	var total float64
	db.Gorm.Table("order_items").Select("SUM(quantity * unit_price)").Where("order_id = 1").Scan(&total)
	if total != 119.30 {
		t.Errorf("order 1 lines sum to %v", total)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func names[T any](rows []T, name func(T) string) string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = name(r)
	}
	return strings.Join(out, ",")
}

func TestEagerRelationsOnTheSeededDatabase(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)

	t.Run("users: belongsTo, hasOne, hasMany, belongsToMany", func(t *testing.T) {
		users := must(orm.New[User](db).Query().
			With("Country").With("Profile").With("Roles").With("Orders").OrderBy("id", "asc").Get(ctx))
		ana, bia, caio := users[0], users[1], users[2]
		if ana.Country.Value().Code != "BR" || caio.Country.Value().Code != "PT" {
			t.Errorf("countries: %+v %+v", ana.Country.Value(), caio.Country.Value())
		}
		if p := ana.Profile.Value(); p == nil || p.Bio == nil || *p.Bio != "Writes; a lot" {
			t.Errorf("ana's profile: %+v", p)
		}
		if p := bia.Profile.Value(); p == nil || p.Bio != nil {
			t.Errorf("bia's profile has a NULL bio: %+v", p)
		}
		if caio.Profile.Value() != nil || !caio.Profile.Loaded() {
			t.Errorf("caio has no profile")
		}
		if len(ana.Roles.Items()) != 2 || len(bia.Roles.Items()) != 1 || caio.Roles.Items()[0].Name != "reader" {
			t.Errorf("roles: %d %d", len(ana.Roles.Items()), len(bia.Roles.Items()))
		}
		if len(ana.Orders.Items()) != 1 || len(bia.Orders.Items()) != 1 || len(caio.Orders.Items()) != 0 {
			t.Errorf("orders: %d %d %d", len(ana.Orders.Items()), len(bia.Orders.Items()), len(caio.Orders.Items()))
		}
	})

	t.Run("nested through a composite-key model", func(t *testing.T) {
		orders := must(orm.New[Order](db).Query().With("Items.Product").With("User").OrderBy("id", "asc").Get(ctx))
		if len(orders) != 2 || len(orders[0].Items.Items()) != 2 || orders[0].User.Value().Name != "Ana" {
			t.Fatalf("orders: %+v", orders)
		}
		var sum float64
		for _, it := range orders[0].Items.Items() {
			sum += float64(it.Quantity) * it.UnitPrice
			if it.Product.Value() == nil || it.Product.Value().SKU == "" {
				t.Errorf("line %d lost its product", it.LineNo)
			}
		}
		if sum != 119.30 {
			t.Errorf("lines sum to %v", sum)
		}
		item := &orders[0].Items.Items()[1]                       // a pointer: relations load into the element itself, not into a copy
		if o := must(item.Order.Get(ctx)); o.ID != orders[0].ID { // lazy belongsTo from a composite-key owner
			t.Errorf("item.Order: %+v", o)
		}
	})

	t.Run("self-referencing tree, both directions", func(t *testing.T) {
		root := must(orm.New[Category](db).Query().Where("parent_id IS NULL").With("Children.Children").First(ctx))
		if root.Name != "Tech" || len(root.Children.Items()) != 1 ||
			root.Children.Items()[0].Name != "Databases" || root.Children.Items()[0].Children.Items()[0].Name != "Migrations" {
			t.Fatalf("down the tree: %+v", root)
		}
		leaf := must(orm.New[Category](db).Query().Where("name = ?", "Migrations").With("Parent.Parent").First(ctx))
		if leaf.Parent.Value().Name != "Databases" || leaf.Parent.Value().Parent.Value().Name != "Tech" ||
			leaf.Parent.Value().Parent.Value().Parent.Value() != nil {
			t.Fatalf("up the tree: %+v", leaf.Parent.Value())
		}
	})

	t.Run("hasManyThrough: country -> users -> posts", func(t *testing.T) {
		cs := must(orm.New[Country](db).Query().With("Posts").With("Users").OrderBy("id", "asc").Get(ctx))
		if len(cs[0].Posts.Items()) != 3 || len(cs[1].Posts.Items()) != 0 || len(cs[2].Posts.Items()) != 0 || len(cs[0].Users.Items()) != 2 {
			t.Fatalf("through: %d %d %d", len(cs[0].Posts.Items()), len(cs[1].Posts.Items()), len(cs[2].Posts.Items()))
		}
	})

	t.Run("polymorphic: morphTo, morphMany", func(t *testing.T) {
		comments := must(orm.New[Comment](db).Query().With("Commentable").With("User").OrderBy("id", "asc").Get(ctx))
		if p, isPost := comments[0].Commentable.Value().(*Post); !isPost || p.Slug != "hello-db" || !p.Exists() {
			t.Errorf("comment 1 -> %+v", comments[0].Commentable.Value())
		}
		if v, isVideo := comments[2].Commentable.Value().(*Video); !isVideo || v.Title != "Intro to indexes" {
			t.Errorf("comment 3 -> %+v", comments[2].Commentable.Value())
		}
		// post 3 and video 1 share ids with other types: the type column keeps them apart
		if p, isPost := comments[3].Commentable.Value().(*Post); !isPost || p.Slug != "draft" {
			t.Errorf("comment 4 -> %+v", comments[3].Commentable.Value())
		}
		if comments[1].User.Value().Name != "Caio" {
			t.Errorf("comment author: %+v", comments[1].User.Value())
		}
		posts := must(orm.New[Post](db).Query().With("Comments").OrderBy("id", "asc").Get(ctx))
		if len(posts[0].Comments.Items()) != 2 || len(posts[1].Comments.Items()) != 0 || len(posts[2].Comments.Items()) != 1 {
			t.Errorf("post comments: %d %d %d", len(posts[0].Comments.Items()), len(posts[1].Comments.Items()), len(posts[2].Comments.Items()))
		}
	})

	t.Run("polymorphic many-to-many from both sides", func(t *testing.T) {
		posts := must(orm.New[Post](db).Query().With("Tags").OrderBy("id", "asc").Get(ctx))
		if len(posts[0].Tags.Items()) != 2 || len(posts[1].Tags.Items()) != 1 || len(posts[2].Tags.Items()) != 0 {
			t.Errorf("post tags: %d %d %d", len(posts[0].Tags.Items()), len(posts[1].Tags.Items()), len(posts[2].Tags.Items()))
		}
		video := must(orm.New[Video](db).Query().With("Tags").First(ctx))
		if len(video.Tags.Items()) != 1 || video.Tags.Items()[0].Name != "sql" {
			t.Errorf("video tags: %+v", video.Tags.Items())
		}
		tags := must(orm.New[Tag](db).Query().With("Posts").With("Videos").OrderBy("id", "asc").Get(ctx))
		got := names(tags, func(tg Tag) string {
			return tg.Name + ":" + itoa(len(tg.Posts.Items())) + "/" + itoa(len(tg.Videos.Items()))
		})
		if got != "go:2/0,sql:1/1,orm:0/0,unused:0/0" {
			t.Errorf("tags: %s", got)
		}
	})

	t.Run("withCount and existence queries", func(t *testing.T) {
		users := must(orm.New[User](db).Query().WithCount("Posts", "Roles", "Comments").OrderBy("id", "asc").Get(ctx))
		counts := names(users, func(u User) string {
			return itoa(int(u.Extra("posts_count").(int64))) + itoa(int(u.Extra("roles_count").(int64))) + itoa(int(u.Extra("comments_count").(int64)))
		})
		if counts != "221,111,012" { // posts, roles, comments authored — per user
			t.Errorf("counts: %s", counts)
		}
		who := func(q *orm.Query[User]) string {
			return names(must(q.OrderBy("id", "asc").Get(ctx)), func(u User) string { return u.Name })
		}
		published := func(q *gorm.DB) *gorm.DB { return q.Where("published = ?", true) }
		for name, c := range map[string]struct {
			got, want string
		}{
			"has orders":            {who(orm.New[User](db).Query().Has("Orders")), "Ana,Bia"},
			"doesn't have orders":   {who(orm.New[User](db).Query().DoesntHave("Orders")), "Caio"},
			"has published posts":   {who(orm.New[User](db).Query().WhereHas("Posts", published)), "Ana"},
			"no published posts":    {who(orm.New[User](db).Query().WhereDoesntHave("Posts", published)), "Bia,Caio"},
			"has admin role":        {who(orm.New[User](db).Query().WhereHas("Roles", func(q *gorm.DB) *gorm.DB { return q.Where("name = ?", "admin") })), "Ana"},
			"has profile and posts": {who(orm.New[User](db).Query().Has("Profile").Has("Posts")), "Ana,Bia"},
			"combined with a where": {who(orm.New[User](db).Query().Has("Posts").Where("name <> ?", "Ana")), "Bia"},
		} {
			if c.got != c.want {
				t.Errorf("%s: got %q, want %q", name, c.got, c.want)
			}
		}
		parents := must(orm.New[Category](db).Query().Has("Children").OrderBy("id", "asc").Get(ctx))
		if names(parents, func(c Category) string { return c.Name }) != "Tech,Databases" {
			t.Errorf("categories with children: %+v", parents)
		}
		tagged := must(orm.New[Post](db).Query().WhereHas("Tags", func(q *gorm.DB) *gorm.DB { return q.Where("name = ?", "sql") }).Get(ctx))
		if len(tagged) != 1 || tagged[0].Slug != "hello-db" {
			t.Errorf("posts tagged sql: %+v", tagged)
		}
		countriesWithPosts := must(orm.New[Country](db).Query().Has("Posts").Get(ctx))
		if len(countriesWithPosts) != 1 || countriesWithPosts[0].Code != "BR" {
			t.Errorf("countries with posts: %+v", countriesWithPosts)
		}
	})

	t.Run("soft deletes keep the model API consistent", func(t *testing.T) {
		users := orm.New[User](db)
		caio := must(users.Query().Where("name = ?", "Caio").First(ctx))
		ok(t, caio.Delete(ctx))
		if !caio.Trashed() || must(users.Query().Count(ctx)) != 2 || must(users.Query().OnlyTrashed().Count(ctx)) != 1 {
			t.Errorf("soft delete")
		}
		ok(t, caio.Restore(ctx))
		if caio.Trashed() || must(users.Query().Count(ctx)) != 3 {
			t.Errorf("restore")
		}
	})

	t.Run("serialization", func(t *testing.T) {
		ana := must(orm.New[User](db).Query().Where("name = ?", "Ana").With("Posts.Tags").With("Roles").First(ctx))
		m, err := orm.ToMap(ana)
		if err != nil {
			t.Fatal(err)
		}
		posts, _ := m["posts"].([]any)
		roles, _ := m["roles"].([]any)
		if m["email"] != "ana@example.com" || len(posts) != 2 || len(roles) != 2 {
			t.Fatalf("map: %v", m)
		}
		if tags, _ := posts[0].(map[string]any)["tags"].([]any); len(tags) != 2 {
			t.Errorf("nested tags: %v", posts[0])
		}
		if _, loaded := m["orders"]; loaded {
			t.Error("relations that were not loaded must not be serialized")
		}
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestRelationWritesRespectConstraints(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	ana := must(orm.New[User](db).Find(ctx, 1))
	post := must(orm.New[Post](db).Find(ctx, 1))

	// the composite unique (user_id, title) still guards rows created through a relation
	if _, err := ana.Posts.Create(ctx, map[string]any{"slug": "dup", "title": "Hello DB"}); err == nil {
		t.Fatal("duplicate (user, title) must be rejected")
	}
	if n := count(t, db, "posts", "slug = 'dup'"); n != 0 {
		t.Fatalf("a failed create left %d rows", n)
	}
	// mass assignment is enforced on User, not just on the columns
	if _, err := orm.New[User](db).CreateFrom(ctx, map[string]any{"email": "x@example.com", "name": "x", "id": 99, "country_id": 1}); migro.CodeOf(err) != migro.MassAssignment {
		t.Fatalf("id is not fillable: %v", err)
	}
	// belongsToMany / morphToMany: the pivot foreign keys protect the links
	mustFail(t, "attach unknown tag", post.Tags.Attach(ctx, 99))
	mustFail(t, "attach unknown role", ana.Roles.Attach(ctx, 99))

	// Sync only touches this post's rows, never the video's
	res := must(post.Tags.Sync(ctx, 3, 4))
	if len(res.Detached) != 2 || len(res.Attached) != 2 {
		t.Fatalf("sync: %+v", res)
	}
	if n := count(t, db, "taggables", "taggable_type = 'video'"); n != 1 {
		t.Fatalf("video links: %d", n)
	}
	// ...and from the inverse side
	orm1 := must(orm.New[Tag](db).Find(ctx, 3))
	ok(t, orm1.Posts.Attach(ctx, must(orm.New[Post](db).Find(ctx, 2))))
	if n, _ := must(post.Tags.Query()).Count(ctx); n != 2 {
		t.Fatalf("post 1 tags: %d", n)
	}
	// a failed multi-step write rolls back as a unit
	err := db.Transaction(ctx, func(tx *migro.DB) error {
		if _, err := orm.New[Tag](tx).CreateFrom(ctx, map[string]any{"name": "temp"}); err != nil {
			return err
		}
		_, err := orm.New[Tag](tx).CreateFrom(ctx, map[string]any{"name": "go"}) // unique violation
		return err
	})
	mustFail(t, "duplicate tag in tx", err)
	if n := count(t, db, "tags", "name = 'temp'"); n != 0 {
		t.Fatalf("transaction leaked %d rows", n)
	}
	// deleting through the model API still honours cascades and restrictions
	mustFail(t, "delete country in use", must(orm.New[Country](db).Find(ctx, 1)).ForceDelete(ctx))
	bia := must(orm.New[User](db).Find(ctx, 2))
	mustFail(t, "force delete user with orders", bia.ForceDelete(ctx))
}

func TestSeedRunnerOnceAndEnvironments(t *testing.T) {
	ctx := context.Background()
	db, _ := migrated(t)

	r := seedRunner(t, db, "testing", seed.Once())
	first, err := r.RunOnly(ctx, "DatabaseSeeder")
	if err != nil || len(first.Ran) != 1 || first.Ran[0] != "DatabaseSeeder" {
		t.Fatalf("first run: %+v %v", first, err)
	}
	again, err := r.RunOnly(ctx, "DatabaseSeeder")
	if err != nil || len(again.Ran) != 0 || len(again.Skipped) != 1 {
		t.Fatalf("Once must skip executed seeders: %+v %v", again, err)
	}
	if count(t, db, "users") != 3 {
		t.Fatal("second run duplicated data")
	}
	if _, err := r.RunOnly(ctx, "GhostSeeder"); migro.CodeOf(err) != migro.SeedFailed {
		t.Fatalf("unknown class: %v", err)
	}

	// production skips seeders without an explicit environment
	res, err := seedRunner(t, db, "production").RunOnly(ctx, "DatabaseSeeder")
	if err != nil || len(res.Ran) != 0 || len(res.Skipped) != 1 {
		t.Fatalf("production: %+v %v", res, err)
	}
}

func TestFactorySeederAddsGeneratedRowsOnTopOfDatabaseSeeder(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)

	if _, err := seedRunner(t, db, "testing").RunOnly(ctx, "FactorySeeder"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, "users"); n != 6 {
		t.Errorf("users: %d, want 3 seeded + 3 generated", n)
	}
	if n := count(t, db, "posts"); n != 9 {
		t.Errorf("posts: %d, want 3 seeded + 2 per generated user", n)
	}
	// every generated post points at a user that exists
	var orphans int64
	db.Gorm.Raw("SELECT COUNT(*) FROM posts p LEFT JOIN users u ON u.id = p.user_id WHERE u.id IS NULL").Scan(&orphans)
	if orphans != 0 {
		t.Errorf("%d orphan posts", orphans)
	}
	// same factory seed -> same data, so a second database is identical
	other := seeded(t)
	if _, err := seedRunner(t, other, "testing").RunOnly(ctx, "FactorySeeder"); err != nil {
		t.Fatal(err)
	}
	var a, b []string
	db.Gorm.Table("users").Where("id > 3").Order("id").Pluck("name", &a)
	other.Gorm.Table("users").Where("id > 3").Order("id").Pluck("name", &b)
	if strings.Join(a, ",") != strings.Join(b, ",") || len(a) != 3 {
		t.Errorf("factory not reproducible: %v vs %v", a, b)
	}
}

func TestFactoriesBuildLinkedGraphs(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)

	// For("country") creates a country per user and links it; With("posts") hangs posts off each user
	users := must(factories.UserFactory().Seed(7).For("country").With("posts", 2).CreateMany(ctx, db, 2))
	if count(t, db, "countries") != 5 || count(t, db, "posts") != 7 {
		t.Fatalf("countries=%d posts=%d", count(t, db, "countries"), count(t, db, "posts"))
	}
	if users[0].CountryID == users[1].CountryID || users[0].CountryID < 4 {
		t.Fatalf("each user gets a new country: %d %d", users[0].CountryID, users[1].CountryID)
	}
	// the returned models are bound: relations and the instance API work straight away
	posts := must(users[0].Posts.Get(ctx))
	country := must(users[0].Country.Get(ctx))
	if len(posts) != 2 || country.ID != users[0].CountryID {
		t.Fatalf("relations on a factory model: posts=%d country=%+v", len(posts), country)
	}
	users[0].Name = "Renamed"
	ok(t, users[0].Save(ctx))
	if must(orm.New[User](db).Find(ctx, users[0].ID)).Name != "Renamed" {
		t.Fatal("Save on a factory model")
	}
	// Make never touches the database
	made := must(factories.UserFactory().Make())
	if made.Exists() || count(t, db, "users") != 5 {
		t.Fatalf("Make persisted: users=%d", count(t, db, "users"))
	}
}

func TestFailingSeederRollsBackItself(t *testing.T) {
	ctx := context.Background()
	db, _ := migrated(t)

	r := seedRunner(t, db, "testing")
	if _, err := r.RunOnly(ctx, "ReferenceDataSeeder"); err != nil {
		t.Fatal(err)
	}
	if err := db.Gorm.Exec("DELETE FROM roles").Error; err != nil { // UserSeeder's pivot rows will now violate role_user -> roles
		t.Fatal(err)
	}
	// users and profiles are inserted before the pivot rows fail: all of it must be undone
	res, err := r.RunOnly(ctx, "UserSeeder")
	if migro.CodeOf(err) != migro.SeedFailed {
		t.Fatalf("want SEED_FAILED, got %v", err)
	}
	if len(res.Ran) != 0 || count(t, db, "users") != 0 || count(t, db, "profiles") != 0 {
		t.Fatalf("partial seed leaked: ran=%v users=%d", res.Ran, count(t, db, "users"))
	}
}

func TestPretendRendersEveryMigrationWithoutTouchingTheDatabase(t *testing.T) {
	ctx := context.Background()
	db := open(t)
	m := migrator(t, db)

	res, err := m.Run(ctx, migrate.RunOptions{Pretend: true})
	if err != nil || len(res.Names) != 16 {
		t.Fatalf("pretend: %+v %v", res, err)
	}
	var all []string
	for _, name := range res.Names {
		all = append(all, res.SQL[name]...)
	}
	sql := strings.NewReplacer(`"`, "", "`", "", "[", "", "]", "").Replace(strings.Join(all, "\n"))
	for _, want := range []string{
		"CREATE TABLE countries", "CREATE TABLE order_items", "CREATE INDEX comments_commentable_type_commentable_id_index",
		"CREATE INDEX orders_status_placed_at_index", "FOREIGN KEY (parent_id) REFERENCES categories (id) ON DELETE CASCADE",
		"PRIMARY KEY (tag_id, taggable_id, taggable_type)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("pretend output lacks %q", want)
		}
	}
	for _, tbl := range allTables {
		if db.Gorm.Migrator().HasTable(tbl) {
			t.Errorf("pretend created %s", tbl)
		}
	}
}

func TestRollbackUnwindsInForeignKeyOrder(t *testing.T) {
	ctx := context.Background()
	db := seeded(t) // data present: down migrations must cope with populated tables
	m := migrator(t, db)

	rb, err := m.RollbackSteps(ctx, 5)
	if err != nil {
		t.Fatalf("rollback with data present: %v", err)
	}
	want := []string{"add_reporting_indexes", "add_phone_to_users_table", "create_order_items_table", "create_orders_table", "create_products_table"}
	if len(rb.Names) != len(want) {
		t.Fatalf("rolled back: %v", rb.Names)
	}
	for i, suffix := range want {
		if !strings.HasSuffix(rb.Names[i], suffix) {
			t.Fatalf("rollback must run newest first: %v", rb.Names)
		}
	}
	for _, tbl := range []string{"order_items", "orders", "products"} {
		if db.Gorm.Migrator().HasTable(tbl) {
			t.Errorf("%s should be gone", tbl)
		}
	}
	for _, tbl := range []string{"users", "posts", "comments", "taggables"} {
		if !db.Gorm.Migrator().HasTable(tbl) {
			t.Errorf("%s should survive a partial rollback", tbl)
		}
	}
	if ok, _ := schema.New(db).HasColumn(ctx, "users", "phone"); ok {
		t.Error("users.phone should be dropped")
	}
	if db.Gorm.Migrator().HasIndex("comments", "comments_user_id_index") {
		t.Error("the reporting index should be dropped")
	}
	if count(t, db, "users") != 3 {
		t.Error("rolling back must not touch unrelated data")
	}
}

func TestResetAndFreshRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, m := migrated(t)

	// every migration must be reversible: after Reset only the bookkeeping tables remain
	res, err := m.Reset(ctx)
	if err != nil || len(res.Names) != 16 {
		t.Fatalf("reset: %+v %v", res, err)
	}
	tables, err := db.Gorm.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	for _, tbl := range tables {
		if !strings.HasPrefix(tbl, "migro_") && tbl != "sqlite_sequence" {
			t.Errorf("table %s survived Reset", tbl)
		}
	}

	// migrate -> seed -> fresh -> migrate -> seed lands in the same state
	r := seedRunner(t, db, "testing")
	snapshot := func() map[string]int64 {
		out := map[string]int64{}
		for _, tbl := range allTables {
			out[tbl] = count(t, db, tbl)
		}
		return out
	}
	if _, err := m.Latest(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunOnly(ctx, "DatabaseSeeder"); err != nil {
		t.Fatal(err)
	}
	before := snapshot()

	if _, err := m.Fresh(ctx); err != nil {
		t.Fatalf("fresh: %v", err)
	}
	for _, tbl := range allTables {
		if n := count(t, db, tbl); n != 0 {
			t.Errorf("%s has %d rows after fresh", tbl, n)
		}
	}
	if _, err := r.RunOnly(ctx, "DatabaseSeeder"); err != nil {
		t.Fatalf("reseed after fresh: %v", err)
	}
	for tbl, n := range snapshot() {
		if before[tbl] != n {
			t.Errorf("%s: %d rows after rebuild, %d before", tbl, n, before[tbl])
		}
	}
}

func TestMigrationIsAtomicWhenAStatementFails(t *testing.T) {
	ctx := context.Background()
	db, m := migrated(t)

	broken := migrate.Migration{
		Name: "20260301000001_half_done",
		Up: func(ctx context.Context, s *schema.Builder) error {
			if err := s.CreateTable(ctx, "half_done", func(t *schema.Table) { t.Increments("id") }); err != nil {
				return err
			}
			return s.Raw(ctx, "CREATE INDEX nope ON missing_table (x)")
		},
		Down: func(ctx context.Context, s *schema.Builder) error { return s.DropTableIfExists(ctx, "half_done") },
	}
	if err := m.Add(broken); err != nil {
		t.Fatal(err)
	}
	res, err := m.Latest(ctx)
	if err == nil {
		t.Fatalf("broken migration should fail: %+v", res)
	}
	if migro.CodeOf(err) != migro.MigrationFailed {
		t.Fatalf("want MIGRATION_FAILED, got %v", err)
	}
	if db.Gorm.Migrator().HasTable("half_done") {
		t.Fatal("transactional DDL should have rolled the table back")
	}
	st, _ := m.Status(ctx)
	if last := st[len(st)-1]; last.Applied {
		t.Fatalf("failed migration must not be recorded: %+v", last)
	}
	// the earlier schema is untouched
	if !db.Gorm.Migrator().HasTable("order_items") {
		t.Fatal("previous migrations must survive")
	}
}
