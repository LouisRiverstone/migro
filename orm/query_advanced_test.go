package orm_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/orm"
	"gorm.io/gorm"
)

func names(users []eUser) []string {
	out := make([]string, len(users))
	for i, u := range users {
		out[i] = u.Name
	}
	return out
}

func TestAdvancedWheres(t *testing.T) {
	db, ctx := eSeed(t)
	users, posts := orm.New[eUser](db), orm.New[ePost](db)

	got := must(users.Query().WhereColumn("id", "=", "country_id").Get(ctx))
	if !slices.Equal(names(got), []string{"ana"}) {
		t.Fatalf("WhereColumn: %v", names(got))
	}
	if _, err := users.Query().WhereColumn("id", "; DROP", "country_id").Get(ctx); migro.CodeOf(err) != migro.QueryFailed {
		t.Fatalf("operator must be validated: %v", err)
	}

	if got := must(users.Query().WhereLike("name", "%a").Get(ctx)); len(got) != 2 { // ana, bia
		t.Fatalf("WhereLike: %v", names(got))
	}
	if n := must(users.Query().WhereYear("created_at", "=", time.Now().Year()).Count(ctx)); n != 3 {
		t.Fatalf("WhereYear: %d", n)
	}
	if n := must(users.Query().WhereDate("created_at", "=", time.Now().Format("2006-01-02")).Count(ctx)); n != 3 {
		t.Fatalf("WhereDate: %d", n)
	}

	withPosts := must(users.Query().WhereExists(posts.Query().WhereColumn("e_posts.user_id", "=", "e_users.id")).Get(ctx))
	if !slices.Equal(names(withPosts), []string{"ana", "bia"}) {
		t.Fatalf("WhereExists: %v", names(withPosts))
	}
	without := must(users.Query().WhereNotExists(posts.Query().WhereColumn("e_posts.user_id", "=", "e_users.id")).Get(ctx))
	if !slices.Equal(names(without), []string{"caio"}) {
		t.Fatalf("WhereNotExists: %v", names(without))
	}
	if got := must(users.Query().WhereRelation("Posts", "title = ?", "p3").Get(ctx)); !slices.Equal(names(got), []string{"bia"}) {
		t.Fatalf("WhereRelation: %v", names(got))
	}

	sql := must(users.Query().WhereJSON("name", "address.city", "=", "Rio").ToSQL(ctx))
	if !strings.Contains(sql, "json_extract") || !strings.Contains(sql, "$.address.city") {
		t.Fatalf("WhereJSON: %s", sql)
	}
	if _, err := users.Query().WhereJSON("name", "a'); DROP TABLE x;--", "=", 1).Get(ctx); migro.CodeOf(err) != migro.QueryFailed {
		t.Fatalf("JSON path must be validated: %v", err)
	}
	if _, err := users.Query().WhereFullText("go", "name").Get(ctx); migro.CodeOf(err) != migro.UnsupportedFeature {
		t.Fatalf("sqlite has no full text: %v", err)
	}
}

func TestSoleFirstWhereLocks(t *testing.T) {
	db, ctx := eSeed(t)
	users := orm.New[eUser](db)

	if u := must(users.Query().Where("name = ?", "ana").Sole(ctx)); u.Email != "ana@x.io" {
		t.Fatalf("Sole: %+v", u)
	}
	if _, err := users.Query().Sole(ctx); migro.CodeOf(err) != migro.MultipleRecords {
		t.Fatalf("several rows: %v", err)
	}
	if _, err := users.Query().Where("name = ?", "zed").Sole(ctx); migro.CodeOf(err) != migro.NotFound {
		t.Fatalf("no rows: %v", err)
	}
	if u := must(users.Query().FirstWhere(ctx, "email", "bia@x.io")); u.Name != "bia" {
		t.Fatalf("FirstWhere: %+v", u)
	}
	// SQLite ignores row locks, so the query still runs
	if n := must(users.Query().LockForUpdate().SharedLock().Count(ctx)); n != 3 {
		t.Fatalf("locks on sqlite: %d", n)
	}
}

