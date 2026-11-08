package migro_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LouisRiverstone/migro"
	_ "github.com/LouisRiverstone/migro/dialect/sqlite"
)

func TestOpenUnknownDialect(t *testing.T) {
	_, err := migro.Open(migro.Config{Dialect: "nope"})
	if migro.CodeOf(err) != migro.UnsupportedDialect {
		t.Fatalf("got %v", err)
	}
}

func TestOpenAndTransaction(t *testing.T) {
	ctx := context.Background()
	db, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: t.TempDir() + "/a.db"})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	if err := db.Transaction(ctx, func(*migro.DB) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestErrorMatching(t *testing.T) {
	err := migro.Wrap(migro.NotFound, errors.New("x"), "finding")
	if !errors.Is(err, migro.ErrNotFound) || errors.Is(err, migro.ErrMigrationLocked) {
		t.Fatal("errors.Is should match by code only")
	}
}
