package migro_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/LouisRiverstone/migro"
	_ "github.com/LouisRiverstone/migro/dialect/sqlite"
)

func TestStickyReads(t *testing.T) {
	dir := t.TempDir()
	db, err := migro.Open(migro.Config{
		Dialect:  "sqlite",
		DSN:      filepath.Join(dir, "primary.db"),
		Replicas: []string{filepath.Join(dir, "replica.db")},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// The primary and the replica are separate files, so a read tells which one answered.
	for _, name := range []string{"primary", "replica"} {
		other, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: filepath.Join(dir, name+".db")})
		if err != nil {
			t.Fatal(err)
		}
		if err := other.Gorm.Exec("CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)").Error; err != nil {
			t.Fatal(err)
		}
		_ = other.Close()
	}
	count := func(ctx context.Context) int64 {
		var n int64
		if err := db.Gorm.WithContext(ctx).Raw("SELECT COUNT(*) FROM notes").Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}

	plain := context.Background()
	if err := db.Gorm.WithContext(plain).Exec("INSERT INTO notes (body) VALUES ('a')").Error; err != nil {
		t.Fatal(err)
	}
	if n := count(plain); n != 0 {
		t.Fatalf("without sticky a read goes to the (empty) replica, got %d", n)
	}

	ctx := migro.Sticky(context.Background())
	if n := count(ctx); n != 0 {
		t.Fatalf("before any write a sticky context still reads the replica, got %d", n)
	}
	if err := db.Gorm.WithContext(ctx).Exec("INSERT INTO notes (body) VALUES ('b')").Error; err != nil {
		t.Fatal(err)
	}
	if n := count(ctx); n != 2 {
		t.Fatalf("after a write a sticky context reads the primary, got %d", n)
	}
	if n := count(plain); n != 0 {
		t.Fatalf("other contexts are unaffected, got %d", n)
	}
}
