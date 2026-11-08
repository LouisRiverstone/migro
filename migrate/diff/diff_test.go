package diff_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/migrate/diff"
	"github.com/LouisRiverstone/migro/orm"
)

type Account struct {
	orm.SoftDeleteModel
	Email   string  `gorm:"size:120;not null;uniqueIndex:ux_accounts_email"`
	Balance float64 `gorm:"precision:10;scale:2"`
	Active  bool    `gorm:"default:true"`
	Nick    string  `gorm:"index:ix_accounts_nick"`
}

func TestNewTableThenApplyAndReDiffIsEmpty(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)

	plan, err := diff.Generate(ctx, db, diff.Options{}, &Account{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Ops) != 1 || plan.Ops[0].Kind != diff.CreateTable {
		t.Fatalf("plan: %+v", plan.Ops)
	}
	dir := t.TempDir()
	paths, err := plan.WriteFiles(dir, "create accounts!")
	if err != nil || len(paths) != 2 {
		t.Fatalf("write: %v %v", paths, err)
	}
	if !strings.Contains(filepath.Base(paths[0]), "create_accounts_") {
		t.Fatalf("name not sanitised: %s", paths[0])
	}

	// The generated files must load and apply through the normal migrator.
	ms, err := migrate.FromFS(os.DirFS(dir), ".")
	if err != nil {
		t.Fatal(err)
	}
	m := migrate.New(db)
	_ = m.Add(ms...)
	if _, err := m.Latest(ctx); err != nil {
		up, _ := os.ReadFile(paths[0])
		t.Fatalf("apply generated migration: %v\n%s", err, up)
	}
	again, err := diff.Generate(ctx, db, diff.Options{}, &Account{})
	if err != nil || !again.Empty() {
		t.Fatalf("diff after apply should be empty: %+v %v", again.Ops, err)
	}
	// And it must roll back cleanly.
	if _, err := m.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if ok := db.Gorm.Migrator().HasTable("accounts"); ok {
		t.Fatal("rollback should drop accounts")
	}
}

type v1 struct {
	ID   uint `gorm:"primaryKey"`
	Name string
	Old  string
}

func (v1) TableName() string { return "people" }

type v2 struct {
	ID       uint `gorm:"primaryKey"`
	FullName string
	Age      int    `gorm:"not null"`
	City     string `gorm:"index:ix_people_city"`
}

func (v2) TableName() string { return "people" }

func TestAlterAddRenameDropBlocked(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	first, _ := diff.Generate(ctx, db, diff.Options{}, &v1{})
	up, _, err := first.Render()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range up {
		if err := db.Gorm.Exec(s).Error; err != nil {
			t.Fatal(err)
		}
	}

	plan, err := diff.Generate(ctx, db, diff.Options{Renames: map[string]string{"people.name": "full_name"}}, &v2{})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[diff.OpKind]int{}
	for _, op := range plan.Ops {
		kinds[op.Kind]++
	}
	if kinds[diff.RenameColumn] != 1 || kinds[diff.AddColumn] != 2 || kinds[diff.AddIndex] != 1 || kinds[diff.DropColumn] != 0 {
		t.Fatalf("ops: %+v", plan.Ops)
	}
	if len(plan.Blocked) != 1 || plan.Blocked[0].From != "old" {
		t.Fatalf("drop of 'old' must be blocked: %+v", plan.Blocked)
	}
	warned := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "people.age") && strings.Contains(w, "NOT NULL without a default") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("expected NOT NULL warning, got %v", plan.Warnings)
	}

	allowed, _ := diff.Generate(ctx, db, diff.Options{AllowDestructive: true, Renames: map[string]string{"people.name": "full_name"}}, &v2{})
	if len(allowed.Blocked) != 0 {
		t.Fatalf("nothing should be blocked: %+v", allowed.Blocked)
	}
	upSQL, downSQL, err := allowed.Render()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(downSQL, "\n")
	if !strings.Contains(joined, "irreversible") {
		t.Fatalf("down must flag the irreversible drop:\n%s", joined)
	}
	for _, s := range upSQL {
		if strings.HasPrefix(s, "--") {
			continue
		}
		if s2 := strings.Contains(s, "ADD COLUMN \"age\""); s2 {
			s = strings.Replace(s, "NOT NULL", "DEFAULT 0 NOT NULL", 1) // sqlite requires a default for NOT NULL adds
		}
		if err := db.Gorm.Exec(s).Error; err != nil {
			t.Fatalf("apply %q: %v", s, err)
		}
	}
	final, _ := diff.Generate(ctx, db, diff.Options{AllowDestructive: true}, &v2{})
	if !final.Empty() {
		t.Fatalf("expected convergence, still: %+v", final.Ops)
	}
}

func TestEmptyPlanWritesNothing(t *testing.T) {
	var p diff.Plan
	paths, err := p.WriteFiles(t.TempDir(), "x")
	if err != nil || paths != nil {
		t.Fatalf("%v %v", paths, err)
	}
	if _, _, err := (diff.Plan{Ops: []diff.Op{{Kind: diff.CreateTable}}}).Render(); migro.CodeOf(err) != migro.DiffFailed {
		t.Fatalf("render without dialect: %v", err)
	}
}
