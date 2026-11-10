package integration_test

import (
	"context"
	"testing"

	"github.com/LouisRiverstone/migro"
	. "github.com/LouisRiverstone/migro/integration/models"
	"github.com/LouisRiverstone/migro/orm"
)

func TestQueriesAndRelationsReturnCollections(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	users := orm.New[User](db)

	var col orm.Collection[User] = must(users.Query().OrderBy("id", "asc").Get(ctx)) // no conversion needed
	var plain []User = col                                                           // and back to []T
	if col.Count() != 3 || len(plain) != 3 || col.IsEmpty() {
		t.Fatalf("count %d", col.Count())
	}
	if col.First().Name != "Ana" || col.Last().Name != "Caio" {
		t.Errorf("First/Last: %s %s", col.First().Name, col.Last().Name)
	}
	if (orm.Collection[User]{}).First() != nil {
		t.Error("First of an empty collection is nil")
	}

	if _, ok := any(must(users.All(ctx))).(orm.Collection[User]); !ok {
		t.Error("All")
	}
	if _, ok := any(must(users.FindMany(ctx, 1, 2))).(orm.Collection[User]); !ok {
		t.Error("FindMany")
	}
	ana := &col[0]
	posts, err := ana.Posts.Get(ctx)
	ok(t, err)
	if _, isCol := any(posts).(orm.Collection[Post]); !isCol || ana.Posts.Items().Count() != 2 {
		t.Error("relations hand out collections")
	}
	pg := must(users.Query().Paginate(ctx, 1, 2))
	if pg.Data.Count() != 2 {
		t.Error("Page.Data is a collection")
	}
}

func TestCollectionFilteringAndReducing(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	posts := must(orm.New[Post](db).Query().OrderBy("id", "asc").Get(ctx))

	pub := posts.Filter(func(p *Post) bool { return p.Published })
	if pub.Count() != 2 || posts.Reject(func(p *Post) bool { return p.Published }).Count() != 1 {
		t.Errorf("Filter/Reject: %d", pub.Count())
	}

	titles := orm.MapTo(posts, func(p *Post) string { return p.Slug })
	if len(titles) != 3 || titles[0] != "hello-db" {
		t.Errorf("MapTo: %v", titles)
	}
	total := orm.Reduce(posts, 0, func(acc int, p *Post) int { return acc + int(p.UserID) })
	if total != 1+1+2 {
		t.Errorf("Reduce: %d", total)
	}

	seen := 0
	ok(t, posts.Each(func(i int, p *Post) error { seen += i; return nil }))
	if seen != 0+1+2 {
		t.Errorf("Each indexes: %d", seen)
	}
	stop := posts.Each(func(i int, p *Post) error {
		if i == 1 {
			return migro.Newf(migro.QueryFailed, "stop")
		}
		return nil
	})
	if stop == nil {
		t.Error("Each must stop on the first error")
	}
	// the filtered copies stay bound: they can be edited and saved on their own
	pub[0].Title = "Edited"
	ok(t, pub[0].Save(ctx))
	if got := rawColumn(t, db, "posts", "title", pub[0].ID); got != "Edited" {
		t.Errorf("title = %q", got)
	}
	if posts[0].Title == "Edited" {
		t.Error("a filtered copy must not alias the original")
	}
}

