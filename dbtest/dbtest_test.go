package dbtest_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/dbtest"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

// recorder turns a failed assertion into a value instead of ending the test.
type recorder struct {
	testing.TB
	msg string
}

type stop struct{}

func (r *recorder) Helper() {}
func (r *recorder) Fatalf(format string, args ...any) {
	r.msg = strings.TrimSpace(sprintf(format, args...))
	panic(stop{})
}

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }

func failure(t *testing.T, fn func(testing.TB)) (msg string) {
	t.Helper()
	r := &recorder{TB: t}
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(stop); !ok {
				panic(p)
			}
			msg = r.msg
		}
	}()
	fn(r)
	return ""
}

func setup(t *testing.T) *migro.DB {
	t.Helper()
	ctx := context.Background()
	db := testdb.Open(t)
	if err := schema.New(db).CreateTable(ctx, "d_items", func(t *schema.Table) {
		t.Increments("id")
		t.String("name")
		t.Timestamp("deleted_at").Nullable()
	}); err != nil {
		t.Fatal(err)
	}
	db.Gorm.Exec("INSERT INTO d_items (name, deleted_at) VALUES ('live', NULL), ('gone', '2026-01-01 00:00:00')")
	return db
}

func TestAssertions(t *testing.T) {
	db := setup(t)
	dbtest.AssertDatabaseHas(t, db, "d_items", map[string]any{"name": "live"})
	dbtest.AssertDatabaseHas(t, db, "d_items", map[string]any{"deleted_at": nil, "name": "live"})
	dbtest.AssertDatabaseMissing(t, db, "d_items", map[string]any{"name": "ghost"})
	dbtest.AssertDatabaseCount(t, db, "d_items", 2)
	dbtest.AssertSoftDeleted(t, db, "d_items", map[string]any{"name": "gone"})
	dbtest.AssertNotSoftDeleted(t, db, "d_items", map[string]any{"name": "live"})

	for name, fn := range map[string]func(testing.TB){
		"has":     func(tb testing.TB) { dbtest.AssertDatabaseHas(tb, db, "d_items", map[string]any{"name": "ghost"}) },
		"missing": func(tb testing.TB) { dbtest.AssertDatabaseMissing(tb, db, "d_items", map[string]any{"name": "live"}) },
		"count":   func(tb testing.TB) { dbtest.AssertDatabaseCount(tb, db, "d_items", 5) },
		"trashed": func(tb testing.TB) { dbtest.AssertSoftDeleted(tb, db, "d_items", map[string]any{"name": "live"}) },
		"live":    func(tb testing.TB) { dbtest.AssertNotSoftDeleted(tb, db, "d_items", map[string]any{"name": "gone"}) },
	} {
		if msg := failure(t, fn); msg == "" {
			t.Errorf("%s: the assertion should have failed", name)
		} else if !strings.Contains(msg, "d_items") {
			t.Errorf("%s: the message should name the table: %s", name, msg)
		}
	}
}

func TestQueryCountTransactionAndRefresh(t *testing.T) {
	db := setup(t)
	n := dbtest.CountQueries(db, func() {
		var c int64
		db.Gorm.Table("d_items").Count(&c)
		db.Gorm.Table("d_items").Count(&c)
	})
	if n != 2 {
		t.Fatalf("CountQueries: %d", n)
	}
	dbtest.AssertQueryCount(t, db, 1, func() { db.Gorm.Exec("SELECT 1") })
	if failure(t, func(tb testing.TB) { dbtest.AssertQueryCount(tb, db, 3, func() {}) }) == "" {
		t.Error("a wrong count should fail")
	}

	t.Run("rolls back", func(t *testing.T) {
		tx := dbtest.Transaction(t, db)
		tx.Gorm.Exec("INSERT INTO d_items (name) VALUES ('temp')")
		dbtest.AssertDatabaseHas(t, tx, "d_items", map[string]any{"name": "temp"})
	})
	dbtest.AssertDatabaseMissing(t, db, "d_items", map[string]any{"name": "temp"})

	dbtest.RefreshDatabase(t, db, []migrate.Migration{{
		Name: "001_create_fresh",
		Up: func(ctx context.Context, s *schema.Builder) error {
			return s.CreateTable(ctx, "d_fresh", func(t *schema.Table) { t.Increments("id") })
		},
	}})
	if has, _ := schema.New(db).HasTable(context.Background(), "d_items"); has {
		t.Fatal("RefreshDatabase must drop what was there")
	}
	dbtest.AssertDatabaseCount(t, db, "d_fresh", 0)
}
