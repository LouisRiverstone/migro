package orm_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LouisRiverstone/migro"
	_ "github.com/LouisRiverstone/migro/dialect/sqlite"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
)

type Author struct {
	orm.BaseModel
	Name  string
	Books orm.HasMany[Book] `orm:"foreignKey:author_id"`
}

type Book struct {
	orm.BaseModel
	AuthorID uint
	Title    string
	Author   orm.BelongsTo[Author] `orm:"foreignKey:author_id"`
}

// exampleDB opens a throwaway SQLite database with the two tables above.
func exampleDB() (*migro.DB, func()) {
	dir, _ := os.MkdirTemp("", "migro-example")
	db, err := migro.Open(migro.Config{Dialect: "sqlite", DSN: filepath.Join(dir, "example.db")})
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	s := schema.New(db)
	must0 := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	must0(s.CreateTable(ctx, "authors", func(t *schema.Table) { t.Increments("id"); t.String("name"); t.Timestamps() }))
	must0(s.CreateTable(ctx, "books", func(t *schema.Table) {
		t.Increments("id")
		t.Integer("author_id")
		t.String("title")
		t.Timestamps()
	}))
	return db, func() { _ = db.Close(); _ = os.RemoveAll(dir) }
}

func ExampleModel_Create() {
	db, done := exampleDB()
	defer done()
	ctx := context.Background()

	authors := orm.New[Author](db)
	a := &Author{Name: "Ursula"}
	if err := authors.Create(ctx, a); err != nil {
		panic(err)
	}
	found, _ := authors.Find(ctx, a.ID)
	fmt.Println(found.Name, found.ID)
	// Output: Ursula 1
}

func ExampleQuery_With() {
	db, done := exampleDB()
	defer done()
	ctx := context.Background()

	authors, books := orm.New[Author](db), orm.New[Book](db)
	a := &Author{Name: "Ursula"}
	_ = authors.Create(ctx, a)
	_ = books.CreateMany(ctx, []Book{{AuthorID: a.ID, Title: "A Wizard of Earthsea"}, {AuthorID: a.ID, Title: "The Dispossessed"}})

	// One query for authors, one for all their books: never one query per author.
	list, _ := authors.Query().With("Books").Get(ctx)
	for _, au := range list {
		for _, b := range au.Books.Items() {
			fmt.Println(au.Name, "-", b.Title)
		}
	}
	// Output:
	// Ursula - A Wizard of Earthsea
	// Ursula - The Dispossessed
}

func ExampleQuery_Paginate() {
	db, done := exampleDB()
	defer done()
	ctx := context.Background()

	books := orm.New[Book](db)
	rows := make([]Book, 25)
	for i := range rows {
		rows[i] = Book{AuthorID: 1, Title: fmt.Sprintf("book %02d", i+1)}
	}
	_ = books.CreateMany(ctx, rows)

	page, _ := books.Query().OrderBy("id", "asc").Paginate(ctx, 2, 10)
	fmt.Println(len(page.Data), page.Total, page.CurrentPage)
	// Output: 10 25 2
}

func ExampleQuery_Has() {
	db, done := exampleDB()
	defer done()
	ctx := context.Background()

	authors, books := orm.New[Author](db), orm.New[Book](db)
	a, b := &Author{Name: "with books"}, &Author{Name: "without books"}
	_ = authors.Create(ctx, a)
	_ = authors.Create(ctx, b)
	_ = books.Create(ctx, &Book{AuthorID: a.ID, Title: "x"})

	list, _ := authors.Query().Has("Books").Get(ctx)
	for _, au := range list {
		fmt.Println(au.Name)
	}
	// Output: with books
}
