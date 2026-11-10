package console_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/console"
	_ "github.com/LouisRiverstone/migro/dialect/sqlite"
	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
	"github.com/LouisRiverstone/migro/seed"
)

type mig struct{ table string }

func (m mig) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, m.table, func(t *schema.Table) { t.Increments("id"); t.String("name").Index() })
}
func (m mig) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, m.table)
}

type seedA struct{}

func (seedA) Run(ctx context.Context, db *migro.DB) error {
	return db.Gorm.Exec("INSERT INTO cs_a (name) VALUES ('seeded')").Error
}

type harness struct {
	t        *testing.T
	app      *console.App
	out, err *bytes.Buffer
	dbPath   string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	migrate.ResetRegistry()
	seed.ResetRegistry()
	t.Cleanup(func() { migrate.ResetRegistry(); seed.ResetRegistry() })
	migrate.RegisterAs("20260101000001_create_cs_a_table", mig{"cs_a"})
	migrate.RegisterAs("20260101000002_create_cs_b_table", mig{"cs_b"})
	migrate.RegisterAs("20260101000003_create_cs_c_table", mig{"cs_c"})
	seed.Register(seedA{})

	h := &harness{t: t, out: &bytes.Buffer{}, err: &bytes.Buffer{}, dbPath: filepath.Join(t.TempDir(), "app.db")}
	open := func(path string) console.Connection {
		return func(ctx context.Context) (*migro.DB, error) {
			return migro.OpenContext(ctx, migro.Config{Dialect: "sqlite", DSN: path})
		}
	}
	h.app = &console.App{
		Root:        t.TempDir(),
		Env:         "development",
		Connections: map[string]console.Connection{"default": open(h.dbPath), "other": open(filepath.Join(t.TempDir(), "other.db"))},
		Out:         h.out, Err: h.err,
		Now: func() time.Time { return time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC) },
	}
	return h
}

func (h *harness) run(args ...string) int {
	h.t.Helper()
	h.out.Reset()
	h.err.Reset()
	return h.app.Run(context.Background(), args)
}

func (h *harness) mustRun(args ...string) string {
	h.t.Helper()
	if code := h.run(args...); code != 0 {
		h.t.Fatalf("%v exited %d\nstdout: %s\nstderr: %s", args, code, h.out, h.err)
	}
	return h.out.String()
}

func (h *harness) tables() map[string]bool {
	h.t.Helper()
	db, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: h.dbPath})
	if err != nil {
		h.t.Fatal(err)
	}
	defer db.Close()
	got := map[string]bool{}
	list, _ := db.Gorm.Migrator().GetTables()
	for _, n := range list {
		got[n] = true
	}
	return got
}

func TestRunStatusRollbackStep(t *testing.T) {
	h := newHarness(t)
	out := h.mustRun("migration:run")
	if !strings.Contains(out, "Migrating:  20260101000001_create_cs_a_table") || !strings.Contains(out, "Migrated:   20260101000003_create_cs_c_table") {
		t.Fatalf("run output:\n%s", out)
	}
	if !h.tables()["cs_c"] {
		t.Fatal("migrations not applied")
	}
	if out := h.mustRun("migration:run"); !strings.Contains(out, "Nothing to migrate") {
		t.Fatalf("second run: %s", out)
	}
	if out := h.mustRun("migration:status"); strings.Count(out, "Yes") != 3 {
		t.Fatalf("status:\n%s", out)
	}

	h.mustRun("migration:rollback", "--step=2")
	tabs := h.tables()
	if !tabs["cs_a"] || tabs["cs_b"] || tabs["cs_c"] {
		t.Fatalf("--step=2 should leave only cs_a: %v", tabs)
	}
	if out := h.mustRun("migration:status"); strings.Count(out, "No") != 2 {
		t.Fatalf("status after rollback:\n%s", out)
	}
	// space-separated value and the laravel alias both work
	h.mustRun("migrate")
	h.mustRun("migrate:rollback", "--step", "1")
	if h.tables()["cs_c"] {
		t.Fatal("cs_c should be rolled back")
	}
}

func TestRunStepAndRollbackBatch(t *testing.T) {
	h := newHarness(t)
	h.mustRun("migration:run", "--step")
	h.mustRun("migration:rollback", "--batch=1")
	tabs := h.tables()
	if !tabs["cs_a"] || tabs["cs_b"] {
		t.Fatalf("--batch=1 keeps batch 1: %v", tabs)
	}
	h.mustRun("migration:reset")
	if h.tables()["cs_a"] {
		t.Fatal("reset should roll back everything")
	}
}

