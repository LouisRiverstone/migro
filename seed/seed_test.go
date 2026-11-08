package seed_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
	"github.com/LouisRiverstone/migro/seed"
)

type Author struct {
	ID    uint `gorm:"primaryKey"`
	Name  string
	Email string
	Role  string
	Age   int
}
type Book struct {
	ID       uint `gorm:"primaryKey"`
	AuthorID uint
	Title    string
}

func setup(t *testing.T) *migro.DB {
	t.Helper()
	ctx := context.Background()
	db := testdb.Open(t)
	s := schema.New(db)
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.CreateTable(ctx, "authors", func(t *schema.Table) {
		t.Increments("id")
		t.String("name")
		t.String("email")
		t.String("role")
		t.Integer("age")
	}))
	must(s.CreateTable(ctx, "books", func(t *schema.Table) { t.Increments("id"); t.Integer("author_id"); t.String("title") }))
	return db
}

func authors() *seed.Factory[Author] {
	return seed.Define(func(f *seed.Faker) Author {
		return Author{Name: f.Name(), Email: f.Email(), Role: "reader", Age: f.Int(18, 80)}
	}).State("admin", func(a *Author) { a.Role = "admin" }).
		Relation("books", func(ctx context.Context, db *migro.DB, a *Author, n int, f *seed.Faker) error {
			for range n {
				if err := db.Gorm.Create(&Book{AuthorID: a.ID, Title: f.Sentence()}).Error; err != nil {
					return err
				}
			}
			return nil
		})
}

func TestFactoryMakeMergeApplySeed(t *testing.T) {
	f := authors()
	a, err := f.Seed(7).Make()
	if err != nil || a.ID != 0 || !strings.Contains(a.Email, "@example.") {
		t.Fatalf("make: %+v %v", a, err)
	}
	b, _ := f.Seed(7).Make()
	if a != b {
		t.Fatalf("same seed must reproduce: %+v vs %+v", a, b)
	}
	c, _ := f.Seed(7).Apply("admin").Merge(func(x *Author) { x.Name = "Fixed" }).Make()
	if c.Role != "admin" || c.Name != "Fixed" {
		t.Fatalf("state/merge: %+v", c)
	}
	if base, _ := f.Seed(7).Make(); base.Role != "reader" {
		t.Fatal("Apply/Merge must not mutate the shared factory")
	}
	if _, err := f.Apply("ghost").Make(); migro.CodeOf(err) != migro.SeedFailed {
		t.Fatalf("unknown state: %v", err)
	}
	many, _ := f.MakeMany(3)
	if len(many) != 3 {
		t.Fatal("makeMany")
	}
}

func TestFactoryCreateWithRelationsAndHooks(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	hooked := 0
	f := authors().After(func(ctx context.Context, db *migro.DB, a *Author) error { hooked++; return nil })

	got, err := f.Apply("admin").With("books", 2).CreateMany(ctx, db, 3)
	if err != nil || len(got) != 3 || got[0].ID == 0 {
		t.Fatalf("createMany: %+v %v", got, err)
	}
	var books, admins int64
	db.Gorm.Model(&Book{}).Count(&books)
	db.Gorm.Model(&Author{}).Where("role = ?", "admin").Count(&admins)
	if books != 6 || admins != 3 || hooked != 3 {
		t.Fatalf("books=%d admins=%d hooked=%d", books, admins, hooked)
	}

	one, err := authors().Create(ctx, db)
	if err != nil || one.ID == 0 {
		t.Fatal(err)
	}
}

func TestFactoryCreateManyIsAtomic(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	calls := 0
	f := authors().After(func(context.Context, *migro.DB, *Author) error {
		calls++
		if calls == 3 {
			return errors.New("boom")
		}
		return nil
	})
	if _, err := f.CreateMany(ctx, db, 5); migro.CodeOf(err) != migro.SeedFailed {
		t.Fatalf("want SEED_FAILED, got %v", err)
	}
	if n, _ := orm.New[Author](db).Query().Count(ctx); n != 0 {
		t.Fatalf("partial insert leaked: %d rows", n)
	}
	if _, err := authors().CreateMany(ctx, db, 0); err == nil {
		t.Fatal("n=0 should fail")
	}
	if _, err := authors().With("nope", 1).Create(ctx, db); err == nil {
		t.Fatal("unknown relation should fail")
	}
}

