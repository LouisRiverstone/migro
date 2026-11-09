package orm_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
	"gorm.io/gorm"
)

type eCountry struct {
	orm.BaseModel
	Name  string
	Users orm.HasMany[eUser]               `gorm:"-" orm:"foreignKey:country_id"`
	Posts orm.HasManyThrough[ePost, eUser] `gorm:"-" orm:"foreignKey:country_id;throughForeignKey:user_id"`
}

func (eCountry) TableName() string { return "e_countries" }

type eUser struct {
	orm.SoftDeleteModel
	CountryID *uint
	Name      string
	Email     string
	Password  string
	Visits    int
	Country   orm.BelongsTo[eCountry]  `gorm:"-" orm:"foreignKey:country_id"`
	Profile   orm.HasOne[eProfile]     `gorm:"-" orm:"foreignKey:user_id"`
	Posts     orm.HasMany[ePost]       `gorm:"-" orm:"foreignKey:user_id"`
	Roles     orm.BelongsToMany[eRole] `gorm:"-" orm:"pivotTable:e_role_user;pivotForeignKey:user_id;pivotRelatedForeignKey:role_id;pivotColumns:assigned_by"`
}

func (eUser) TableName() string { return "e_users" }
func (eUser) Guarded() []string { return []string{"password", "visits"} }
func (eUser) Hidden() []string  { return []string{"password"} }
func (u eUser) Appends() map[string]any {
	return map[string]any{"initials": strings.ToUpper(u.Name[:1])}
}

type eProfile struct {
	orm.BaseModel
	UserID uint
	Bio    string
}

func (eProfile) TableName() string { return "e_profiles" }

type ePost struct {
	orm.BaseModel
	UserID   uint
	Title    string
	Author   orm.BelongsTo[eUser]    `gorm:"-" orm:"foreignKey:user_id"`
	Comments orm.MorphMany[eComment] `gorm:"-" orm:"morph:commentable"`
	Tags     orm.MorphToMany[eTag]   `gorm:"-" orm:"morph:taggable;pivotTable:e_taggables;pivotRelatedForeignKey:tag_id"`
}

func (ePost) TableName() string { return "e_posts" }

type eVideo struct {
	orm.BaseModel
	Title    string
	Comments orm.MorphMany[eComment] `gorm:"-" orm:"morph:commentable"`
}

func (eVideo) TableName() string { return "e_videos" }

type eRole struct {
	orm.BaseModel
	Name string
}

func (eRole) TableName() string { return "e_roles" }

type eTag struct {
	orm.BaseModel
	Name  string
	Posts orm.MorphedByMany[ePost] `gorm:"-" orm:"morph:taggable;pivotTable:e_taggables;pivotForeignKey:tag_id"`
}

func (eTag) TableName() string { return "e_tags" }

type eComment struct {
	orm.BaseModel
	CommentableType string
	CommentableID   uint
	Body            string
	Commentable     orm.MorphTo `gorm:"-" orm:"morph:commentable"`
}

func (eComment) TableName() string { return "e_comments" }

func eSetup(t *testing.T) (*migro.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db := testdb.Open(t)
	s := schema.New(db)
	tables := []struct {
		name string
		fn   func(*schema.Table)
	}{
		{"e_countries", func(t *schema.Table) { t.Increments("id"); t.String("name"); t.Timestamps() }},
		{"e_users", func(t *schema.Table) {
			t.Increments("id")
			t.Integer("country_id").Nullable()
			t.String("name")
			t.String("email").Unique()
			t.String("password").Default("")
			t.Integer("visits").Default(0)
			t.Timestamps()
			t.Timestamp("deleted_at").Nullable()
		}},
		{"e_profiles", func(t *schema.Table) { t.Increments("id"); t.Integer("user_id"); t.String("bio"); t.Timestamps() }},
		{"e_posts", func(t *schema.Table) { t.Increments("id"); t.Integer("user_id"); t.String("title"); t.Timestamps() }},
		{"e_videos", func(t *schema.Table) { t.Increments("id"); t.String("title"); t.Timestamps() }},
		{"e_roles", func(t *schema.Table) { t.Increments("id"); t.String("name"); t.Timestamps() }},
		{"e_role_user", func(t *schema.Table) {
			t.Integer("user_id")
			t.Integer("role_id")
			t.String("assigned_by").Nullable()
		}},
		{"e_tags", func(t *schema.Table) { t.Increments("id"); t.String("name"); t.Timestamps() }},
		{"e_taggables", func(t *schema.Table) {
			t.Integer("tag_id")
			t.Integer("taggable_id")
			t.String("taggable_type")
		}},
		{"e_comments", func(t *schema.Table) {
			t.Increments("id")
			t.String("commentable_type")
			t.Integer("commentable_id")
			t.String("body")
			t.Timestamps()
		}},
	}
	for _, tb := range tables {
		if err := s.CreateTable(ctx, tb.name, tb.fn); err != nil {
			t.Fatal(err)
		}
	}
	orm.MorphMap(map[string]any{"e_post": ePost{}, "e_video": eVideo{}})
	return db, ctx
}

