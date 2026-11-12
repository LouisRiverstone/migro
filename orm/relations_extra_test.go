package orm_test

import (
	"context"
	"testing"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
)

type fCountry struct {
	orm.BaseModel
	Name      string
	FirstBook orm.HasOneThrough[fBook, fAuthor] `orm:"foreignKey:country_id;throughForeignKey:author_id"`
}

func (fCountry) TableName() string { return "f_countries" }

type fAuthor struct {
	orm.BaseModel
	CountryID uint
	Name      string
	Books     orm.HasMany[fBook]      `orm:"foreignKey:author_id"`
	Latest    orm.HasOne[fBook]       `orm:"foreignKey:author_id;ofMany:latest"`
	Cheapest  orm.HasOne[fBook]       `orm:"foreignKey:author_id;ofMany:min:price"`
	Tags      orm.BelongsToMany[fTag] `orm:"pivotTable:f_author_tag;pivotForeignKey:author_id;pivotRelatedForeignKey:tag_id;pivotColumns:status;pivotWhere:status=active;pivotAs:link"`
}

func (fAuthor) TableName() string { return "f_authors" }

type fBook struct {
	orm.BaseModel
	AuthorID uint
	Title    string
	Price    int
	Author   orm.BelongsTo[fAuthor] `orm:"foreignKey:author_id;withDefault"`
}

func (fBook) Defaults() map[string]any { return map[string]any{"price": 99} }
func (fBook) TableName() string        { return "f_books" }
func (fBook) Touches() []string        { return []string{"Author"} }

type fTag struct {
	orm.BaseModel
	Name string
}

func (fTag) TableName() string              { return "f_tags" }
func (fTag) SerializeAs() map[string]string { return map[string]string{"name": "label"} }

func fSetup(t *testing.T) (*migro.DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db := testdb.Open(t)
	s := schema.New(db)
	for _, tb := range []struct {
		name string
		fn   func(*schema.Table)
	}{
		{"f_countries", func(t *schema.Table) { t.Increments("id"); t.String("name"); t.Timestamps() }},
		{"f_authors", func(t *schema.Table) { t.Increments("id"); t.Integer("country_id"); t.String("name"); t.Timestamps() }},
		{"f_books", func(t *schema.Table) {
			t.Increments("id")
			t.Integer("author_id")
			t.String("title")
			t.Integer("price")
			t.Timestamps()
		}},
		{"f_tags", func(t *schema.Table) { t.Increments("id"); t.String("name"); t.Timestamps() }},
		{"f_author_tag", func(t *schema.Table) { t.Integer("author_id"); t.Integer("tag_id"); t.String("status") }},
	} {
		ok(t, s.CreateTable(ctx, tb.name, tb.fn))
	}
	ok(t, orm.New[fCountry](db).CreateMany(ctx, []fCountry{{Name: "BR"}, {Name: "PT"}}))
	ok(t, orm.New[fAuthor](db).CreateMany(ctx, []fAuthor{{CountryID: 1, Name: "a1"}, {CountryID: 1, Name: "a2"}, {CountryID: 2, Name: "a3"}}))
	ok(t, orm.New[fBook](db).CreateMany(ctx, []fBook{
		{AuthorID: 1, Title: "b1", Price: 30}, {AuthorID: 1, Title: "b2", Price: 10}, {AuthorID: 1, Title: "b3", Price: 20},
		{AuthorID: 2, Title: "b4", Price: 5},
	}))
	ok(t, orm.New[fTag](db).CreateMany(ctx, []fTag{{Name: "t1"}, {Name: "t2"}, {Name: "t3"}}))
	return db, ctx
}

