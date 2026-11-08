package orm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
)

type Post struct {
	ID     uint `gorm:"primaryKey"`
	UserID uint
	Title  string
}

type User struct {
	orm.SoftDeleteModel
	Name  string
	Email string
	Posts []Post
}

func setup(t *testing.T) (*migro.DB, *orm.Model[User]) {
	t.Helper()
	ctx := context.Background()
	db := testdb.Open(t)
	s := schema.New(db)
	if err := s.CreateTable(ctx, "users", func(t *schema.Table) {
		t.Increments("id")
		t.String("name")
		t.String("email")
		t.Timestamps()
		t.SoftDeletes()
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTable(ctx, "posts", func(t *schema.Table) {
		t.Increments("id")
		t.Integer("user_id")
		t.String("title")
	}); err != nil {
		t.Fatal(err)
	}
	return db, orm.New[User](db)
}

func TestCRUDAndNotFound(t *testing.T) {
	ctx := context.Background()
	_, users := setup(t)
	u := &User{Name: "Ada", Email: "ada@x.io"}
	if err := users.Create(ctx, u); err != nil || u.ID == 0 {
		t.Fatalf("create: %v id=%d", err, u.ID)
	}
	got, err := users.Find(ctx, u.ID)
	if err != nil || got.Name != "Ada" {
		t.Fatalf("find: %+v %v", got, err)
	}
	got.Name = "Ada L"
	if err := users.Save(ctx, got); err != nil {
		t.Fatal(err)
	}
	by, err := users.FindBy(ctx, "email", "ada@x.io")
	if err != nil || by.Name != "Ada L" {
		t.Fatalf("findBy: %+v %v", by, err)
	}
	if _, err := users.Find(ctx, 9999); !errors.Is(err, migro.ErrNotFound) {
		t.Fatalf("want NOT_FOUND, got %v", err)
	}
}

func TestSoftDeleteAndTrashed(t *testing.T) {
	ctx := context.Background()
	_, users := setup(t)
	u := &User{Name: "Bob"}
	_ = users.Create(ctx, u)
	if err := users.Delete(ctx, u); err != nil {
		t.Fatal(err)
	}
	if _, err := users.Find(ctx, u.ID); !errors.Is(err, migro.ErrNotFound) {
		t.Fatalf("soft-deleted row must be hidden: %v", err)
	}
	with, _ := users.Query().WithTrashed().Count(ctx)
	only, _ := users.Query().OnlyTrashed().Count(ctx)
	if with != 1 || only != 1 {
		t.Fatalf("withTrashed=%d onlyTrashed=%d", with, only)
	}
}

func TestQueryPaginatePreload(t *testing.T) {
	ctx := context.Background()
	db, users := setup(t)
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		_ = users.Create(ctx, &User{Name: n})
	}
	first, _ := users.FindBy(ctx, "name", "a")
	_ = orm.New[Post](db).Create(ctx, &Post{UserID: first.ID, Title: "hello"})

	page, err := users.Query().OrderBy("name", "desc").Paginate(ctx, 2, 2)
	if err != nil || page.Total != 5 || page.LastPage != 3 || len(page.Data) != 2 || page.Data[0].Name != "c" {
		t.Fatalf("paginate: %+v %v", page, err)
	}
	loaded, err := users.Query().Where("name = ?", "a").Preload("Posts").First(ctx)
	if err != nil || len(loaded.Posts) != 1 || loaded.Posts[0].Title != "hello" {
		t.Fatalf("preload: %+v %v", loaded, err)
	}
	if _, err := users.Query().OrderBy("name; DROP TABLE users", "asc").Get(ctx); err != nil {
		// column is quoted, so this is an error from the DB, not an injection
		t.Logf("expected db error: %v", err)
	}
	if _, err := users.Query().OrderBy("name", "sideways").Get(ctx); migro.CodeOf(err) != migro.QueryFailed {
		t.Fatalf("bad direction should fail: %v", err)
	}
	if _, err := users.Query().Paginate(ctx, 1, 0); err == nil {
		t.Fatal("perPage 0 should fail")
	}
}

func TestBulkWritesRequireWhere(t *testing.T) {
	ctx := context.Background()
	_, users := setup(t)
	_ = users.Create(ctx, &User{Name: "x"})
	if _, err := users.Query().Delete(ctx); err == nil {
		t.Fatal("delete without where must fail")
	}
	if _, err := users.Query().Update(ctx, map[string]any{"name": "y"}); err == nil {
		t.Fatal("update without where must fail")
	}
	n, err := users.Query().Where("name = ?", "x").Update(ctx, map[string]any{"name": "z"})
	if err != nil || n != 1 {
		t.Fatalf("update: %d %v", n, err)
	}
}

func TestFirstOrCreateAndTransaction(t *testing.T) {
	ctx := context.Background()
	db, users := setup(t)
	u, created, err := users.FirstOrCreate(ctx, User{Email: "q@x.io"}, User{Name: "Q"})
	if err != nil || !created || u.Name != "Q" {
		t.Fatalf("first create: %+v %v %v", u, created, err)
	}
	_, created, _ = users.FirstOrCreate(ctx, User{Email: "q@x.io"}, User{Name: "ignored"})
	if created {
		t.Fatal("second call must find, not create")
	}
	boom := errors.New("boom")
	err = db.Transaction(ctx, func(tx *migro.DB) error {
		_ = orm.New[User](tx).Create(ctx, &User{Name: "rolled-back"})
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if n, _ := users.Query().Where("name = ?", "rolled-back").Count(ctx); n != 0 {
		t.Fatal("transaction did not roll back")
	}
}