// must unwraps a (value, error) pair; a panic here fails the test with the error attached.
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

// eSeed inserts a small, fully linked data set and returns the db.
func eSeed(t *testing.T) (*migro.DB, context.Context) {
	db, ctx := eSetup(t)
	u1, u2, u3 := uint(1), uint(1), uint(2)
	ok(t, orm.New[eCountry](db).CreateMany(ctx, []eCountry{{Name: "BR"}, {Name: "PT"}}))
	ok(t, orm.New[eUser](db).CreateMany(ctx, []eUser{
		{CountryID: &u1, Name: "ana", Email: "ana@x.io", Password: "p1"},
		{CountryID: &u2, Name: "bia", Email: "bia@x.io", Password: "p2"},
		{CountryID: &u3, Name: "caio", Email: "caio@x.io", Password: "p3"},
	}))
	ok(t, orm.New[eProfile](db).Create(ctx, &eProfile{UserID: 1, Bio: "hi"}))
	ok(t, orm.New[ePost](db).CreateMany(ctx, []ePost{{UserID: 1, Title: "p1"}, {UserID: 1, Title: "p2"}, {UserID: 2, Title: "p3"}}))
	ok(t, orm.New[eVideo](db).Create(ctx, &eVideo{Title: "v1"}))
	ok(t, orm.New[eRole](db).CreateMany(ctx, []eRole{{Name: "admin"}, {Name: "editor"}}))
	ok(t, orm.New[eTag](db).CreateMany(ctx, []eTag{{Name: "go"}, {Name: "sql"}, {Name: "unused"}}))
	ok(t, orm.New[eComment](db).CreateMany(ctx, []eComment{
		{CommentableType: "e_post", CommentableID: 1, Body: "c1"},
		{CommentableType: "e_post", CommentableID: 1, Body: "c2"},
		{CommentableType: "e_video", CommentableID: 1, Body: "c3"},
		{CommentableType: "e_post", CommentableID: 99, Body: "orphan"},
	}))
	db.Gorm.Exec("INSERT INTO e_role_user (user_id, role_id, assigned_by) VALUES (1,1,'root'),(1,2,NULL),(2,2,'ana')")
	db.Gorm.Exec("INSERT INTO e_taggables (tag_id, taggable_id, taggable_type) VALUES (1,1,'e_post'),(2,1,'e_post'),(1,2,'e_post'),(2,1,'e_video')")
	return db, ctx
}

// ---- instance behaviour --------------------------------------------------------------------

func TestEntityLifecycleAndDirtyTracking(t *testing.T) {
	db, ctx := eSetup(t)
	users := orm.New[eUser](db)

	u, err := users.CreateFrom(ctx, map[string]any{"name": "Ana", "email": "ana@x.io"})
	if err != nil || u.ID == 0 || !u.Exists() || u.IsDirty() {
		t.Fatalf("create: %+v %v", u, err)
	}
	u.Name = "Ana B"
	if !u.IsDirty("name") || u.IsDirty("email") || u.GetOriginal("name") != "Ana" || u.Dirty()["name"] != "Ana B" {
		t.Fatalf("dirty tracking: %v", u.Dirty())
	}
	ok(t, u.Save(ctx))
	if u.IsDirty() {
		t.Fatal("save must reset the snapshot")
	}
	f1 := must(orm.Fresh(ctx, u))
	if f1.Name != "Ana B" {
		t.Fatalf("not persisted: %+v", f1)
	}

	// a clean save issues no UPDATE: updated_at must not move
	time.Sleep(15 * time.Millisecond)
	ok(t, u.Save(ctx))
	if f2 := must(orm.Fresh(ctx, u)); !f2.UpdatedAt.Equal(f1.UpdatedAt) {
		t.Fatalf("clean save touched the row: %v -> %v", f1.UpdatedAt, f2.UpdatedAt)
	}
	u.Email = "new@x.io"
	ok(t, u.Save(ctx))
	if f3 := must(orm.Fresh(ctx, u)); !f3.UpdatedAt.After(f1.UpdatedAt) || f3.Email != "new@x.io" {
		t.Fatalf("dirty save must bump updated_at: %+v", f3)
	}

	// a stale instance picks up outside changes with Refresh
	db.Gorm.Exec("UPDATE e_users SET name = 'Outside' WHERE id = ?", u.ID)
	ok(t, u.Refresh(ctx))
	if u.Name != "Outside" || u.IsDirty() {
		t.Fatalf("refresh: %+v", u)
	}

	ok(t, u.Increment(ctx, "visits", 5))
	ok(t, u.Decrement(ctx, "visits", 2))
	if u.Visits != 3 {
		t.Fatalf("increment: %d", u.Visits)
	}
	ok(t, u.Update(ctx, map[string]any{"name": "Via Update"}))
	if must(users.Find(ctx, u.ID)).Name != "Via Update" {
		t.Fatal("Update must persist")
	}
	if err := u.Update(ctx, map[string]any{"visits": 99}); migro.CodeOf(err) != migro.MassAssignment {
		t.Fatalf("guarded column: %v", err)
	}
}