func TestOfManyHasOneThroughAndWithDefault(t *testing.T) {
	db, ctx := fSetup(t)
	as := must(orm.New[fAuthor](db).Query().With("Latest").With("Cheapest").OrderBy("id", "asc").Get(ctx))
	if l := as[0].Latest.Item(); l == nil || l.Title != "b3" {
		t.Fatalf("a1 latest: %+v", l)
	}
	if c := as[0].Cheapest.Item(); c == nil || c.Title != "b2" {
		t.Fatalf("a1 cheapest: %+v", c)
	}
	if as[1].Latest.Item().Title != "b4" || as[2].Latest.Item() != nil {
		t.Fatalf("a2 / a3: %+v %+v", as[1].Latest.Item(), as[2].Latest.Item())
	}
	// the relation query applies the same choice
	q := must(as[0].Latest.Query())
	if b := must(q.First(ctx)); b.Title != "b3" {
		t.Fatalf("relation query: %+v", b)
	}

	cs := must(orm.New[fCountry](db).Query().With("FirstBook").OrderBy("id", "asc").Get(ctx))
	if b := cs[0].FirstBook.Item(); b == nil || b.Title != "b1" || cs[1].FirstBook.Item() != nil {
		t.Fatalf("HasOneThrough: %+v / %+v", cs[0].FirstBook.Item(), cs[1].FirstBook.Item())
	}
	m := must(orm.ToMap(&cs[0]))
	if _, isMap := m["first_book"].(map[string]any); !isMap {
		t.Fatalf("a one-through relation serializes as an object: %#v", m["first_book"])
	}

	// withDefault: an orphan answers with an empty, unsaved model
	ok(t, orm.New[fBook](db).Create(ctx, &fBook{AuthorID: 999, Title: "orphan"}))
	orphan := must(orm.New[fBook](db).Query().Where("title = ?", "orphan").With("Author").First(ctx))
	if a := orphan.Author.Item(); a == nil || a.Exists() || a.Name != "" {
		t.Fatalf("default author: %+v", a)
	}
	b1 := must(orm.New[fBook](db).Query().Where("title = ?", "b1").With("Author").First(ctx))
	if a := b1.Author.Item(); a == nil || a.Name != "a1" || !a.Exists() {
		t.Fatalf("real author: %+v", a)
	}
}

func TestTouchesParents(t *testing.T) {
	db, ctx := fSetup(t)
	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	reset := func() { db.Gorm.Exec("UPDATE f_authors SET updated_at = ?", old) }
	touched := func() bool { return must(orm.New[fAuthor](db).Find(ctx, 1)).UpdatedAt.After(old) }

	reset()
	ok(t, orm.New[fBook](db).Create(ctx, &fBook{AuthorID: 1, Title: "new", Price: 1}))
	if !touched() {
		t.Fatal("creating a book must touch its author")
	}
	reset()
	book := must(orm.New[fBook](db).FindBy(ctx, "title", "b1"))
	book.Title = "b1 edited"
	ok(t, book.Save(ctx))
	if !touched() {
		t.Fatal("saving must touch")
	}
	reset()
	ok(t, book.Delete(ctx))
	if !touched() {
		t.Fatal("deleting must touch")
	}
	reset()
	other := must(orm.New[fBook](db).FindBy(ctx, "title", "b4")) // author 2
	other.Title = "b4 edited"
	ok(t, other.Save(ctx))
	if touched() {
		t.Fatal("author 1 must not be touched by author 2's book")
	}
}

