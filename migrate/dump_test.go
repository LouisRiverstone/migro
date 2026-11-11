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

func thirdMig() migrate.Migration {
	return migrate.Migration{
		Name: "003_create_mg_c",
		Up: func(ctx context.Context, s *schema.Builder) error {
			return s.CreateTable(ctx, "mg_c", func(t *schema.Table) { t.Increments("id") })
		},
	}
}

func TestDumpRoundTripAndLoad(t *testing.T) {
	ctx := context.Background()
	src := migrate.New(testdb.Open(t))
	ok(t, src.Add(goMigs()...))

	d, err := src.Dump(ctx)
	ok(t, err)
	if len(d.Migrations) != 2 || len(d.Statements) != 2 || !strings.Contains(d.Statements[0], "mg_a") {
		t.Fatalf("dump: %+v", d)
	}
	if has, _ := schema.New(src.DB()).HasTable(ctx, "mg_a"); has {
		t.Fatal("dumping must not touch the database")
	}

	parsed, err := migrate.ParseDump(d.String())
	ok(t, err)
	if parsed.Dialect != d.Dialect || strings.Join(parsed.Migrations, ",") != strings.Join(d.Migrations, ",") || len(parsed.Statements) != len(d.Statements) {
		t.Fatalf("round trip:\n%s\n%+v", d.String(), parsed)
	}

	// a fresh database gets the dump, then only the migrations the dump does not cover
	db := testdb.Open(t)
	var events []string
	fresh := migrate.New(db, migrate.WithDump(parsed), migrate.WithObserver(func(e migrate.Event) {
		if e.Kind == migrate.Migrated {
			events = append(events, e.Name)
		}
	}))
	ok(t, fresh.Add(append(goMigs(), thirdMig())...))

	pre, err := fresh.Run(ctx, migrate.RunOptions{Pretend: true})
	ok(t, err)
	if len(pre.Names) != 2 || pre.Names[0] != "schema dump" || pre.Names[1] != "003_create_mg_c" {
		t.Fatalf("pretend: %+v", pre)
	}
	if has, _ := schema.New(db).HasTable(ctx, "mg_a"); has {
		t.Fatal("pretend must not load the dump")
	}

	events = nil // the pretend run reports too
	res, err := fresh.Run(ctx, migrate.RunOptions{})
	ok(t, err)
	if len(res.Names) != 2 || res.Names[0] != "schema dump" || res.Names[1] != "003_create_mg_c" {
		t.Fatalf("run: %+v", res)
	}
	for _, tbl := range []string{"mg_a", "mg_b", "mg_c"} {
		if has, _ := schema.New(db).HasTable(ctx, tbl); !has {
			t.Fatalf("%s missing", tbl)
		}
	}
	st, _ := fresh.Status(ctx)
	if len(st) != 3 || !st[0].Applied || st[0].Batch != 1 || !st[2].Applied || st[2].Batch != 2 {
		t.Fatalf("status: %+v", st)
	}
	if strings.Join(events, ",") != "schema dump,003_create_mg_c" {
		t.Fatalf("events: %v", events)
	}
	if again, err := fresh.Run(ctx, migrate.RunOptions{}); err != nil || len(again.Names) != 0 {
		t.Fatalf("a database with history never reloads the dump: %+v %v", again, err)
	}
}

func TestDumpRejectsWrongInput(t *testing.T) {
	ctx := context.Background()
	for _, bad := range []string{"", "CREATE TABLE x (id int);", "-- migro schema dump\nCREATE TABLE x (id int);"} {
		if _, err := migrate.ParseDump(bad); migro.CodeOf(err) != migro.InvalidSchema {
			t.Fatalf("%q must be refused: %v", bad, err)
		}
	}
	m := migrate.New(testdb.Open(t), migrate.WithDump(migrate.Dump{Dialect: "oracle", Statements: []string{"SELECT 1"}}))
	ok(t, m.Add(goMigs()...))
	if _, err := m.Run(ctx, migrate.RunOptions{}); migro.CodeOf(err) != migro.InvalidSchema {
		t.Fatalf("a dump for another dialect: %v", err)
	}
}

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
