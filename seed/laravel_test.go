package seed_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/events"
	"github.com/LouisRiverstone/migro/seed"
)

type step struct {
	name string
	log  *[]string
	fail bool
}

func (s step) Run(ctx context.Context, db *migro.DB) error {
	*s.log = append(*s.log, s.name)
	if s.fail {
		return errors.New("boom")
	}
	return nil
}

func TestCallRunsInOrderAndStopsAtTheFirstError(t *testing.T) {
	db := setup(t)
	var log []string
	err := seed.Call(context.Background(), db, step{"a", &log, false}, step{"b", &log, false})
	if err != nil || len(log) != 2 || log[0] != "a" || log[1] != "b" {
		t.Fatalf("order: %v %v", log, err)
	}
	log = nil
	err = seed.Call(context.Background(), db, step{"a", &log, false}, step{"b", &log, true}, step{"c", &log, false})
	if migro.CodeOf(err) != migro.SeedFailed || len(log) != 2 {
		t.Fatalf("stop at error: %v %v", log, err)
	}
	if !strings.Contains(err.Error(), "seeder step") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error must name the seeder and wrap the cause: %v", err)
	}
}

func TestSeedStartsFromTheRootSeederWhenThereIsOne(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	var log []string
	root := seed.Seeder{Name: seed.RootSeeder, Run: func(ctx context.Context, db *migro.DB) error {
		// the root decides the order, not the alphabet: Zeta before Alpha
		return seed.Call(ctx, db, step{"zeta", &log, false}, step{"alpha", &log, false})
	}}
	alpha := seed.Seeder{Name: "Alpha", Run: func(context.Context, *migro.DB) error { log = append(log, "alpha-direct"); return nil }}

	r := seed.NewRunner(db, seed.WithEnvironment("testing"))
	if err := r.Add(alpha, root); err != nil {
		t.Fatal(err)
	}
	res, err := r.Seed(ctx)
	if err != nil || len(res.Ran) != 1 || res.Ran[0] != seed.RootSeeder || len(log) != 2 || log[0] != "zeta" {
		t.Fatalf("root seed: %+v %v %v", res, err, log)
	}

	// without a root seeder every registered seeder runs, by name
	log = nil
	r2 := seed.NewRunner(db, seed.WithEnvironment("testing"))
	if err := r2.Add(alpha, seed.Seeder{Name: "Beta", Run: func(context.Context, *migro.DB) error { log = append(log, "beta"); return nil }}); err != nil {
		t.Fatal(err)
	}
	res, err = r2.Seed(ctx)
	if err != nil || len(res.Ran) != 2 || log[0] != "alpha-direct" || log[1] != "beta" {
		t.Fatalf("no root: %+v %v %v", res, err, log)
	}
}

func TestRootSeederIsAtomic(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	root := seed.Seeder{Name: seed.RootSeeder, Run: func(ctx context.Context, db *migro.DB) error {
		return seed.Call(ctx, db,
			funcSeeder(func(ctx context.Context, db *migro.DB) error {
				return db.Gorm.WithContext(ctx).Create(&Author{Name: "kept?"}).Error
			}),
			step{"boom", new([]string), true})
	}}
	r := seed.NewRunner(db, seed.WithEnvironment("testing"))
	if err := r.Add(root); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Seed(ctx); migro.CodeOf(err) != migro.SeedFailed {
		t.Fatalf("want SEED_FAILED, got %v", err)
	}
	var n int64
	db.Gorm.Model(&Author{}).Count(&n)
	if n != 0 {
		t.Fatalf("a failing root must roll back everything, %d rows left", n)
	}
}

type funcSeeder func(ctx context.Context, db *migro.DB) error

func (f funcSeeder) Run(ctx context.Context, db *migro.DB) error { return f(ctx, db) }

func TestWithoutEventsMutesLifecycleEvents(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	bus := events.New()
	if err := bus.Attach(db); err != nil {
		t.Fatal(err)
	}
	fired := 0
	bus.On(events.AfterCreate, func(context.Context, events.Event) error { fired++; return nil })
	create := seed.Seeder{Name: "S", Run: func(ctx context.Context, db *migro.DB) error {
		return db.Gorm.WithContext(ctx).Create(&Author{Name: "x"}).Error
	}}

	loud := seed.NewRunner(db, seed.WithEnvironment("testing"))
	_ = loud.Add(create)
	if _, err := loud.Run(ctx); err != nil || fired == 0 {
		t.Fatalf("events should fire by default: fired=%d err=%v", fired, err)
	}
	fired = 0
	quiet := seed.NewRunner(db, seed.WithEnvironment("testing"), seed.WithoutEvents())
	_ = quiet.Add(create)
	if _, err := quiet.Run(ctx); err != nil || fired != 0 {
		t.Fatalf("WithoutEvents: fired=%d err=%v", fired, err)
	}
}

func TestFactorySequenceForAndAfterMaking(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	made := 0
	f := authors().
		Parent("publisher", func(ctx context.Context, db *migro.DB, a *Author, fk *seed.Faker) error {
			a.Role = "child-of-publisher" // stands in for creating a parent row and setting the FK
			return nil
		}).
		AfterMaking(func(a *Author) { made++ })

	seq := f.Sequence(
		func(i int, a *Author) { a.Role = "admin"; a.Age = 10 + i },
		func(i int, a *Author) { a.Role = "editor"; a.Age = 10 + i },
	)
	got, err := seq.MakeMany(5)
	if err != nil || len(got) != 5 || made != 5 {
		t.Fatalf("sequence make: %+v %v made=%d", got, err, made)
	}
	roles := []string{got[0].Role, got[1].Role, got[2].Role, got[3].Role, got[4].Role}
	if roles[0] != "admin" || roles[1] != "editor" || roles[2] != "admin" || roles[4] != "admin" || got[3].Age != 13 {
		t.Fatalf("sequence cycles by index: %v ages %d", roles, got[3].Age)
	}
	// an explicit Merge wins over the sequence
	merged, _ := seq.Merge(func(a *Author) { a.Role = "fixed" }).MakeMany(2)
	if merged[0].Role != "fixed" || merged[1].Role != "fixed" {
		t.Fatalf("merge after sequence: %+v", merged)
	}

	// For runs the parent builder before the insert; Make never does
	child, err := f.For("publisher").Create(ctx, db)
	if err != nil || child.Role != "child-of-publisher" || child.ID == 0 {
		t.Fatalf("for: %+v %v", child, err)
	}
	if m, _ := f.For("publisher").Make(); m.Role == "child-of-publisher" {
		t.Fatal("Make must not run parents")
	}
	if _, err := f.For("ghost").Create(ctx, db); migro.CodeOf(err) != migro.SeedFailed {
		t.Fatalf("unknown parent: %v", err)
	}
	var n int64
	db.Gorm.Model(&Author{}).Count(&n)
	if n != 1 {
		t.Fatalf("failed create must leave nothing behind: %d rows", n)
	}
	// the original factory was not mutated by Sequence / For
	if base, _ := f.Make(); base.Role != "reader" {
		t.Fatalf("shared factory mutated: %+v", base)
	}
}
