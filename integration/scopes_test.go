package integration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LouisRiverstone/migro"
	. "github.com/LouisRiverstone/migro/integration/models"
	"github.com/LouisRiverstone/migro/orm"
)

func TestLocalScopesComposeThroughApply(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	posts := orm.New[Post](db)

	published, err := posts.Query().Apply(PublishedPosts).Get(ctx)
	ok(t, err)
	if int64(len(published)) != count(t, db, "posts", "published = ?", true) || len(published) != 2 {
		t.Fatalf("published scope returned %d posts", len(published))
	}

	// scopes stack, take arguments and mix with plain query methods
	got, err := posts.Query().Apply(PublishedPosts, PostsBy(1), TitleContains("Hello")).Latest().Get(ctx)
	ok(t, err)
	if len(got) != 1 || got[0].Slug != "hello-db" {
		t.Fatalf("stacked scopes: %+v", got)
	}

	none, err := posts.Query().Apply(PublishedPosts, PostsBy(2)).Get(ctx)
	ok(t, err)
	if len(none) != 0 {
		t.Fatalf("bia has no published post: %+v", none)
	}

	// scopes also work on the query a relation hands out
	q, err := must(posts.Find(ctx, 1)).User.Query()
	ok(t, err)
	if n, _ := q.Count(ctx); n != 1 {
		t.Fatalf("user query: %d", n)
	}
	ana := must(orm.New[User](db).Find(ctx, 1))
	pq, err := ana.Posts.Query()
	ok(t, err)
	if n, err := pq.Apply(PublishedPosts).Count(ctx); err != nil || n != 2 {
		t.Fatalf("relation query + scope: %d %v", n, err)
	}
}

func TestGlobalScopeFiltersEveryEntryPoint(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	posts := orm.New[Post](db)
	mustExec(t, db, "UPDATE posts SET archived = ? WHERE slug = ?", true, "draft")

	// every read path skips the archived post
	if n, _ := posts.Query().Count(ctx); n != 2 {
		t.Errorf("Count: %d", n)
	}
	all, err := posts.All(ctx)
	ok(t, err)
	if len(all) != 2 {
		t.Errorf("All: %d", len(all))
	}
	if _, err := posts.Find(ctx, 3); !errors.Is(err, migro.ErrNotFound) {
		t.Errorf("Find of an archived post: %v", err)
	}
	many, err := posts.FindMany(ctx, 1, 2, 3)
	ok(t, err)
	if len(many) != 2 {
		t.Errorf("FindMany: %d", len(many))
	}
	if exists, _ := posts.Query().Where("slug = ?", "draft").Exists(ctx); exists {
		t.Error("Exists sees the archived post")
	}
	page, err := posts.Query().Paginate(ctx, 1, 10)
	ok(t, err)
	if page.Total != 2 {
		t.Errorf("Paginate total: %d", page.Total)
	}
	seen := 0
	ok(t, posts.Query().Chunk(ctx, 1, func(rows []Post) error { seen += len(rows); return nil }))
	if seen != 2 {
		t.Errorf("Chunk saw %d posts", seen)
	}

	// relations apply the related model's global scope, lazily and eagerly
	bia := must(orm.New[User](db).Find(ctx, 2))
	lazy, err := bia.Posts.Get(ctx)
	ok(t, err)
	if len(lazy) != 0 {
		t.Errorf("lazy relation: %d", len(lazy))
	}
	users, err := orm.New[User](db).Query().With("Posts").Get(ctx)
	ok(t, err)
	total := 0
	for _, u := range users {
		total += len(u.Posts.Items())
	}
	if total != 2 {
		t.Errorf("eager relation loaded %d posts", total)
	}

	// writes obey it too: the archived row is out of reach
	n, err := posts.Query().Where("slug = ?", "draft").Update(ctx, map[string]any{"title": "x"})
	ok(t, err)
	if n != 0 {
		t.Errorf("update reached %d archived rows", n)
	}
}

func TestWithoutGlobalScopeBringsRowsBack(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	posts := orm.New[Post](db)
	mustExec(t, db, "UPDATE posts SET archived = ? WHERE slug = ?", true, "draft")

	for name, q := range map[string]*orm.Query[Post]{
		"by name": posts.Query().WithoutGlobalScope("not_archived"),
		"all":     posts.Query().WithoutGlobalScopes(),
	} {
		if n, _ := q.Count(ctx); n != 3 {
			t.Errorf("%s: Count %d, want 3", name, n)
		}
	}
	// an unrelated name leaves the scope on
	if n, _ := posts.Query().WithoutGlobalScope("other").Count(ctx); n != 2 {
		t.Errorf("unknown scope name switched something off: %d", n)
	}
	// switching off after other clauses still works: the scope is resolved when the query runs
	q := posts.Query().Where("slug = ?", "draft")
	q.WithoutGlobalScopes()
	row, err := q.First(ctx)
	ok(t, err)
	if !row.Archived {
		t.Error("expected the archived draft")
	}
}
