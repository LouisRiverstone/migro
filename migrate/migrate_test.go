package migrate_test

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

func goMigs() []migrate.Migration {
	return []migrate.Migration{
		{
			Name: "001_create_mg_a",
			Up: func(ctx context.Context, s *schema.Builder) error {
				return s.CreateTable(ctx, "mg_a", func(t *schema.Table) { t.Increments("id"); t.String("n") })
			},
			Down: func(ctx context.Context, s *schema.Builder) error { return s.DropTable(ctx, "mg_a") },
		},
		{
			Name: "002_create_mg_b",
			Up: func(ctx context.Context, s *schema.Builder) error {
				return s.CreateTable(ctx, "mg_b", func(t *schema.Table) { t.Increments("id") })
			},
			Down: func(ctx context.Context, s *schema.Builder) error { return s.DropTable(ctx, "mg_b") },
		},
	}
}

func newMigrator(t *testing.T, ms ...migrate.Migration) (*migrate.Migrator, *schema.Builder) {
	t.Helper()
	db := testdb.Open(t)
	m := migrate.New(db)
	if err := m.Add(ms...); err != nil {
		t.Fatal(err)
	}
	return m, schema.New(db)
}

func TestLatestRollbackReset(t *testing.T) {
	ctx := context.Background()
	m, s := newMigrator(t, goMigs()...)

	res, err := m.Latest(ctx)
	if err != nil || len(res.Names) != 2 || res.Batch != 1 {
		t.Fatalf("latest: %+v %v", res, err)
	}
	if ok, _ := s.HasTable(ctx, "mg_b"); !ok {
		t.Fatal("mg_b missing")
	}
	again, err := m.Latest(ctx)
	if err != nil || len(again.Names) != 0 {
		t.Fatalf("second latest should be a no-op: %+v %v", again, err)
	}
	st, _ := m.Status(ctx)
	if len(st) != 2 || !st[0].Applied || !st[1].Applied {
		t.Fatalf("status: %+v", st)
	}
	rb, err := m.Rollback(ctx)
	if err != nil || len(rb.Names) != 2 || rb.Names[0] != "002_create_mg_b" {
		t.Fatalf("rollback order newest-first: %+v %v", rb, err)
	}
	if ok, _ := s.HasTable(ctx, "mg_a"); ok {
		t.Fatal("mg_a should be dropped")
	}
	if _, err := m.Latest(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Reset(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestBatchesRollbackOnlyLast(t *testing.T) {
	ctx := context.Background()
	all := goMigs()
	m, _ := newMigrator(t, all[0])
	if _, err := m.Latest(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Add(all[1]); err != nil {
		t.Fatal(err)
	}
	r, _ := m.Latest(ctx)
	if r.Batch != 2 {
		t.Fatalf("want batch 2, got %d", r.Batch)
	}
	rb, _ := m.Rollback(ctx)
	if len(rb.Names) != 1 || rb.Names[0] != "002_create_mg_b" {
		t.Fatalf("rollback should only revert last batch: %+v", rb)
	}
}

func TestFailedMigrationReportsAndReleasesLock(t *testing.T) {
	ctx := context.Background()
	bad := migrate.Migration{Name: "001_bad", Up: func(ctx context.Context, s *schema.Builder) error {
		return s.Raw(ctx, "THIS IS NOT SQL")
	}}
	m, _ := newMigrator(t, bad)
	_, err := m.Latest(ctx)
	if migro.CodeOf(err) != migro.MigrationFailed {
		t.Fatalf("want MIGRATION_FAILED, got %v", err)
	}
	// lock must be released so a retry reaches the migration again instead of MIGRATION_LOCKED
	_, err = m.Latest(ctx)
	if errors.Is(err, migro.ErrMigrationLocked) {
		t.Fatal("lock leaked after failure")
	}
}

func TestLockHeldByAnotherRunner(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	m := migrate.New(db)
	_ = m.Add(goMigs()...)
	if _, err := m.Status(ctx); err != nil { // creates tables
		t.Fatal(err)
	}
	if err := db.Gorm.Exec("INSERT INTO migro_migrations_lock (id) VALUES (1)").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := m.Latest(ctx); !errors.Is(err, migro.ErrMigrationLocked) {
		t.Fatalf("want MIGRATION_LOCKED, got %v", err)
	}
	if err := m.Unlock(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Latest(ctx); err != nil {
		t.Fatalf("after unlock: %v", err)
	}
}

func TestChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	mk := func(sum string) *migrate.Migrator {
		m := migrate.New(db)
		_ = m.Add(migrate.Migration{Name: "001_x", Checksum: sum, Up: func(ctx context.Context, s *schema.Builder) error { return nil }})
		return m
	}
	if _, err := mk("aaa").Latest(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := mk("bbb").Latest(ctx); migro.CodeOf(err) != migro.ChecksumMismatch {
		t.Fatalf("want checksum mismatch, got %v", err)
	}
}

func TestDuplicateAndInvalidNames(t *testing.T) {
	m := migrate.New(testdb.Open(t))
	up := func(context.Context, *schema.Builder) error { return nil }
	if err := m.Add(migrate.Migration{Name: "a", Up: up}, migrate.Migration{Name: "a", Up: up}); err == nil {
		t.Fatal("duplicate should fail")
	}
	if err := m.Add(migrate.Migration{Name: "", Up: up}); err == nil {
		t.Fatal("empty name should fail")
	}
}

func TestFromFS(t *testing.T) {
	ctx := context.Background()
	fsys := fstest.MapFS{
		"m/001_t.up.sql":   {Data: []byte("CREATE TABLE fs_t (id integer, note varchar(20)); -- trailing; comment\nINSERT INTO fs_t (id, note) VALUES (1, 'a;b');")},
		"m/001_t.down.sql": {Data: []byte("DROP TABLE fs_t;")},
		"m/README.md":      {Data: []byte("ignored")},
	}
	ms, err := migrate.FromFS(fsys, "m")
	if err != nil || len(ms) != 1 || ms[0].Checksum == "" {
		t.Fatalf("fromfs: %+v %v", ms, err)
	}
	db := testdb.Open(t)
	m := migrate.New(db)
	_ = m.Add(ms...)
	if _, err := m.Latest(ctx); err != nil {
		t.Fatal(err)
	}
	var note string
	if err := db.Gorm.Raw("SELECT note FROM fs_t").Scan(&note).Error; err != nil || note != "a;b" {
		t.Fatalf("quoted semicolon must survive: %q %v", note, err)
	}
	if _, err := m.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSplitStatements(t *testing.T) {
	got := migrate.SplitStatements("a; b -- x;y\n; 'q;r'; ")
	want := []string{"a", "b", "'q;r'"}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q want %q", got, want)
		}
	}
}