func TestPretendPrintsSQLAndChangesNothing(t *testing.T) {
	h := newHarness(t)
	out := h.mustRun("migration:run", "--pretend")
	if !strings.Contains(out, "-- 20260101000001_create_cs_a_table") || !strings.Contains(out, `CREATE TABLE "cs_a"`) || !strings.Contains(out, "CREATE INDEX") {
		t.Fatalf("pretend output:\n%s", out)
	}
	if h.tables()["cs_a"] || h.tables()["migro_migrations"] {
		t.Fatal("pretend must not touch the database")
	}
	h.mustRun("migration:run")
	out = h.mustRun("migration:rollback", "--pretend")
	if !strings.Contains(out, "DROP TABLE") || !h.tables()["cs_c"] {
		t.Fatalf("pretend rollback:\n%s", out)
	}
}

func TestInstallFreshRefreshSeedUnlock(t *testing.T) {
	h := newHarness(t)
	h.mustRun("migration:install")
	if !h.tables()["migro_migrations"] {
		t.Fatal("install should create tracking tables")
	}
	h.mustRun("migration:fresh", "--seed")
	if !strings.Contains(h.out.String(), "Seeded:   seedA") {
		t.Fatalf("fresh --seed:\n%s", h.out)
	}
	h.mustRun("migration:refresh", "--seed")
	h.mustRun("db:seed", "--class=seedA")
	if code := h.run("db:seed", "--class=Nope"); code != console.ExitError {
		t.Fatalf("unknown class should fail, got %d", code)
	}
	h.mustRun("migration:unlock")
}

func TestProductionGuard(t *testing.T) {
	h := newHarness(t)
	h.app.Env = "production"
	if code := h.run("migration:run"); code != console.ExitError || !strings.Contains(h.err.String(), "--force") {
		t.Fatalf("non-interactive production must refuse: %d %s", code, h.err)
	}
	if h.tables()["cs_a"] {
		t.Fatal("refused command must not run")
	}
	h.mustRun("migration:run", "--pretend") // read-only is never blocked
	h.mustRun("migration:status")

	h.app.Interactive = true
	h.app.In = strings.NewReader("no\n")
	if code := h.run("migration:run"); code != console.ExitError {
		t.Fatalf("answering no must cancel, got %d", code)
	}
	h.app.In = strings.NewReader("yes\n")
	h.mustRun("migration:run")
	if !h.tables()["cs_a"] {
		t.Fatal("answering yes should proceed")
	}
	h.app.Interactive = false
	h.mustRun("migration:rollback", "--force", "--step=1")
	if code := h.run("migration:fresh", "-n"); code != console.ExitError {
		t.Fatalf("fresh in production needs --force, got %d", code)
	}
}

func TestUsageErrorsAndConnections(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{
		{"nope"},
		{"migration:run", "--bogus"},
		{"migration:rollback", "--step=abc"},
		{"migration:rollback", "--step=0"},
		{"migration:rollback", "--step=1", "--batch=1"},
		{"make:migration"},
		{"make:migration", "a", "--create=x", "--table=y"},
		{"migration:diff", "--rename=bad"},
	} {
		if code := h.run(args...); code != console.ExitUsage && (args[0] != "migration:diff" || code != console.ExitError) {
			t.Errorf("%v: want usage exit, got %d (%s)", args, code, h.err)
		}
	}
	if code := h.run("migration:run", "--database=ghost"); code != console.ExitError || !strings.Contains(h.err.String(), "ghost") {
		t.Fatalf("unknown connection: %d %s", code, h.err)
	}
	h.mustRun("migration:run", "--database=other")
	if h.tables()["cs_a"] {
		t.Fatal("--database=other must not touch the default database")
	}
	if !strings.Contains(h.mustRun("help"), "make:migration") {
		t.Fatal("help should list commands")
	}
}

func TestMakeMigrationStubs(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		args           []string
		file, contains string
	}{
		{[]string{"make:migration", "create_users_table"}, "20260304050607_create_users_table.go", `s.CreateTable(ctx, "users"`},
		{[]string{"make:migration", "add_phone_to_users_table"}, "20260304050608_add_phone_to_users_table.go", `s.AlterTable(ctx, "users"`},
		{[]string{"make:migration", "CreatePosts", "--create=articles"}, "20260304050609_create_posts.go", `s.CreateTable(ctx, "articles"`},
		{[]string{"make:migration", "backfill data"}, "20260304050610_backfill_data.go", "return nil"},
	}
	for _, c := range cases {
		h.mustRun(c.args...)
		b, err := os.ReadFile(filepath.Join(h.app.Root, "database/migrations", c.file))
		if err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if !strings.Contains(string(b), c.contains) || !strings.Contains(string(b), "migrate.Register(&") || !strings.Contains(string(b), "package migrations") {
			t.Errorf("%s:\n%s", c.file, b)
		}
	}
	if code := h.run("make:migration", "create_users_table"); code != console.ExitError || !strings.Contains(h.err.String(), "already exists") {
		t.Fatalf("duplicate name: %d %s", code, h.err)
	}
}