func TestSoftDeleteRestoreAndForceDelete(t *testing.T) {
	db, ctx := eSetup(t)
	users := orm.New[eUser](db)
	u := must(users.CreateFrom(ctx, map[string]any{"name": "Ana", "email": "a@x.io"}))

	ok(t, u.Delete(ctx))
	if !u.Trashed() || !u.Exists() {
		t.Fatalf("soft delete: trashed=%v exists=%v", u.Trashed(), u.Exists())
	}
	if n, _ := users.Query().Count(ctx); n != 0 {
		t.Fatalf("hidden rows: %d", n)
	}
	ok(t, u.Restore(ctx))
	if u.Trashed() {
		t.Fatal("restore")
	}
	if n, _ := users.Query().Count(ctx); n != 1 {
		t.Fatalf("restored rows: %d", n)
	}
	ok(t, u.ForceDelete(ctx))
	if n, _ := users.Query().WithTrashed().Count(ctx); n != 0 || u.Exists() {
		t.Fatalf("force delete left %d rows", n)
	}
	if err := (&eVideo{}).Save(ctx); migro.CodeOf(err) != migro.UnboundModel {
		t.Fatalf("unbound model: %v", err)
	}
	// hard-deleting model: Delete marks it as gone
	v := must(orm.New[eVideo](db).CreateFrom(ctx, map[string]any{"title": "v"}))
	ok(t, v.Delete(ctx))
	if v.Exists() {
		t.Fatal("hard delete")
	}
}

