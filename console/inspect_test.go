package console_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/orm"
)

type csPart struct {
	orm.BaseModel
	CsWidgetID uint
	Label      string
}

type csWidget struct {
	orm.SoftDeleteModel
	Name  string `gorm:"unique"`
	Parts orm.HasMany[csPart]
}

func (csWidget) Hidden() []string { return []string{"name"} }

func TestSchemaDumpLoadsOnAFreshDatabase(t *testing.T) {
	h := newHarness(t)
	out := h.mustRun("schema:dump")
	path := filepath.Join(h.app.Root, "database", "schema", "sqlite-schema.sql")
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), `CREATE TABLE "cs_a"`) || !strings.Contains(string(b), "-- migration: 20260101000003_create_cs_c_table") {
		t.Fatalf("dump file: %v\n%s", err, b)
	}
	if !strings.Contains(out, "3 migrations") || len(h.tables()) != 0 {
		t.Fatalf("dumping must not touch the database: %s %v", out, h.tables())
	}

	// the database is still empty, so the dump loads instead of three migrations replaying
	run := h.mustRun("migration:run")
	if !strings.Contains(run, "schema dump") || strings.Contains(run, "20260101000001_create_cs_a_table") {
		t.Fatalf("run output: %s", run)
	}
	status := h.mustRun("migration:status")
	if strings.Count(status, "Yes") != 3 {
		t.Fatalf("every squashed migration is recorded as applied:\n%s", status)
	}
	if pre := h.mustRun("migration:run", "--pretend"); !strings.Contains(pre, "Nothing to migrate") {
		t.Fatalf("nothing left to run: %s", pre)
	}
}

func TestRunSeedWipeShowTable(t *testing.T) {
	h := newHarness(t)
	h.mustRun("migration:run", "--seed")
	db, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: h.dbPath})
	if err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Gorm.Table("cs_a").Count(&n)
	_ = db.Close()
	if n != 1 {
		t.Fatalf("--seed must seed after migrating: %d", n)
	}

	show := h.mustRun("db:show", "--counts")
	if !strings.Contains(show, "Dialect:    sqlite") || !strings.Contains(show, "cs_a") || !strings.Contains(show, "1 rows") {
		t.Fatalf("db:show:\n%s", show)
	}
	table := h.mustRun("db:table", "cs_a")
	if !strings.Contains(table, "name") || !strings.Contains(table, "primary") || !strings.Contains(table, "Indexes:") {
		t.Fatalf("db:table:\n%s", table)
	}
	if code := h.run("db:table", "ghost"); code == 0 || !strings.Contains(h.err.String(), "does not exist") {
		t.Fatalf("unknown table: %d %s", code, h.err)
	}

	h.mustRun("db:wipe")
	got := h.tables()
	delete(got, "sqlite_sequence") // SQLite's own bookkeeping table
	if len(got) != 0 {
		t.Fatalf("db:wipe left %v", got)
	}
	if prune := h.mustRun("model:prune"); !strings.Contains(prune, "No prunable") && !strings.Contains(prune, "pruned") {
		t.Fatalf("model:prune: %s", prune)
	}
}

func TestModelShow(t *testing.T) {
	h := newHarness(t)
	h.app.Models = []any{csWidget{}}
	out := h.mustRun("model:show", "csWidget")
	for _, want := range []string{"Model: csWidget", "Soft deletes: true", "Behaviours: Hidden", "name", "unique", "parts", "has_many", "csPart"} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(want)) {
			t.Fatalf("model:show lacks %q:\n%s", want, out)
		}
	}
	if code := h.run("model:show", "Nope"); code == 0 || !strings.Contains(h.err.String(), "csWidget") {
		t.Fatalf("unknown model should list the known ones: %d %s", code, h.err)
	}
}