func TestMakeMigrationKeepsOrderWithinOneSecond(t *testing.T) {
	h := newHarness(t)
	h.mustRun("make:migration", "create_users_table")
	h.mustRun("make:migration", "add_phone_to_users_table") // sorts before "create" alphabetically
	files, _ := filepath.Glob(filepath.Join(h.app.Root, "database/migrations/*.go"))
	if len(files) != 2 || !strings.Contains(files[0], "create_users") || !strings.Contains(files[1], "add_phone") {
		t.Fatalf("creation order must be preserved by the timestamps: %v", files)
	}
	// a clock with sub-second precision (time.Now) must still bump past a stamp from the same second
	h.app.Now = func() time.Time { return time.Date(2026, 3, 4, 5, 6, 7, 900_000_000, time.UTC) }
	h.mustRun("make:migration", "third")
	if _, err := os.Stat(filepath.Join(h.app.Root, "database/migrations/20260304050609_third.go")); err != nil {
		t.Fatal(err)
	}
	// a later real clock is not pushed around
	h.app.Now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	h.mustRun("make:migration", "later")
	if _, err := os.Stat(filepath.Join(h.app.Root, "database/migrations/20300101000000_later.go")); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedStubsCompile(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	h := newHarness(t)
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("testdata", "gen")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	h.app.Root = root
	h.mustRun("make:migration", "create_widgets_table")
	h.mustRun("make:migration", "add_color_to_widgets_table")
	h.mustRun("make:seeder", "Widget")
	h.mustRun("make:model", "Widget")
	// The factory imports the model package of the host module; point it at the model we just made.
	h.mustRun("make:factory", "Widget", "--model=github.com/LouisRiverstone/migro/console/"+filepath.ToSlash(root)+"/app/models.Widget")
	out, err := exec.Command(goBin, "vet", "./"+root+"/...").CombinedOutput()
	if err != nil {
		t.Fatalf("generated code does not compile: %v\n%s", err, out)
	}
}

func TestMakeModelAll(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(filepath.Join(h.app.Root, "go.mod"), []byte("module example.com/shop\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun("make:model", "Category", "-a")
	for _, f := range []string{
		"app/models/category.go",
		"database/migrations/20260304050607_create_categories_table.go",
		"database/factories/category_factory.go",
		"database/seeders/category_seeder.go",
	} {
		if _, err := os.Stat(filepath.Join(h.app.Root, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	b, _ := os.ReadFile(filepath.Join(h.app.Root, "database/factories/category_factory.go"))
	if !strings.Contains(string(b), `"example.com/shop/app/models"`) || !strings.Contains(string(b), "seed.Factory[models.Category]") {
		t.Fatalf("factory:\n%s", b)
	}
	if code := h.run("make:model", "Category"); code != console.ExitError {
		t.Fatalf("existing file must not be overwritten, got %d", code)
	}
}

type Gadget struct {
	orm.BaseModel
	Label string `gorm:"size:50;not null"`
}

func TestMigrationDiffWritesGoFile(t *testing.T) {
	h := newHarness(t)
	h.app.Models = []any{&Gadget{}}
	out := h.mustRun("migration:diff", "sync_gadgets")
	if !strings.Contains(out, "Created Migration") {
		t.Fatalf("diff output: %s", out)
	}
	b, err := os.ReadFile(filepath.Join(h.app.Root, "database/migrations/20260304050607_sync_gadgets.go"))
	if err != nil || !strings.Contains(string(b), `s.CreateTable(ctx, "gadgets"`) {
		t.Fatalf("%v\n%s", err, b)
	}
	h.app.Models = nil
	if code := h.run("migration:diff"); code != console.ExitError {
		t.Fatalf("no models should fail, got %d", code)
	}
}

func TestDotEnvAndConnections(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	_ = os.WriteFile(env, []byte("# comment\nMIGRO_DIALECT=sqlite\nexport MIGRO_DSN=\"x.db\"\nMIGRO_ANALYTICS_DIALECT=sqlite\nMIGRO_ANALYTICS_DSN='y.db'\n"), 0o644)
	for _, k := range []string{"MIGRO_DIALECT", "MIGRO_DSN", "MIGRO_ANALYTICS_DIALECT", "MIGRO_ANALYTICS_DSN"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	if err := console.LoadDotEnv(env); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("MIGRO_DSN") != "x.db" || os.Getenv("MIGRO_ANALYTICS_DSN") != "y.db" {
		t.Fatal("dotenv not loaded/unquoted")
	}
	t.Setenv("MIGRO_DSN", "already-set.db")
	_ = console.LoadDotEnv(env)
	if os.Getenv("MIGRO_DSN") != "already-set.db" {
		t.Fatal("real environment must win over .env")
	}
	conns := console.ConnectionsFromEnv()
	if _, ok := conns["default"]; !ok {
		t.Fatal("default connection missing")
	}
	if _, ok := conns["analytics"]; !ok {
		t.Fatalf("named connection missing: %v", conns)
	}
	if err := console.LoadDotEnv(filepath.Join(dir, "missing.env")); err != nil {
		t.Fatalf("missing .env is fine: %v", err)
	}
}
