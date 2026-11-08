package query_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/query"
	"github.com/LouisRiverstone/migro/schema"
)

func TestBuilder(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	if err := schema.New(db).CreateTable(ctx, "q_items", func(t *schema.Table) {
		t.Increments("id")
		t.String("name")
		t.Integer("qty")
	}); err != nil {
		t.Fatal(err)
	}
	items := func() *query.Builder { return query.Table(db, "q_items") }
	if err := items().Insert(ctx, query.Row{"name": "a", "qty": 1}, query.Row{"name": "b", "qty": 5}, query.Row{"name": "c", "qty": 9}); err != nil {
		t.Fatal(err)
	}
	rows, err := items().Where("qty > ?", 2).OrderBy("qty", "desc").Get(ctx)
	if err != nil || len(rows) != 2 || rows[0]["name"] != "c" {
		t.Fatalf("get: %v %v", rows, err)
	}
	if n, _ := items().WhereIn("name", []string{"a", "b"}).Count(ctx); n != 2 {
		t.Fatalf("whereIn count=%d", n)
	}
	if n, _ := items().WhereBetween("qty", 4, 10).Count(ctx); n != 2 {
		t.Fatalf("between count=%d", n)
	}
	if _, err := items().Where("name = ?", "zzz").First(ctx); !errors.Is(err, migro.ErrNotFound) {
		t.Fatalf("want NOT_FOUND, got %v", err)
	}
	if n, err := items().Where("name = ?", "a").Update(ctx, query.Row{"qty": 100}); err != nil || n != 1 {
		t.Fatalf("update %d %v", n, err)
	}
	if _, err := items().Delete(ctx); err == nil {
		t.Fatal("delete without where must fail")
	}
	if n, err := items().Where("name = ?", "b").Delete(ctx); err != nil || n != 1 {
		t.Fatalf("delete %d %v", n, err)
	}
	p, err := items().OrderBy("id", "asc").Paginate(ctx, 1, 1)
	if err != nil || p.Total != 2 || p.LastPage != 2 || len(p.Data) != 1 {
		t.Fatalf("paginate %+v %v", p, err)
	}
	if _, err := items().OrderBy("id", "x").Get(ctx); migro.CodeOf(err) != migro.QueryFailed {
		t.Fatalf("bad order dir: %v", err)
	}
	// builder is reusable after terminal calls
	b := items().Where("qty > ?", 0)
	c1, _ := b.Count(ctx)
	c2, _ := b.Count(ctx)
	if c1 != c2 || c1 != 2 {
		t.Fatalf("builder state leaked: %d %d", c1, c2)
	}
}