func TestGroupHavingDistinctUnion(t *testing.T) {
	db, ctx := eSeed(t)
	users, posts := orm.New[eUser](db), orm.New[ePost](db)

	busy := must(posts.Query().Select("user_id").GroupBy("user_id").Having("COUNT(*) > ?", 1).Get(ctx))
	if len(busy) != 1 || busy[0].UserID != 1 {
		t.Fatalf("Having: %+v", busy)
	}
	if got := must(posts.Query().Distinct("user_id").Get(ctx)); len(got) != 2 {
		t.Fatalf("Distinct: %d", len(got))
	}

	ana := users.Query().Where("name = ?", "ana")
	caio := users.Query().Where("name = ?", "caio")
	u := must(ana.Union(caio).OrderBy("name", "desc").Get(ctx))
	if !slices.Equal(names(u), []string{"caio", "ana"}) {
		t.Fatalf("Union: %v", names(u))
	}
	dup := must(users.Query().Where("name = ?", "ana").Union(users.Query().Where("name = ?", "ana")).Count(ctx))
	all := must(users.Query().Where("name = ?", "ana").UnionAll(users.Query().Where("name = ?", "ana")).Count(ctx))
	if dup != 1 || all != 2 {
		t.Fatalf("Union dedupes, UnionAll keeps: %d %d", dup, all)
	}
}

