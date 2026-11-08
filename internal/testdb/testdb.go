// Package testdb opens the database used by tests.
// Default: a temp-file SQLite. Set MIGRO_TEST_DIALECT and MIGRO_TEST_DSN to target another database.
package testdb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LouisRiverstone/migro"
	_ "github.com/LouisRiverstone/migro/dialect/mysql"
	_ "github.com/LouisRiverstone/migro/dialect/postgres"
	_ "github.com/LouisRiverstone/migro/dialect/sqlite"
)

func Open(t testing.TB) *migro.DB {
	t.Helper()
	cfg := migro.Config{Dialect: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")}
	if d := os.Getenv("MIGRO_TEST_DIALECT"); d != "" {
		cfg = migro.Config{Dialect: d, DSN: os.Getenv("MIGRO_TEST_DSN")}
	}
	db, err := migro.Open(cfg)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if os.Getenv("MIGRO_TEST_DIALECT") != "" {
		dropAll(db) // shared server: start and finish clean
		t.Cleanup(func() { dropAll(db) })
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// dropAll removes every table, retrying so foreign-key order does not matter.
func dropAll(db *migro.DB) {
	m := db.Gorm.Migrator()
	for range 5 {
		tables, err := m.GetTables()
		if err != nil || len(tables) == 0 {
			return
		}
		for _, tbl := range tables {
			_ = m.DropTable(tbl)
		}
	}
}
