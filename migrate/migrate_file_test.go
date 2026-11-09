package migrate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type tableMigration struct{ table string }

func (m tableMigration) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, m.table, func(t *schema.Table) {
		t.Increments("id")
		t.String("name").Index()
	})
}
func (m tableMigration) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, m.table)
}

type pinned struct{ tableMigration }

func (pinned) Connection() string { return "analytics" }

func threeMigrations(t *testing.T, db *migro.DB, opts ...migrate.Option) *migrate.Migrator {
	t.Helper()
	migrate.ResetRegistry()
	t.Cleanup(migrate.ResetRegistry)
	migrate.RegisterAs("20260101000001_create_a_table", tableMigration{"lv_a"})
	migrate.RegisterAs("20260101000002_create_b_table", tableMigration{"lv_b"})
	migrate.RegisterAs("20260101000003_create_c_table", tableMigration{"lv_c"})
	m := migrate.New(db, opts...)
	if err := m.AddRegistered(); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRegisterNamingAndDuplicates(t *testing.T) {
	migrate.ResetRegistry()
	t.Cleanup(migrate.ResetRegistry)
	// Register infers the name from the calling file; this test file is not a timestamped migration.
	migrate.Register(tableMigration{"x"})
	if _, err := migrate.Registered(); migro.CodeOf(err) == "" && err == nil {
		t.Fatal("a non-timestamped file name must be reported")
	}
	migrate.ResetRegistry()
	migrate.RegisterAs("20260101000001_ok", tableMigration{"x"})
	migrate.RegisterAs("20260101000001_ok", tableMigration{"y"})
	if _, err := migrate.Registered(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate not reported: %v", err)
	}
	migrate.ResetRegistry()
	for _, bad := range []string{"create_users", "2026_create", "20260101000001_Upper", "20260101000001"} {
		migrate.RegisterAs(bad, tableMigration{"x"})
	}
	if _, err := migrate.Registered(); err == nil {
		t.Fatal("bad names must be reported")
	}
}

func TestRunStepAndRollbackSteps(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	m := threeMigrations(t, db)

	res, err := m.Run(ctx, migrate.RunOptions{Step: true})
	if err != nil || len(res.Names) != 3 || res.Batch != 3 {
		t.Fatalf("step run: %+v %v", res, err)
	}
	st, _ := m.Status(ctx)
	if st[0].Batch != 1 || st[1].Batch != 2 || st[2].Batch != 3 {
		t.Fatalf("each migration should own a batch: %+v", st)
	}
	rb, err := m.RollbackSteps(ctx, 2)
	if err != nil || len(rb.Names) != 2 || rb.Names[0] != "20260101000003_create_c_table" || rb.Names[1] != "20260101000002_create_b_table" {
		t.Fatalf("rollback 2: %+v %v", rb, err)
	}
	if ok, _ := schema.New(db).HasTable(ctx, "lv_a"); !ok {
		t.Fatal("lv_a must survive")
	}
	if ok, _ := schema.New(db).HasTable(ctx, "lv_b"); ok {
		t.Fatal("lv_b must be gone")
	}
}

func TestRollbackToBatchAndConflictingSelectors(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	m := threeMigrations(t, db)
	if _, err := m.Run(ctx, migrate.RunOptions{Step: true}); err != nil {
		t.Fatal(err)
	}
	rb, err := m.RollbackToBatch(ctx, 1)
	if err != nil || len(rb.Names) != 2 {
		t.Fatalf("to batch 1 keeps batch 1: %+v %v", rb, err)
	}
	all, err := m.RollbackToBatch(ctx, 0)
	if err != nil || len(all.Names) != 1 {
		t.Fatalf("to batch 0: %+v %v", all, err)
	}
	one := 1
	if _, err := m.Revert(ctx, migrate.RevertOptions{Steps: 1, ToBatch: &one}); migro.CodeOf(err) != migro.InvalidConfig {
		t.Fatalf("conflicting selectors: %v", err)
	}
	if _, err := m.Revert(ctx, migrate.RevertOptions{Steps: -1}); migro.CodeOf(err) != migro.InvalidConfig {
		t.Fatalf("negative steps: %v", err)
	}
	if res, err := m.Rollback(ctx); err != nil || len(res.Names) != 0 {
		t.Fatalf("nothing applied: %+v %v", res, err)
	}
}

func TestPretendChangesNothing(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	m := threeMigrations(t, db)

	res, err := m.Run(ctx, migrate.RunOptions{Pretend: true})
	if err != nil || len(res.Names) != 3 {
		t.Fatalf("pretend run: %+v %v", res, err)
	}
	sql := strings.Join(res.SQL["20260101000001_create_a_table"], "\n")
	// identifier quoting differs per database ("lv_a" vs `lv_a`), so compare without quotes
	plain := strings.NewReplacer(`"`, "", "`", "", "[", "", "]", "").Replace(sql)
	if !strings.Contains(plain, "CREATE TABLE lv_a") || !strings.Contains(plain, "CREATE INDEX") {
		t.Fatalf("recorded sql: %s", sql)
	}
	gm := db.Gorm.Migrator()
	if gm.HasTable("lv_a") || gm.HasTable("migro_migrations") || gm.HasTable("migro_migrations_lock") {
		t.Fatal("pretend must not create tables, not even bookkeeping ones")
	}

	if _, err := m.Latest(ctx); err != nil {
		t.Fatal(err)
	}
	down, err := m.Revert(ctx, migrate.RevertOptions{Pretend: true})
	if err != nil || len(down.Names) != 3 || !strings.Contains(strings.Join(down.SQL["20260101000003_create_c_table"], ""), "DROP TABLE") {
		t.Fatalf("pretend rollback: %+v %v", down, err)
	}
	if !db.Gorm.Migrator().HasTable("lv_c") {
		t.Fatal("pretend rollback must not drop anything")
	}
	var applied int64
	db.Gorm.Table("migro_migrations").Count(&applied)
	if applied != 3 {
		t.Fatalf("bookkeeping rows must be untouched, got %d", applied)
	}
}

func TestInstallFreshRefreshAndObserver(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	var events []string
	m := threeMigrations(t, db, migrate.WithObserver(func(e migrate.Event) { events = append(events, string(e.Kind)+":"+e.Name[len(e.Name)-7:]) }))

	if err := m.Install(ctx); err != nil {
		t.Fatal(err)
	}
	if !db.Gorm.Migrator().HasTable("migro_migrations") {
		t.Fatal("install should create the tables")
	}
	if err := m.Install(ctx); err != nil {
		t.Fatalf("install must be idempotent: %v", err)
	}
	if _, err := m.Latest(ctx); err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 || events[0] != "migrating:a_table" || events[1] != "migrated:a_table" {
		t.Fatalf("events: %v", events)
	}

	events = nil
	if res, err := m.Refresh(ctx); err != nil || len(res.Names) != 3 {
		t.Fatalf("refresh: %+v %v", res, err)
	}
	if events[0] != "reverting:c_table" {
		t.Fatalf("refresh reverts newest first: %v", events)
	}

	// Fresh also removes tables the migrations know nothing about.
	db.Gorm.Exec("CREATE TABLE stray (id integer)")
	res, err := m.Fresh(ctx)
	if err != nil || len(res.Names) != 3 {
		t.Fatalf("fresh: %+v %v", res, err)
	}
	if db.Gorm.Migrator().HasTable("stray") {
		t.Fatal("fresh must drop unrelated tables")
	}
}

func TestConnectionFilter(t *testing.T) {
	ctx := context.Background()
	migrate.ResetRegistry()
	t.Cleanup(migrate.ResetRegistry)
	migrate.RegisterAs("20260101000001_create_main_table", tableMigration{"cf_main"})
	migrate.RegisterAs("20260101000002_create_events_table", pinned{tableMigration{"cf_events"}})

	run := func(selected string) []string {
		db := testdb.Open(t)
		m := migrate.New(db, migrate.WithConnection(selected, "default"))
		if err := m.AddRegistered(); err != nil {
			t.Fatal(err)
		}
		res, err := m.Latest(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return res.Names
	}
	if got := run("default"); len(got) != 1 || !strings.HasSuffix(got[0], "main_table") {
		t.Fatalf("default connection: %v", got)
	}
	if got := run("analytics"); len(got) != 1 || !strings.HasSuffix(got[0], "events_table") {
		t.Fatalf("analytics connection: %v", got)
	}
}
