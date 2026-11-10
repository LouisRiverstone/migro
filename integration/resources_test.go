package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	. "github.com/LouisRiverstone/migro/integration/models"
	"github.com/LouisRiverstone/migro/orm"
)

func asJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	return out
}

func TestResourceExposesOnlyWhatItLists(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	users := orm.New[User](db)
	ana := must(users.Find(ctx, 1))
	ok(t, ana.SetAttribute("password", "pw"))
	ok(t, ana.Save(ctx))

	body, err := UserResource.Make(must(users.Find(ctx, 1)))
	ok(t, err)
	data := asJSON(t, body)["data"].(map[string]any)
	if data["email"] != "ana@example.com" || data["name"] != "Ana" {
		t.Fatalf("data = %v", data)
	}
	for _, leaked := range []string{"password", "api_token", "country_id", "created_at", "settings"} {
		if _, has := data[leaked]; has {
			t.Errorf("resource exposed %q", leaked)
		}
	}
	if _, has := data["role"]; has {
		t.Error("role is for admins only")
	}
	if _, has := data["posts"]; has {
		t.Error("an unloaded relation must not appear (and must not be queried)")
	}

	// conditional fields
	ok(t, ana.SetAttribute("role", "admin"))
	adminBody, _ := UserResource.Make(ana)
	if asJSON(t, adminBody)["data"].(map[string]any)["role"] != "admin" {
		t.Error("admin role missing")
	}
}

func TestResourceWhenLoadedAndNestedResources(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	rows, err := orm.New[User](db).Query().With("Posts.Comments").Where("id = ?", 1).Get(ctx)
	ok(t, err)

	body, err := UserResource.Make(&rows[0])
	ok(t, err)
	posts := asJSON(t, body)["data"].(map[string]any)["posts"].([]any)
	if len(posts) != 2 {
		t.Fatalf("posts = %v", posts)
	}
	first := posts[0].(map[string]any)
	if first["slug"] != "hello-db" {
		t.Errorf("first post %v", first)
	}
	comments := first["comments"].([]any)
	if len(comments) != 2 || comments[0].(map[string]any)["body"] == nil {
		t.Errorf("nested comments %v", comments)
	}
	if second := posts[1].(map[string]any)["comments"].([]any); len(second) != 0 {
		t.Errorf("loaded but empty relation should be [], got %v", second)
	}
}

func TestResourceCollectionWrappingAndAdditional(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	users, err := orm.New[User](db).Query().OrderBy("id", "asc").Get(ctx)
	ok(t, err)

	col, err := UserResource.Collection(users)
	ok(t, err)
	if list := asJSON(t, col)["data"].([]any); len(list) != 3 {
		t.Errorf("collection: %v", list)
	}

	custom, err := UserResource.Wrap("users").Additional(map[string]any{"version": 2}).Collection(users)
	ok(t, err)
	got := asJSON(t, custom)
	if _, has := got["data"]; has || len(got["users"].([]any)) != 3 || got["version"] != float64(2) {
		t.Errorf("custom wrap: %v", got)
	}

	bare, err := UserResource.WithoutWrapping().Collection(users)
	ok(t, err)
	b, _ := json.Marshal(bare)
	if b[0] != '[' {
		t.Errorf("unwrapped collection should be an array: %s", b)
	}
	one, err := UserResource.WithoutWrapping().Make(&users[0])
	ok(t, err)
	if asJSON(t, one)["email"] != "ana@example.com" {
		t.Errorf("unwrapped item: %v", one)
	}
	// the original resource is untouched by the builders above
	again, _ := UserResource.Collection(users)
	if _, has := asJSON(t, again)["data"]; !has {
		t.Error("builder methods must return copies")
	}
}

func TestResourcePaginatedMetaAndLinks(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	page, err := orm.New[User](db).Query().OrderBy("id", "asc").Paginate(ctx, 2, 2)
	ok(t, err)

	body, err := UserResource.Paginated(page, "https://api.test/users?sort=name")
	ok(t, err)
	got := asJSON(t, body)
	if len(got["data"].([]any)) != 1 {
		t.Errorf("page 2 of 2 holds one user: %v", got["data"])
	}
	meta := got["meta"].(map[string]any)
	want := map[string]float64{"current_page": 2, "last_page": 2, "per_page": 2, "total": 3, "from": 3, "to": 3}
	for k, v := range want {
		if meta[k] != v {
			t.Errorf("meta[%s] = %v, want %v", k, meta[k], v)
		}
	}
	links := got["links"].(map[string]any)
	if links["next"] != nil || links["prev"] != "https://api.test/users?page=1&sort=name" || links["first"] == nil || links["last"] == nil {
		t.Errorf("links = %v", links)
	}

	// without a base URL there are no links
	plain, _ := UserResource.Paginated(page)
	if _, has := asJSON(t, plain)["links"]; has {
		t.Error("links appear only when a base URL is given")
	}
	// an empty page reports from/to as 0
	empty, err := orm.New[User](db).Query().Where("id > ?", 99).Paginate(ctx, 1, 10)
	ok(t, err)
	eb, _ := UserResource.Paginated(empty)
	if m := asJSON(t, eb)["meta"].(map[string]any); m["from"] != float64(0) || m["to"] != float64(0) {
		t.Errorf("empty meta %v", m)
	}
}