func TestRunnerEnvironmentRules(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	var ran []string
	mk := func(name string, envs ...string) seed.Seeder {
		return seed.Seeder{Name: name, Environments: envs, Run: func(context.Context, *migro.DB) error { ran = append(ran, name); return nil }}
	}
	all := []seed.Seeder{mk("b_default"), mk("a_dev_only", "development"), mk("c_prod", "production")}

	for env, want := range map[string]string{
		"development": "a_dev_only,b_default",
		"production":  "c_prod",
		"staging":     "b_default",
	} {
		ran = nil
		r := seed.NewRunner(db, seed.WithEnvironment(env))
		if err := r.Add(all...); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(ran, ","); got != want {
			t.Errorf("env %s: got %q want %q", env, got, want)
		}
	}
}

func TestRunnerOnceTrackingRollbackAndOnly(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	runs := 0
	good := seed.Seeder{Name: "good", Run: func(ctx context.Context, db *migro.DB) error {
		runs++
		return db.Gorm.Create(&Author{Name: "x"}).Error
	}}
	bad := seed.Seeder{Name: "zbad", Run: func(ctx context.Context, db *migro.DB) error {
		_ = db.Gorm.Create(&Author{Name: "half"}).Error
		return errors.New("fail")
	}}
	r := seed.NewRunner(db, seed.WithEnvironment("development"), seed.Once())
	_ = r.Add(good, bad)
	res, err := r.Run(ctx)
	if migro.CodeOf(err) != migro.SeedFailed || len(res.Ran) != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	var n int64
	db.Gorm.Model(&Author{}).Where("name = ?", "half").Count(&n)
	if n != 0 {
		t.Fatal("failed seeder must roll back its writes")
	}
	res, _ = r.RunOnly(ctx, "good")
	if runs != 1 || len(res.Skipped) != 1 {
		t.Fatalf("once: runs=%d res=%+v", runs, res)
	}
	if _, err := r.RunOnly(ctx, "missing"); err == nil {
		t.Fatal("unknown seeder should fail")
	}
	if err := r.Add(good); err == nil {
		t.Fatal("duplicate name should fail")
	}
}

func TestFromFS(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	ss, err := seed.FromFS(fstest.MapFS{
		"s/001_authors.sql": {Data: []byte("INSERT INTO authors (name, email, role, age) VALUES ('a;b', 'x', 'r', 1); -- c")},
		"s/notes.txt":       {Data: []byte("ignored")},
	}, "s")
	if err != nil || len(ss) != 1 {
		t.Fatalf("%v %v", ss, err)
	}
	r := seed.NewRunner(db, seed.WithEnvironment("development"))
	_ = r.Add(ss...)
	if _, err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var name string
	db.Gorm.Raw("SELECT name FROM authors").Scan(&name)
	if name != "a;b" {
		t.Fatalf("got %q", name)
	}
}

func TestFakerBasics(t *testing.T) {
	f := seed.NewFaker(1)
	for range 50 {
		if v := f.Int(3, 5); v < 3 || v > 5 {
			t.Fatalf("int out of range: %d", v)
		}
	}
	if u := f.UUID(); len(u) != 36 || u[14] != '4' {
		t.Fatalf("uuid: %s", u)
	}
	if f.Pick() != "" || f.Int(5, 1) != 5 {
		t.Fatal("edge cases")
	}
	if s := f.Sentence(); !strings.HasSuffix(s, ".") {
		t.Fatal(s)
	}
}

type regSeeder struct{}

func (regSeeder) Run(ctx context.Context, db *migro.DB) error {
	return db.Gorm.Create(&Author{Name: "from-class"}).Error
}

type prodOnly struct{ regSeeder }

func (prodOnly) Environments() []string { return []string{"production"} }

func TestRegisterClasses(t *testing.T) {
	ctx := context.Background()
	db := setup(t)
	seed.ResetRegistry()
	t.Cleanup(seed.ResetRegistry)
	seed.Register(&regSeeder{})
	seed.Register(prodOnly{})

	got := seed.Registered()
	if len(got) != 2 || got[0].Name != "prodOnly" || got[1].Name != "regSeeder" {
		t.Fatalf("names come from the type: %+v", got)
	}
	r := seed.NewRunner(db, seed.WithEnvironment("development"))
	if err := r.AddRegistered(); err != nil {
		t.Fatal(err)
	}
	res, err := r.Run(ctx)
	if err != nil || len(res.Ran) != 1 || res.Ran[0] != "regSeeder" || len(res.Skipped) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
}