func TestFillFillableGuardedAndSerialization(t *testing.T) {
	db, ctx := eSeed(t)
	users := orm.New[eUser](db)

	if _, err := users.CreateFrom(ctx, map[string]any{"name": "x", "email": "x@x.io", "password": "leak"}); migro.CodeOf(err) != migro.MassAssignment {
		t.Fatalf("guarded: %v", err)
	}
	if _, err := users.Make(map[string]any{"nope": 1}); migro.CodeOf(err) != migro.MassAssignment {
		t.Fatalf("unknown column: %v", err)
	}
	made := must(users.Make(map[string]any{"Name": "by field name", "email": "m@x.io"}))
	if made.Name != "by field name" || made.Exists() {
		t.Fatalf("make: %+v", made)
	}

	u := must(users.Query().With("Profile").With("Posts").With("Roles").Where("id = ?", 1).First(ctx))
	m, err := orm.ToMap(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := m["password"]; leaked || m["initials"] != "A" || m["name"] != "ana" {
		t.Fatalf("hidden/appends: %v", m)
	}
	if prof, _ := m["profile"].(map[string]any); prof["bio"] != "hi" {
		t.Fatalf("hasOne in map: %v", m["profile"])
	}
	posts, _ := m["posts"].([]any)
	roles, _ := m["roles"].([]any)
	if len(posts) != 2 || len(roles) != 2 {
		t.Fatalf("nested relations: posts=%d roles=%d", len(posts), len(roles))
	}
	if _, loaded := m["country"]; loaded {
		t.Fatal("relations that were not loaded must not appear")
	}
	js, err := orm.JSON(u)
	if err != nil || !strings.Contains(string(js), `"initials":"A"`) || strings.Contains(string(js), "password") {
		t.Fatalf("json: %s %v", js, err)
	}
}

// ---- query additions -----------------------------------------------------------------------

func TestQueryHelpers(t *testing.T) {
	db, ctx := eSeed(t)
	users := orm.New[eUser](db)

	names, err := orm.Pluck[eUser, string](ctx, users.Query().OrderBy("name", "desc"), "name")
	if err != nil || strings.Join(names, ",") != "caio,bia,ana" {
		t.Fatalf("pluck: %v %v", names, err)
	}
	if v, err := orm.Value[eUser, string](ctx, users.Query().Where("name = ?", "bia"), "email"); err != nil || v != "bia@x.io" {
		t.Fatalf("value: %v %v", v, err)
	}
	if _, err := orm.Value[eUser, string](ctx, users.Query().Where("name = ?", "ghost"), "email"); migro.CodeOf(err) != migro.NotFound {
		t.Fatalf("missing value: %v", err)
	}
	db.Gorm.Exec("UPDATE e_users SET visits = id * 10")
	if s, _ := users.Query().Sum(ctx, "visits"); s != 60 {
		t.Fatalf("sum: %v", s)
	}
	if a, _ := users.Query().Avg(ctx, "visits"); a != 20 {
		t.Fatalf("avg: %v", a)
	}
	if mx, _ := users.Query().Max(ctx, "visits"); asNum(mx) != 30 {
		t.Fatalf("max: %v", mx)
	}
	if mn, _ := users.Query().Where("id > 99").Min(ctx, "visits"); mn != nil {
		t.Fatalf("min of nothing: %v", mn)
	}
	if s, _ := users.Query().Where("id > 99").Sum(ctx, "visits"); s != 0 {
		t.Fatalf("sum of nothing: %v", s)
	}

	got := must(users.Query().WhereNotIn("id", []int{1}).WhereBetween("visits", 10, 25).Oldest("id").Get(ctx))
	if len(got) != 1 || got[0].Name != "bia" {
		t.Fatalf("where helpers: %+v", got)
	}
	latest := must(users.Query().Latest("id").Limit(1).Get(ctx))
	if latest[0].Name != "caio" {
		t.Fatalf("latest: %+v", latest)
	}
	filtered := must(users.Query().When(false, func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.Where("id = 1") }).
		When(true, func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.Where("id = 2") }).Get(ctx))
	if len(filtered) != 1 || filtered[0].ID != 2 {
		t.Fatalf("when: %+v", filtered)
	}

	var seen []uint
	ok(t, users.Query().With("Profile").Chunk(ctx, 2, func(rows []eUser) error {
		for _, r := range rows {
			seen = append(seen, r.ID)
		}
		return nil
	}))
	if len(seen) != 3 || seen[0] != 1 || seen[2] != 3 {
		t.Fatalf("chunk: %v", seen)
	}
	if err := users.Query().Chunk(ctx, 0, nil); migro.CodeOf(err) != migro.QueryFailed {
		t.Fatalf("chunk size: %v", err)
	}

	n, err := users.Query().Where("id = ?", 1).Increment(ctx, "visits", 1)
	if err != nil || n != 1 {
		t.Fatalf("increment: %d %v", n, err)
	}
	if _, err := users.FindOrFail(ctx, 99); migro.CodeOf(err) != migro.NotFound {
		t.Fatalf("findOrFail: %v", err)
	}
	if _, err := users.Query().Where("id = 99").FirstOrFail(ctx); migro.CodeOf(err) != migro.NotFound {
		t.Fatalf("firstOrFail: %v", err)
	}
	many := must(users.FindMany(ctx, 1, 3, 99))
	if len(many) != 2 {
		t.Fatalf("findMany: %+v", many)
	}
	if n, err := users.Destroy(ctx, 2, 3); err != nil || n != 2 {
		t.Fatalf("destroy: %d %v", n, err)
	}
}

func asNum(v any) float64 {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return -1
}

func TestUpdateOrCreateManyAndFirstOrCreateMany(t *testing.T) {
	db, ctx := eSetup(t)
	users := orm.New[eUser](db)
	first := must(users.UpdateOrCreateMany(ctx, []string{"email"}, []eUser{
		{Name: "Ana", Email: "ana@x.io"}, {Name: "Bia", Email: "bia@x.io"},
	}))
	if len(first) != 2 || first[0].ID == 0 {
		t.Fatalf("insert: %+v", first)
	}
	again := must(users.UpdateOrCreateMany(ctx, []string{"email"}, []eUser{
		{Name: "Ana Updated", Email: "ana@x.io"}, {Name: "Caio", Email: "caio@x.io"},
	}))
	if n, _ := users.Query().Count(ctx); n != 3 {
		t.Fatalf("rows: %d", n)
	}
	if again[0].ID != first[0].ID || must(users.Find(ctx, first[0].ID)).Name != "Ana Updated" {
		t.Fatalf("existing row must be updated in place: %+v", again)
	}
	kept := must(users.FirstOrCreateMany(ctx, []string{"email"}, []eUser{{Name: "Changed", Email: "bia@x.io"}}))
	if kept[0].Name != "Bia" {
		t.Fatalf("firstOrCreateMany must not update: %+v", kept)
	}
	if _, err := users.UpdateOrCreateMany(ctx, nil, nil); migro.CodeOf(err) != migro.InvalidConfig {
		t.Fatalf("no keys: %v", err)
	}
	ok(t, users.Truncate(ctx))
	if n, _ := users.Query().WithTrashed().Count(ctx); n != 0 {
		t.Fatalf("truncate left %d", n)
	}
}