func TestCollectionColumnHelpers(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	posts := must(orm.New[Post](db).Query().OrderBy("id", "asc").Get(ctx))

	slugs, err := orm.PluckAs[Post, string](posts, "slug")
	ok(t, err)
	if len(slugs) != 3 || slugs[2] != "draft" {
		t.Errorf("PluckAs: %v", slugs)
	}
	if _, err := orm.PluckAs[Post, int](posts, "slug"); migro.CodeOf(err) != migro.CastFailed {
		t.Errorf("wrong type: %v", err)
	}
	if _, err := posts.Pluck("nope"); migro.CodeOf(err) != migro.InvalidSchema {
		t.Errorf("unknown column: %v", err)
	}
	keys, err := posts.ModelKeys()
	ok(t, err)
	if len(keys) != 3 || keys[0] != uint(1) {
		t.Errorf("ModelKeys: %v", keys)
	}

	byID, err := posts.KeyBy("id")
	ok(t, err)
	if byID[uint(2)].Slug != "rolling-back" {
		t.Errorf("KeyBy: %v", byID)
	}
	// pointer columns key by their value and NULLs are skipped
	byCat, err := posts.KeyBy("category_id")
	ok(t, err)
	if len(byCat) != 3 {
		t.Errorf("category keys: %d", len(byCat))
	}

	groups, err := posts.GroupBy("user_id")
	ok(t, err)
	if groups[uint(1)].Count() != 2 || groups[uint(2)].Count() != 1 {
		t.Errorf("GroupBy: %v", groups)
	}
	uniq, err := posts.Unique("user_id")
	ok(t, err)
	if uniq.Count() != 2 || uniq[0].Slug != "hello-db" {
		t.Errorf("Unique keeps the first of each: %v", uniq)
	}

	p, err := posts.Find(2)
	ok(t, err)
	if p == nil || p.Slug != "rolling-back" {
		t.Errorf("Find: %v", p)
	}
	if miss, _ := posts.Find(99); miss != nil {
		t.Error("Find of a missing key is nil")
	}
	if has, _ := posts.Contains("3"); !has {
		t.Error("Contains compares keys by value, so \"3\" matches 3")
	}
	only, _ := posts.Only(1, 3)
	rest, _ := posts.Except(1, 3)
	if only.Count() != 2 || rest.Count() != 1 || rest[0].ID != 2 {
		t.Errorf("Only/Except: %d %d", only.Count(), rest.Count())
	}

	// hand-built structs are not bound: the helpers say so instead of guessing
	loose := orm.Collection[Post]{{Slug: "x"}}
	if _, err := loose.Pluck("slug"); migro.CodeOf(err) != migro.UnboundModel {
		t.Errorf("unbound: %v", err)
	}
}

func TestCollectionLoadAndLoadMissing(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	users := must(orm.New[User](db).Query().OrderBy("id", "asc").Get(ctx))

	if users[0].Posts.Loaded() {
		t.Fatal("not loaded yet")
	}
	ok(t, users.Load(ctx, "Posts.Comments", "Roles"))
	if !users[0].Posts.Loaded() || users[0].Posts.Items().Count() != 2 || users[2].Roles.Items().Count() != 1 {
		t.Fatal("Load did not fill the relations")
	}
	if users[0].Posts.Items()[0].Comments.Items().Count() != 2 {
		t.Error("nested relation not loaded")
	}

	// LoadMissing leaves loaded models alone and fills the rest
	fresh := must(orm.New[User](db).Query().OrderBy("id", "asc").Get(ctx))
	ok(t, fresh[0].Posts.Load(ctx))
	marker := &fresh[0].Posts.Items()[0]
	ok(t, fresh.LoadMissing(ctx, "Posts"))
	if &fresh[0].Posts.Items()[0] != marker {
		t.Error("LoadMissing reloaded a relation that was already loaded")
	}
	if !fresh[1].Posts.Loaded() || fresh[1].Posts.Items().Count() != 1 || !fresh[2].Posts.Loaded() {
		t.Error("LoadMissing skipped the models that needed it")
	}
	// nothing to do on an empty collection
	ok(t, (orm.Collection[User]{}).Load(ctx, "Posts"))
	ok(t, (orm.Collection[User]{}).LoadMissing(ctx, "Posts"))
}

func TestCollectionFreshKeepsOrderAndDropsDeletedRows(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	posts := must(orm.New[Post](db).Query().OrderBy("id", "desc").Get(ctx)) // 3, 2, 1

	mustExec(t, db, "UPDATE posts SET title = ? WHERE id = ?", "Changed", 2)
	mustExec(t, db, "DELETE FROM taggables WHERE taggable_type = 'post' AND taggable_id = ?", 3)
	mustExec(t, db, "DELETE FROM comments WHERE commentable_type = 'post' AND commentable_id = ?", 3)
	mustExec(t, db, "DELETE FROM posts WHERE id = ?", 3)

	fresh, err := posts.Fresh(ctx)
	ok(t, err)
	if fresh.Count() != 2 || fresh[0].ID != 2 || fresh[1].ID != 1 {
		t.Fatalf("order/deletion: %+v", fresh)
	}
	if fresh[0].Title != "Changed" || posts[1].Title == "Changed" {
		t.Error("Fresh returns new instances and leaves the originals alone")
	}
	if !fresh[0].Exists() {
		t.Error("fresh models are bound")
	}
}
