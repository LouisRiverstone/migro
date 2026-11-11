package migro_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/LouisRiverstone/migro"
	_ "github.com/LouisRiverstone/migro/dialect/sqlite"
)

func TestManager(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := func(name string) migro.Config {
		return migro.Config{Dialect: "sqlite", DSN: filepath.Join(dir, name+".db")}
	}
	if _, err := migro.NewManager("missing", map[string]migro.Config{"main": cfg("main")}); migro.CodeOf(err) != migro.ConnectionNotFound {
		t.Fatalf("unknown default: %v", err)
	}
	m, err := migro.NewManager("main", map[string]migro.Config{"main": cfg("main"), "audit": cfg("audit"), "broken": {Dialect: "nope"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })

	def, err := m.Default(ctx)
	if err != nil || def.Name() != "main" {
		t.Fatalf("default: %v %v", def, err)
	}
	same, _ := m.Connection(ctx, "main")
	if same != def {
		t.Fatal("a connection opens once")
	}
	audit, err := m.Connection(ctx, "audit")
	if err != nil || audit == def || audit.Name() != "audit" {
		t.Fatalf("audit: %v %v", audit, err)
	}
	if _, err := m.Connection(ctx, "ghost"); migro.CodeOf(err) != migro.ConnectionNotFound {
		t.Fatalf("unknown connection: %v", err)
	}
	if _, err := m.Connection(ctx, "broken"); err == nil {
		t.Fatal("a broken config only fails when it is used")
	}
	if got := m.Names(); len(got) != 3 || got[0] != "audit" {
		t.Fatalf("names: %v", got)
	}
	if err := m.Purge("audit"); err != nil {
		t.Fatal(err)
	}
	if again, _ := m.Connection(ctx, "audit"); again == audit {
		t.Fatal("purge must force a reopen")
	}
}
