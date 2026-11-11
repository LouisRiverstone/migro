// Package dbtest has test helpers for code that uses migro: database assertions, a database that
// rolls back when the test ends, and a query counter (Laravel's assertDatabaseHas, RefreshDatabase and
// assertQueryCount).
package dbtest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/migrate"
	"gorm.io/gorm"
)

func describe(where map[string]any) string {
	keys := make([]string, 0, len(where))
	for k := range where {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%v", k, where[k])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func count(t testing.TB, db *migro.DB, table string, where map[string]any, scope func(*gorm.DB) *gorm.DB) int64 {
	t.Helper()
	g := db.Gorm.Table(table)
	if scope != nil {
		g = scope(g)
	}
	var n int64
	if err := g.Where(where).Count(&n).Error; err != nil {
		t.Fatalf("dbtest: counting rows of %s: %v", table, err)
	}
	return n
}

// AssertDatabaseHas fails unless table has a row matching every column in where. A nil value matches NULL.
func AssertDatabaseHas(t testing.TB, db *migro.DB, table string, where map[string]any) {
	t.Helper()
	if count(t, db, table, where, nil) == 0 {
		t.Fatalf("expected %s to have a row matching %s, found none (%d rows in the table)", table, describe(where), count(t, db, table, nil, nil))
	}
}

// AssertDatabaseMissing fails when table has a row matching where.
func AssertDatabaseMissing(t testing.TB, db *migro.DB, table string, where map[string]any) {
	t.Helper()
	if n := count(t, db, table, where, nil); n > 0 {
		t.Fatalf("expected %s to have no row matching %s, found %d", table, describe(where), n)
	}
}

// AssertDatabaseCount fails unless table has exactly want rows.
func AssertDatabaseCount(t testing.TB, db *migro.DB, table string, want int64) {
	t.Helper()
	if got := count(t, db, table, nil, nil); got != want {
		t.Fatalf("expected %s to have %d rows, found %d", table, want, got)
	}
}

// AssertSoftDeleted fails unless a row matching where exists with deleted_at set.
func AssertSoftDeleted(t testing.TB, db *migro.DB, table string, where map[string]any) {
	t.Helper()
	trashed := func(g *gorm.DB) *gorm.DB { return g.Where(db.Dialect.Quote("deleted_at") + " IS NOT NULL") }
	if count(t, db, table, where, trashed) == 0 {
		t.Fatalf("expected a soft-deleted row in %s matching %s, found none", table, describe(where))
	}
}

// AssertNotSoftDeleted fails unless a row matching where exists with deleted_at NULL.
func AssertNotSoftDeleted(t testing.TB, db *migro.DB, table string, where map[string]any) {
	t.Helper()
	live := func(g *gorm.DB) *gorm.DB { return g.Where(db.Dialect.Quote("deleted_at") + " IS NULL") }
	if count(t, db, table, where, live) == 0 {
		t.Fatalf("expected a live row in %s matching %s, found none", table, describe(where))
	}
}

// AssertQueryCount runs fn and fails unless it ran exactly want statements.
func AssertQueryCount(t testing.TB, db *migro.DB, want int, fn func()) {
	t.Helper()
	if got := CountQueries(db, fn); got != want {
		t.Fatalf("expected %d queries, ran %d", want, got)
	}
}

// CountQueries returns how many statements fn ran on db.
func CountQueries(db *migro.DB, fn func()) int {
	n := 0
	off := db.Listen(func(migro.QueryEvent) { n++ })
	defer off()
	fn()
	return n
}

// RefreshDatabase drops every table and migrates from scratch, so the test starts from a known schema
// (Laravel's RefreshDatabase). Use it on a database reserved for tests.
func RefreshDatabase(t testing.TB, db *migro.DB, migrations []migrate.Migration, opts ...migrate.Option) {
	t.Helper()
	m := migrate.New(db, opts...)
	if err := m.Add(migrations...); err != nil {
		t.Fatalf("dbtest: add migrations: %v", err)
	}
	if _, err := m.Fresh(context.Background()); err != nil {
		t.Fatalf("dbtest: refresh database: %v", err)
	}
}

// Transaction returns a database inside a transaction that rolls back when the test ends, so each test
// leaves the data as it found it (Laravel's DatabaseTransactions). DDL that commits implicitly, as on
// MySQL, escapes the rollback.
func Transaction(t testing.TB, db *migro.DB) *migro.DB {
	t.Helper()
	tx := db.Gorm.Begin()
	if tx.Error != nil {
		t.Fatalf("dbtest: begin: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	return db.WithGorm(tx)
}