// ---- relations -----------------------------------------------------------------------------

func TestEagerLoadingEveryRelationKind(t *testing.T) {
	db, ctx := eSeed(t)

	users := must(orm.New[eUser](db).Query().With("Profile").With("Posts").With("Country").With("Roles").OrderBy("id", "asc").Get(ctx))
	ana, bia, caio := users[0], users[1], users[2]
	if ana.Profile.Value() == nil || ana.Profile.Value().Bio != "hi" || bia.Profile.Value() != nil || !bia.Profile.Loaded() {
		t.Fatalf("hasOne: %+v / %+v", ana.Profile.Value(), bia.Profile.Value())
	}
	if len(ana.Posts.Items()) != 2 || len(bia.Posts.Items()) != 1 || len(caio.Posts.Items()) != 0 {
		t.Fatalf("hasMany: %d %d %d", len(ana.Posts.Items()), len(bia.Posts.Items()), len(caio.Posts.Items()))
	}
	if ana.Country.Value().Name != "BR" || caio.Country.Value().Name != "PT" {
		t.Fatalf("belongsTo: %+v %+v", ana.Country.Value(), caio.Country.Value())
	}
	if len(ana.Roles.Items()) != 2 || len(bia.Roles.Items()) != 1 || len(caio.Roles.Items()) != 0 {
		t.Fatalf("belongsToMany: %v", users)
	}
	adminRole := &ana.Roles.Items()[0] // children are bound in place: take a pointer, not a copy
	if adminRole.Name != "admin" || adminRole.Extra("pivot_assigned_by") != "root" {
		t.Fatalf("pivot columns: %+v extras=%v", adminRole, adminRole.Extras())
	}
	// related models are bound: their own instance methods work
	adminRole.Name = "root"
	if !adminRole.IsDirty("name") {
		t.Fatal("eager-loaded children must be bound")
	}

	countries := must(orm.New[eCountry](db).Query().With("Users").With("Posts").OrderBy("id", "asc").Get(ctx))
	if len(countries[0].Users.Items()) != 2 || len(countries[0].Posts.Items()) != 3 || len(countries[1].Posts.Items()) != 0 {
		t.Fatalf("hasManyThrough: users=%d posts=%d", len(countries[0].Users.Items()), len(countries[0].Posts.Items()))
	}

	posts := must(orm.New[ePost](db).Query().With("Author").With("Comments").With("Tags").OrderBy("id", "asc").Get(ctx))
	if posts[0].Author.Value().Name != "ana" || posts[2].Author.Value().Name != "bia" {
		t.Fatalf("belongsTo author: %+v", posts[0].Author.Value())
	}
	if len(posts[0].Comments.Items()) != 2 || len(posts[1].Comments.Items()) != 0 {
		t.Fatalf("morphMany: %d", len(posts[0].Comments.Items()))
	}
	if len(posts[0].Tags.Items()) != 2 || len(posts[1].Tags.Items()) != 1 || len(posts[2].Tags.Items()) != 0 {
		t.Fatalf("morphToMany: %d %d", len(posts[0].Tags.Items()), len(posts[1].Tags.Items()))
	}

	tags := must(orm.New[eTag](db).Query().With("Posts").OrderBy("id", "asc").Get(ctx))
	if len(tags[0].Posts.Items()) != 2 || len(tags[1].Posts.Items()) != 1 || len(tags[2].Posts.Items()) != 0 {
		t.Fatalf("morphedByMany: %d %d %d", len(tags[0].Posts.Items()), len(tags[1].Posts.Items()), len(tags[2].Posts.Items()))
	}

	comments := must(orm.New[eComment](db).Query().With("Commentable").OrderBy("id", "asc").Get(ctx))
	if p, ok := comments[0].Commentable.Value().(*ePost); !ok || p.Title != "p1" || !p.Exists() {
		t.Fatalf("morphTo post: %+v", comments[0].Commentable.Value())
	}
	if v, ok := comments[2].Commentable.Value().(*eVideo); !ok || v.Title != "v1" {
		t.Fatalf("morphTo video: %+v", comments[2].Commentable.Value())
	}
	if comments[3].Commentable.Value() != nil || !comments[3].Commentable.Loaded() {
		t.Fatal("dangling morphTo must load as nil")
	}
}