func TestPivotWhereAndAs(t *testing.T) {
	db, ctx := fSetup(t)
	db.Gorm.Exec("INSERT INTO f_author_tag (author_id, tag_id, status) VALUES (1, 3, 'inactive')")
	a := must(orm.New[fAuthor](db).Find(ctx, 1))
	ok(t, a.Tags.Attach(ctx, 1, 2))

	var statuses []string
	db.Gorm.Raw("SELECT status FROM f_author_tag WHERE author_id = 1 AND tag_id IN (1,2)").Scan(&statuses)
	if len(statuses) != 2 || statuses[0] != "active" || statuses[1] != "active" {
		t.Fatalf("attach must store the pivotWhere value: %v", statuses)
	}

	loaded := must(orm.New[fAuthor](db).Query().With("Tags").Where("id = 1").First(ctx))
	tags := loaded.Tags.Items()
	if len(tags) != 2 {
		t.Fatalf("only active links load: %d", len(tags))
	}
	if tags[0].Extra("link_status") != "active" || tags[0].Extra("pivot_status") != nil {
		t.Fatalf("pivotAs renames the extras: %+v", tags[0].Extras())
	}
	if n := must(orm.New[fAuthor](db).Query().WithCount("Tags").Where("id = 1").First(ctx)); n.Extra("tags_count") != int64(2) {
		t.Fatalf("count honours pivotWhere: %v", n.Extras())
	}
	if n := must(orm.New[fAuthor](db).Query().Has("Tags").Count(ctx)); n != 1 {
		t.Fatalf("Has honours pivotWhere: %d", n)
	}
	q := must(a.Tags.Query())
	if n := must(q.Count(ctx)); n != 2 {
		t.Fatalf("relation query honours pivotWhere: %d", n)
	}

	res, err := a.Tags.Sync(ctx, 1)
	ok(t, err)
	if len(res.Detached) != 1 {
		t.Fatalf("sync: %+v", res)
	}
	var left int64
	db.Gorm.Raw("SELECT COUNT(*) FROM f_author_tag WHERE author_id = 1 AND status = 'inactive'").Scan(&left)
	if left != 1 {
		t.Fatalf("sync must leave links outside the pivotWhere alone: %d", left)
	}
}

func TestSaveManyAndPush(t *testing.T) {
	db, ctx := fSetup(t)
	a := must(orm.New[fAuthor](db).Find(ctx, 2))
	ok(t, a.Books.SaveMany(ctx, []fBook{{Title: "x", Price: 1}, {Title: "y", Price: 2}}))
	if n := must(orm.New[fBook](db).Query().Where("author_id = 2").Count(ctx)); n != 3 {
		t.Fatalf("SaveMany: %d", n)
	}

	book := must(orm.New[fBook](db).Query().Where("title = ?", "b2").With("Author").First(ctx))
	book.Title = "b2 pushed"
	book.Author.Item().Name = "renamed"
	ok(t, orm.Push(ctx, book))
	if got := must(orm.New[fBook](db).FindBy(ctx, "title", "b2 pushed")); got.ID != book.ID {
		t.Fatalf("book not saved: %+v", got)
	}
	if got := must(orm.New[fAuthor](db).Find(ctx, 1)); got.Name != "renamed" {
		t.Fatalf("parent not saved: %+v", got)
	}

	parent := must(orm.New[fAuthor](db).Query().Where("id = 2").With("Books").First(ctx))
	kids := parent.Books.Items()
	kids[0].Title = "kid edited"
	parent.Name = "a2 pushed"
	ok(t, orm.Push(ctx, parent))
	if got := must(orm.New[fBook](db).Find(ctx, kids[0].ID)); got.Title != "kid edited" {
		t.Fatalf("child not saved: %+v", got)
	}
	if err := orm.Push(ctx, &fCountry{}); migro.CodeOf(err) != migro.UnboundModel {
		t.Fatalf("unbound model: %v", err)
	}
}

func TestEnforceMorphMap(t *testing.T) {
	t.Cleanup(orm.ResetMorphMapForTest)
	db, ctx := eSeed(t)        // rows must exist, or the relation is never resolved
	orm.ResetMorphMapForTest() // eSetup registered ePost
	orm.EnforceMorphMap(map[string]any{"e_video": eVideo{}})
	// ePost has a morph relation but is not in the map
	_, err := orm.New[ePost](db).Query().WithCount("Comments").Get(ctx)
	if migro.CodeOf(err) != migro.UnknownMorphType {
		t.Fatalf("unmapped model must be refused: %v", err)
	}
}