func TestPaginationVariantsAndStreaming(t *testing.T) {
	db, ctx := eSeed(t)
	users := orm.New[eUser](db)

	p1 := must(users.Query().OrderBy("id", "asc").SimplePaginate(ctx, 1, 2))
	p2 := must(users.Query().OrderBy("id", "asc").SimplePaginate(ctx, 2, 2))
	if len(p1.Data) != 2 || !p1.HasMore || len(p2.Data) != 1 || p2.HasMore {
		t.Fatalf("SimplePaginate: %+v %+v", p1, p2)
	}

	// walking forward over a column with duplicates must visit every row exactly once
	var seen []uint
	cursor := ""
	for range 10 {
		page := must(users.Query().OrderBy("country_id", "asc").CursorPaginate(ctx, 1, cursor))
		for _, u := range page.Data {
			seen = append(seen, u.ID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if !slices.Equal(seen, []uint{1, 2, 3}) {
		t.Fatalf("forward walk: %v", seen)
	}

	first := must(users.Query().OrderBy("name", "asc").CursorPaginate(ctx, 2, ""))
	if !slices.Equal(names(first.Data), []string{"ana", "bia"}) || first.PrevCursor != "" || first.NextCursor == "" {
		t.Fatalf("first page: %+v", first)
	}
	second := must(users.Query().OrderBy("name", "asc").CursorPaginate(ctx, 2, first.NextCursor))
	if !slices.Equal(names(second.Data), []string{"caio"}) || second.NextCursor != "" || second.PrevCursor == "" {
		t.Fatalf("second page: %+v", second)
	}
	back := must(users.Query().OrderBy("name", "asc").CursorPaginate(ctx, 2, second.PrevCursor))
	if !slices.Equal(names(back.Data), []string{"ana", "bia"}) || back.PrevCursor != "" || back.NextCursor == "" {
		t.Fatalf("going back: %+v", back)
	}
	desc := must(users.Query().OrderBy("name", "desc").CursorPaginate(ctx, 2, ""))
	if !slices.Equal(names(desc.Data), []string{"caio", "bia"}) {
		t.Fatalf("desc: %v", names(desc.Data))
	}
	if _, err := users.Query().CursorPaginate(ctx, 2, ""); migro.CodeOf(err) != migro.QueryFailed {
		t.Fatalf("needs an order: %v", err)
	}
	if _, err := users.Query().OrderBy("name", "asc").CursorPaginate(ctx, 2, "garbage"); migro.CodeOf(err) != migro.QueryFailed {
		t.Fatalf("bad cursor: %v", err)
	}
	if _, err := users.Query().OrderBy("id", "asc").CursorPaginate(ctx, 2, first.NextCursor); migro.CodeOf(err) != migro.QueryFailed {
		t.Fatalf("a cursor from another ordering must be refused: %v", err)
	}

	count := 0
	for u, err := range users.Query().OrderBy("id", "asc").Cursor(ctx) {
		ok(t, err)
		if !u.Exists() {
			t.Fatal("cursor rows must be bound")
		}
		count++
	}
	if count != 3 {
		t.Fatalf("Cursor: %d", count)
	}
	var lazy []string
	for u, err := range users.Query().With("Posts").Lazy(ctx, 2) {
		ok(t, err)
		lazy = append(lazy, u.Name)
		if u.Name == "bia" {
			break // early exit must not hang or leak
		}
	}
	if !slices.Equal(lazy, []string{"ana", "bia"}) {
		t.Fatalf("Lazy: %v", lazy)
	}
	var each []uint
	ok(t, users.Query().Each(ctx, 2, func(u *eUser) error { each = append(each, u.ID); return nil }))
	if !slices.Equal(each, []uint{1, 2, 3}) {
		t.Fatalf("Each: %v", each)
	}
	var chunks int
	ok(t, users.Query().ChunkByID(ctx, 2, "id", func(rows []eUser) error { chunks++; return nil }))
	if chunks != 2 {
		t.Fatalf("ChunkByID: %d", chunks)
	}
}

func TestRelationAggregates(t *testing.T) {
	db, ctx := eSeed(t)
	db.Gorm.Exec("UPDATE e_users SET visits = CASE name WHEN 'ana' THEN 10 WHEN 'bia' THEN 5 ELSE 7 END")
	ok(t, orm.New[eCountry](db).Create(ctx, &eCountry{Name: "NZ"}))

	cs := must(orm.New[eCountry](db).Query().OrderBy("id", "asc").
		WithCount("Users").WithSum("Users", "visits").WithAvg("Users", "visits").
		WithMin("Users", "visits").WithMax("Users", "visits").WithExists("Users").Get(ctx))
	br, pt, nz := cs[0], cs[1], cs[2]
	if br.Extra("users_count") != int64(2) || br.Extra("users_sum_visits") != 15.0 || br.Extra("users_avg_visits") != 7.5 ||
		asI(br.Extra("users_min_visits")) != 5 || asI(br.Extra("users_max_visits")) != 10 || br.Extra("users_exists") != true {
		t.Fatalf("BR: %+v", br.Extras())
	}
	if pt.Extra("users_sum_visits") != 7.0 {
		t.Fatalf("PT: %+v", pt.Extras())
	}
	if nz.Extra("users_count") != int64(0) || nz.Extra("users_sum_visits") != 0.0 || nz.Extra("users_avg_visits") != nil ||
		nz.Extra("users_min_visits") != nil || nz.Extra("users_exists") != false {
		t.Fatalf("NZ: %+v", nz.Extras())
	}

	// many-to-many aggregates join through the pivot
	us := must(orm.New[eUser](db).Query().OrderBy("id", "asc").WithMax("Roles", "name").WithExists("Roles").Get(ctx))
	if us[0].Extra("roles_max_name") != "editor" || us[0].Extra("roles_exists") != true || us[2].Extra("roles_exists") != false {
		t.Fatalf("roles: %+v %+v", us[0].Extras(), us[2].Extras())
	}

	// model-level loaders
	c := must(orm.New[eCountry](db).Find(ctx, 1))
	ok(t, orm.LoadSum(ctx, db, c, "Users", "visits"))
	ok(t, orm.LoadExists(ctx, db, c, "Users"))
	ok(t, orm.LoadCount(ctx, db, c, "Users"))
	if c.Extra("users_sum_visits") != 15.0 || c.Extra("users_exists") != true || c.Extra("users_count") != int64(2) {
		t.Fatalf("Load*: %+v", c.Extras())
	}
	if _, err := orm.New[eCountry](db).Query().WithSum("Users", "visits; DROP").Get(ctx); migro.CodeOf(err) != migro.QueryFailed {
		t.Fatalf("column must be validated: %v", err)
	}
}

func asI(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	}
	return -1
}

func TestWhereBelongsToAndMorph(t *testing.T) {
	db, ctx := eSeed(t)
	ana := must(orm.New[eUser](db).Find(ctx, 1))
	got := must(orm.New[ePost](db).Query().WhereBelongsTo(ana).Get(ctx))
	if len(got) != 2 {
		t.Fatalf("WhereBelongsTo: %d", len(got))
	}
	if _, err := orm.New[ePost](db).Query().WhereBelongsTo(&eVideo{}).Get(ctx); migro.CodeOf(err) != migro.InvalidSchema {
		t.Fatalf("no such relation: %v", err)
	}
}

func TestWhereHasMorph(t *testing.T) {
	db, ctx := eSeed(t)
	cs := orm.New[eComment](db)
	onPosts := must(cs.Query().WhereHasMorph("Commentable", []string{"e_post"}, nil).Count(ctx)) // orphan row 99 has no post
	if onPosts != 2 {
		t.Fatalf("posts only: %d", onPosts)
	}
	both := must(cs.Query().WhereHasMorph("Commentable", []string{"e_post", "e_video"}, nil).Count(ctx))
	if both != 3 {
		t.Fatalf("posts or videos: %d", both)
	}
	scoped := must(cs.Query().WhereHasMorph("Commentable", []string{"e_post"}, func(g *gorm.DB) *gorm.DB { return g.Where("title = ?", "p2") }).Count(ctx))
	if scoped != 0 {
		t.Fatalf("scoped: %d", scoped)
	}
	if _, err := cs.Query().WhereHasMorph("Commentable", []string{"ghost"}, nil).Count(ctx); migro.CodeOf(err) != migro.UnknownMorphType {
		t.Fatalf("unknown alias: %v", err)
	}
}