func TestNestedConstrainedAndCountedEagerLoads(t *testing.T) {
	db, ctx := eSeed(t)

	users := must(orm.New[eUser](db).Query().With("Posts.Comments").With("Posts.Tags").OrderBy("id", "asc").Get(ctx))
	p1 := users[0].Posts.Items()[0]
	if len(p1.Comments.Items()) != 2 || len(p1.Tags.Items()) != 2 || !p1.Comments.Loaded() {
		t.Fatalf("nested: comments=%d tags=%d", len(p1.Comments.Items()), len(p1.Tags.Items()))
	}

	only := must(orm.New[eUser](db).Query().With("Posts", func(q *gorm.DB) *gorm.DB { return q.Where("title = ?", "p2") }).Where("id = 1").First(ctx))
	if len(only.Posts.Items()) != 1 || only.Posts.Items()[0].Title != "p2" {
		t.Fatalf("constrained: %+v", only.Posts.Items())
	}

	counted := must(orm.New[eUser](db).Query().WithCount("Posts", "Roles").OrderBy("id", "asc").Get(ctx))
	if counted[0].Extra("posts_count") != int64(2) || counted[1].Extra("posts_count") != int64(1) || counted[2].Extra("posts_count") != int64(0) {
		t.Fatalf("posts_count: %v %v %v", counted[0].Extras(), counted[1].Extras(), counted[2].Extras())
	}
	if counted[0].Extra("roles_count") != int64(2) || counted[2].Extra("roles_count") != int64(0) {
		t.Fatalf("roles_count: %v", counted[0].Extras())
	}
	cm := must(orm.New[ePost](db).Query().WithCount("Comments").OrderBy("id", "asc").Get(ctx))
	if cm[0].Extra("comments_count") != int64(2) || cm[1].Extra("comments_count") != int64(0) {
		t.Fatalf("morph count: %v", cm[0].Extras())
	}

	if _, err := orm.New[eUser](db).Query().With("Nope").Get(ctx); migro.CodeOf(err) != migro.InvalidSchema {
		t.Fatalf("unknown relation: %v", err)
	}
	if _, err := orm.New[eCountry](db).Query().WithCount("Posts").Get(ctx); migro.CodeOf(err) != migro.UnsupportedFeature {
		t.Fatalf("count through: %v", err)
	}
	// Paginate and First hydrate too
	pg := must(orm.New[eUser](db).Query().With("Posts").OrderBy("id", "asc").Paginate(ctx, 1, 2))
	if len(pg.Data) != 2 || len(pg.Data[0].Posts.Items()) != 2 {
		t.Fatalf("paginate hydrate: %+v", pg)
	}
	ok(t, orm.Load(ctx, db, &pg.Data[1], "Roles", "Posts.Comments"))
	if len(pg.Data[1].Roles.Items()) != 1 || !pg.Data[1].Posts.Items()[0].Comments.Loaded() {
		t.Fatal("Load on a fetched model")
	}
}

