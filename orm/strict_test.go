package orm_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/orm"
)

func TestStrictLazyLoading(t *testing.T) {
	db, ctx := eSeed(t)
	users := orm.New[eUser](db)

	u, err := users.Find(ctx, 1)
	ok(t, err)
	if _, err := u.Posts.Get(ctx); err != nil {
		t.Fatalf("strict is off by default: %v", err)
	}

	db.ShouldBeStrict(true)
	u, _ = users.Find(ctx, 1)
	_, err = u.Posts.Get(ctx)
	if migro.CodeOf(err) != migro.LazyLoadingViolation || !strings.Contains(err.Error(), "eUser.Posts") {
		t.Fatalf("lazy load must fail when strict: %v", err)
	}

	// eager loading, Load and Query() stay legal
	u, err = users.Query().With("Posts").Where("id = 1").First(ctx)
	ok(t, err)
	if posts, err := u.Posts.Get(ctx); err != nil || len(posts) != 2 {
		t.Fatalf("eager loaded relation: %v %v", posts, err)
	}
	u, _ = users.Find(ctx, 1)
	ok(t, orm.Load(ctx, db, u, "Posts"))
	if _, err := u.Posts.Get(ctx); err != nil {
		t.Fatalf("explicit Load: %v", err)
	}
	u, _ = users.Find(ctx, 1)
	q, err := u.Posts.Query()
	ok(t, err)
	if n, err := q.Count(ctx); err != nil || n != 2 {
		t.Fatalf("relation query: %d %v", n, err)
	}

	// a model that is not persisted has nothing to lazy load
	fresh := orm.Bind(db, &eUser{Name: "new"})
	if _, err := fresh.Posts.Get(ctx); errors.Is(err, migro.ErrNotFound) || migro.CodeOf(err) == migro.LazyLoadingViolation {
		t.Fatalf("unsaved model must be exempt: %v", err)
	}

	// the handler downgrades the failure to a callback; transactions share the setting
	var seen string
	db.SetStrict(migro.Strict{LazyLoading: true, OnLazyLoad: func(m, r string) { seen = m + "." + r }})
	ok(t, db.Transaction(ctx, func(tx *migro.DB) error {
		u, _ := orm.New[eUser](tx).Find(ctx, 1)
		_, err := u.Posts.Get(ctx)
		return err
	}))
	if seen != "eUser.Posts" {
		t.Fatalf("handler not called: %q", seen)
	}
}

func TestQueryLogListenAndToSQL(t *testing.T) {
	db, ctx := eSeed(t)
	users := orm.New[eUser](db)

	var heard []migro.QueryEvent
	off := db.Listen(func(e migro.QueryEvent) { heard = append(heard, e) })
	db.EnableQueryLog()
	_, err := users.Query().Where("name = ?", "ana").Get(ctx)
	ok(t, err)
	off()
	_, err = users.Query().Get(ctx)
	ok(t, err)
	db.DisableQueryLog()

	if len(heard) != 1 || !strings.Contains(heard[0].SQL, "e_users") || heard[0].Rows != 1 {
		t.Fatalf("listener: %+v", heard)
	}
	if log := db.QueryLog(); len(log) != 2 {
		t.Fatalf("query log: %+v", log)
	}
	db.FlushQueryLog()
	if len(db.QueryLog()) != 0 {
		t.Fatal("flush must empty the log")
	}

	sql, err := users.Query().Where("name = ?", "ana").Limit(3).ToSQL(ctx)
	ok(t, err)
	if !strings.Contains(sql, "ana") || !strings.Contains(sql, "LIMIT") || strings.Contains(sql, "?") {
		t.Fatalf("to sql: %s", sql)
	}
	if len(db.QueryLog()) != 0 {
		t.Fatal("ToSQL must not touch the database")
	}
}
