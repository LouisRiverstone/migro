package diff_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/migrate/diff"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
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

func TestGoSourceCompilesAndRoundTrips(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	plan, err := diff.Generate(ctx, db, diff.Options{}, &Account{})
	if err != nil {
		t.Fatal(err)
	}
	src, err := plan.GoSource("migrations", "SyncModels")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"func init() { migrate.Register(&SyncModels{}) }", `s.CreateTable(ctx, "accounts"`, `t.String("email", 120).NotNull()`,
		`t.Unique("email").Named("ux_accounts_email")`, `s.DropTableIfExists(ctx, "accounts")`, "BigIncrements"} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
	if _, err := (diff.Plan{}).WriteGoFile(t.TempDir(), "migrations", "x", time.Now()); err != nil {
		t.Fatal(err)
	}
	if diff.TypeName("create_users_table") != "CreateUsersTable" {
		t.Fatal("TypeName")
	}
}

// TestGeneratedFileBuilds drops a generated migration into a throwaway package inside this module and compiles it.
func TestGeneratedFileBuilds(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	ctx := context.Background()
	db := testdb.Open(t)
	plan, _ := diff.Generate(ctx, db, diff.Options{}, &Account{})
	dir, err := os.MkdirTemp("testdata", "gen")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path, err := plan.WriteGoFile(dir, "migrations", "sync_models", time.Now())
	if err != nil || path == "" {
		t.Fatal(path, err)
	}
	cmd := exec.Command(goBin, "vet", "./"+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		src, _ := os.ReadFile(path)
		t.Fatalf("generated migration does not compile: %v\n%s\n%s", err, out, src)
	}
}

type Member struct {
	ID    uint   `gorm:"primaryKey"`
	Email string `gorm:"size:120;uniqueIndex"`
}

// A UNIQUE constraint created by the schema builder satisfies a model's uniqueIndex even though the
// names differ and some drivers do not list constraint-backed indexes.
func TestUniqueConstraintSatisfiesModelUniqueIndex(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	err := schema.New(db).CreateTable(ctx, "members", func(t *schema.Table) {
		t.Increments("id")
		t.String("email", 120).Unique()
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := diff.Generate(ctx, db, diff.Options{}, &Member{})
	if err != nil || !plan.Empty() {
		t.Fatalf("expected no changes, got %+v (%v)", plan.Ops, err)
	}
}

type slim struct {
	ID uint `gorm:"primaryKey"`
}

func (slim) TableName() string { return "wide" }

// Dropping an indexed column must drop the index first, or SQLite and others reject the statement.
func TestDropsIndexBeforeColumn(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	err := schema.New(db).CreateTable(ctx, "wide", func(t *schema.Table) {
		t.Increments("id")
		t.String("tag").Index()
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := diff.Generate(ctx, db, diff.Options{AllowDestructive: true}, &slim{})
	if err != nil || len(plan.Ops) != 2 {
		t.Fatalf("%+v %v", plan.Ops, err)
	}
	if plan.Ops[0].Kind != diff.DropIndex || plan.Ops[1].Kind != diff.DropColumn {
		t.Fatalf("wrong order: %s then %s", plan.Ops[0].Kind, plan.Ops[1].Kind)
	}
	up, _, err := plan.Render()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range up {
		if err := db.Gorm.Exec(s).Error; err != nil {
			t.Fatalf("applying %q: %v", s, err)
		}
	}
}