func TestRelationExistenceQueries(t *testing.T) {
	db, ctx := eSeed(t)
	users := orm.New[eUser](db)
	names := func(q *orm.Query[eUser]) string {
		rows := must(q.OrderBy("id", "asc").Get(ctx))
		var out []string
		for _, r := range rows {
			out = append(out, r.Name)
		}
		return strings.Join(out, ",")
	}
	cases := map[string]struct {
		q    *orm.Query[eUser]
		want string
	}{
		"has hasMany":         {users.Query().Has("Posts"), "ana,bia"},
		"doesntHave hasMany":  {users.Query().DoesntHave("Posts"), "caio"},
		"has hasOne":          {users.Query().Has("Profile"), "ana"},
		"has pivot":           {users.Query().Has("Roles"), "ana,bia"},
		"whereHas pivot":      {users.Query().WhereHas("Roles", func(q *gorm.DB) *gorm.DB { return q.Where("name = ?", "admin") }), "ana"},
		"whereHas hasMany":    {users.Query().WhereHas("Posts", func(q *gorm.DB) *gorm.DB { return q.Where("title = ?", "p3") }), "bia"},
		"whereDoesntHave":     {users.Query().WhereDoesntHave("Posts", func(q *gorm.DB) *gorm.DB { return q.Where("title = ?", "p3") }), "ana,caio"},
		"has belongsTo":       {users.Query().Has("Country"), "ana,bia,caio"},
		"combined with where": {users.Query().Has("Posts").Where("name <> ?", "ana"), "bia"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := names(c.q); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
	// through and morph relations in existence checks
	cs := must(orm.New[eCountry](db).Query().Has("Posts").Get(ctx))
	if len(cs) != 1 || cs[0].Name != "BR" {
		t.Fatalf("has through: %+v", cs)
	}
	ps := must(orm.New[ePost](db).Query().Has("Comments").Get(ctx))
	tg := must(orm.New[eTag](db).Query().DoesntHave("Posts").Get(ctx))
	if len(ps) != 1 || ps[0].Title != "p1" || len(tg) != 1 || tg[0].Name != "unused" {
		t.Fatalf("morph has: %+v %+v", ps, tg)
	}
	if _, err := orm.New[eComment](db).Query().Has("Commentable").Get(ctx); migro.CodeOf(err) != migro.UnsupportedFeature {
		t.Fatalf("has morphTo: %v", err)
	}
}

func TestLazyLoadingAndRelationQueries(t *testing.T) {
	db, ctx := eSeed(t)
	ana := must(orm.New[eUser](db).Find(ctx, 1))

	if ana.Posts.Loaded() || len(ana.Posts.Items()) != 0 {
		t.Fatal("relations start unloaded")
	}
	posts := must(ana.Posts.Get(ctx))
	if len(posts) != 2 || !ana.Posts.Loaded() {
		t.Fatalf("lazy get: %+v", posts)
	}
	if country := must(ana.Country.Get(ctx)); country.Name != "BR" {
		t.Fatalf("lazy belongsTo: %+v", country)
	}
	roles := must(ana.Roles.Get(ctx))
	if len(roles) != 2 {
		t.Fatalf("lazy pivot: %+v", roles)
	}
	q := must(ana.Posts.Query())
	titles := must(orm.Pluck[ePost, string](ctx, q.Where("title <> ?", "p1"), "title"))
	if len(titles) != 1 || titles[0] != "p2" {
		t.Fatalf("relation query: %v", titles)
	}
	rq := must(ana.Roles.Query())
	if n, _ := rq.Count(ctx); n != 2 {
		t.Fatalf("pivot relation query: %d", n)
	}
	post := must(orm.New[ePost](db).Find(ctx, 1))
	if kids := must(post.Comments.Get(ctx)); len(kids) != 2 {
		t.Fatalf("lazy morphMany: %+v", kids)
	}
	c := must(orm.New[eComment](db).Find(ctx, 3))
	if target := must(c.Commentable.Get(ctx)); target.(*eVideo).Title != "v1" {
		t.Fatalf("lazy morphTo: %+v", target)
	}
	var loose eUser
	if _, err := loose.Posts.Get(ctx); migro.CodeOf(err) != migro.UnboundModel {
		t.Fatalf("unbound relation: %v", err)
	}
}

func TestRelationWrites(t *testing.T) {
	db, ctx := eSeed(t)
	ana := must(orm.New[eUser](db).Find(ctx, 1))

	// hasMany create / save / createMany stamp the foreign key
	p := must(ana.Posts.Create(ctx, map[string]any{"title": "new"}))
	if p.UserID != 1 || p.ID == 0 || !p.Exists() {
		t.Fatalf("create: %+v", p)
	}
	loose := &ePost{Title: "saved"}
	ok(t, ana.Posts.Save(ctx, loose))
	if loose.UserID != 1 || loose.ID == 0 {
		t.Fatalf("save: %+v", loose)
	}
	many := must(ana.Posts.CreateMany(ctx, []map[string]any{{"title": "m1"}, {"title": "m2"}}))
	if len(many) != 2 || many[1].UserID != 1 {
		t.Fatalf("createMany: %+v", many)
	}
	if _, err := ana.Posts.CreateMany(ctx, []map[string]any{{"title": "ok"}, {"bogus": 1}}); migro.CodeOf(err) != migro.MassAssignment {
		t.Fatalf("bad attrs: %v", err)
	}
	if n, _ := orm.New[ePost](db).Query().Where("user_id = 1").Count(ctx); n != 6 {
		t.Fatalf("createMany must be atomic: %d posts", n)
	}
	// morphMany stamps type and id
	post := must(orm.New[ePost](db).Find(ctx, 2))
	cm := must(post.Comments.Create(ctx, map[string]any{"body": "via relation"}))
	if cm.CommentableType != "e_post" || cm.CommentableID != 2 {
		t.Fatalf("morph create: %+v", cm)
	}
	// hasOne
	bia := must(orm.New[eUser](db).Find(ctx, 2))
	prof := must(bia.Profile.Create(ctx, map[string]any{"bio": "bio"}))
	if prof.UserID != 2 {
		t.Fatalf("hasOne create: %+v", prof)
	}

	// belongsTo associate / dissociate (the owner must still be saved)
	caio := must(orm.New[eUser](db).Find(ctx, 3))
	br := must(orm.New[eCountry](db).Find(ctx, 1))
	ok(t, caio.Country.Associate(br))
	if !caio.IsDirty("country_id") || caio.Country.Value().Name != "BR" {
		t.Fatalf("associate: %+v", caio)
	}
	ok(t, caio.Save(ctx))
	ok(t, caio.Country.Dissociate())
	ok(t, caio.Save(ctx))
	if must(orm.New[eUser](db).Find(ctx, 3)).CountryID != nil {
		t.Fatal("dissociate must null the FK")
	}
}

func TestPivotWrites(t *testing.T) {
	db, ctx := eSeed(t)
	caio := must(orm.New[eUser](db).Find(ctx, 3))
	admin, editor := must(orm.New[eRole](db).Find(ctx, 1)), must(orm.New[eRole](db).Find(ctx, 2))
	have := func() int64 {
		n, _ := must(caio.Roles.Query()).Count(ctx)
		return n
	}

	ok(t, caio.Roles.Attach(ctx, admin, 2)) // models and raw keys both work
	ok(t, caio.Roles.Attach(ctx, admin))    // idempotent
	if have() != 2 {
		t.Fatalf("attach: %d", have())
	}
	res := must(caio.Roles.Sync(ctx, editor))
	if len(res.Detached) != 1 || len(res.Attached) != 0 || have() != 1 {
		t.Fatalf("sync: %+v have=%d", res, have())
	}
	res = must(caio.Roles.SyncWithoutDetaching(ctx, admin.ID, editor.ID))
	if len(res.Attached) != 1 || len(res.Detached) != 0 || have() != 2 {
		t.Fatalf("syncWithoutDetaching: %+v", res)
	}
	res = must(caio.Roles.Toggle(ctx, admin, 99))
	if len(res.Detached) != 1 || len(res.Attached) != 1 || have() != 1 { // admin off, 99 on (no such role row, so not counted)
		t.Fatalf("toggle: %+v have=%d", res, have())
	}
	ok(t, caio.Roles.Detach(ctx, 99))
	ok(t, caio.Roles.AttachWith(ctx, map[string]any{"assigned_by": "tester"}, admin))
	ok(t, caio.Roles.UpdateExistingPivot(ctx, editor, map[string]any{"assigned_by": "updated"}))
	loaded := must(orm.New[eUser](db).Query().With("Roles").Where("id = 3").First(ctx))
	by := map[string]any{}
	for _, r := range loaded.Roles.Items() {
		by[r.Name] = r.Extra("pivot_assigned_by")
	}
	if by["admin"] != "tester" || by["editor"] != "updated" {
		t.Fatalf("pivot columns: %v", by)
	}
	ok(t, caio.Roles.Detach(ctx))
	if have() != 0 {
		t.Fatalf("detach all: %d", have())
	}
	// attach invalidates a loaded relation
	if caio.Roles.Loaded() {
		t.Fatal("pivot writes must mark the relation unloaded")
	}

	// morphToMany keeps the type column in every operation
	p3 := must(orm.New[ePost](db).Find(ctx, 3))
	ok(t, p3.Tags.Attach(ctx, 1, 3))
	res = must(p3.Tags.Sync(ctx, 2))
	if len(res.Detached) != 2 || len(res.Attached) != 1 {
		t.Fatalf("morph sync: %+v", res)
	}
	var videoLinks int64
	db.Gorm.Table("e_taggables").Where("taggable_type = 'e_video'").Count(&videoLinks)
	if videoLinks != 1 {
		t.Fatalf("morph pivot leaked across types: %d", videoLinks)
	}
	tag := must(orm.New[eTag](db).Find(ctx, 3))
	ok(t, tag.Posts.Attach(ctx, p3))
	if n, _ := must(p3.Tags.Query()).Count(ctx); n != 2 {
		t.Fatalf("morphedByMany attach: %d", n)
	}
}

func TestRelationDeclarationErrors(t *testing.T) {
	db, ctx := eSetup(t)
	type noTag struct {
		orm.BaseModel
		Posts orm.HasMany[ePost]
	}
	type noMorph struct {
		orm.BaseModel
		Comments orm.MorphMany[eComment] `gorm:"-"`
	}
	for name, run := range map[string]func() error{
		"missing gorm tag": func() error { _, err := orm.New[noTag](db).Query().With("Posts").Get(ctx); return err },
		"missing morph":    func() error { _, err := orm.New[noMorph](db).Query().With("Comments").Get(ctx); return err },
	} {
		if err := run(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	// an unregistered morph alias reports the alias
	db.Gorm.Exec("INSERT INTO e_comments (commentable_type, commentable_id, body) VALUES ('ghost', 1, 'x')")
	_, err := orm.New[eComment](db).Query().With("Commentable").Get(ctx)
	if migro.CodeOf(err) != migro.UnknownMorphType {
		t.Fatalf("unknown morph: %v", err)
	}
}
