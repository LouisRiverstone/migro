package orm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/orm"
)

// ---- no N+1 -----------------------------------------------------------------------------

// Eager loading is the library's central promise: the number of statements must not depend on
// how many rows come back.
func TestEagerLoadingQueryCountIsConstant(t *testing.T) {
	db, ctx := eSeed(t)

	load := func() int {
		return countQueries(db, func() {
			posts := must(orm.New[ePost](db).Query().
				With("Author").With("Comments").With("Tags").WithCount("Comments").
				OrderBy("id", "asc").Get(ctx))
			for _, p := range posts { // touching loaded relations must not query again
				_, _ = p.Author.Get(ctx)
				_ = p.Comments.Items()
			}
		})
	}
	small := load()

	extra := make([]ePost, 40)
	for i := range extra {
		extra[i] = ePost{UserID: uint(i%3) + 1, Title: "bulk"}
	}
	ok(t, orm.New[ePost](db).CreateMany(ctx, extra))
	large := load()

	if small != large {
		t.Fatalf("query count grew with the data: %d statements for 3 posts, %d for 43", small, large)
	}
	if small > 6 {
		t.Errorf("eager loading three relations plus a count took %d statements; expected one per relation", small)
	}
}

func countQueries(db *migro.DB, fn func()) int {
	n := 0
	off := db.Listen(func(migro.QueryEvent) { n++ })
	defer off()
	fn()
	return n
}

// ---- identifiers are never spliced into SQL ----------------------------------------------

// outsideQuotes drops every quoted run ("x", `x`, 'x', [x]), honouring the doubled closing character
// that escapes itself, so what is left is SQL the library itself wrote.
func outsideQuotes(sql string) string {
	var b strings.Builder
	rs := []rune(sql)
	var closing rune
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case closing != 0:
			if r != closing {
				continue
			}
			if i+1 < len(rs) && rs[i+1] == closing { // escaped: stays inside the quote
				i++
				continue
			}
			closing = 0
		case r == '"' || r == '`' || r == '\'':
			closing = r
		case r == '[':
			closing = ']'
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Methods that take a column or path name from the caller. Application code should not feed them
// user input, but when it does the name must stay inside identifier quotes.
func TestIdentifierArgumentsAreQuoted(t *testing.T) {
	ctx := context.Background()
	payloads := []string{
		`id; DROP TABLE e_users; --`,
		`id"; DROP TABLE e_users; --`,
		"id`; DROP TABLE e_users; --",
		`id) OR (1=1`,
	}
	type attempt struct {
		name string
		run  func(q *orm.Query[eUser], p string) error
	}
	attempts := []attempt{
		{"WhereIn", func(q *orm.Query[eUser], p string) error { _, err := q.WhereIn(p, []int{1}).Get(ctx); return err }},
		{"WhereNotIn", func(q *orm.Query[eUser], p string) error { _, err := q.WhereNotIn(p, []int{1}).Get(ctx); return err }},
		{"WhereNull", func(q *orm.Query[eUser], p string) error { _, err := q.WhereNull(p).Get(ctx); return err }},
		{"WhereNotNull", func(q *orm.Query[eUser], p string) error { _, err := q.WhereNotNull(p).Get(ctx); return err }},
		{"WhereBetween", func(q *orm.Query[eUser], p string) error { _, err := q.WhereBetween(p, 1, 2).Get(ctx); return err }},
		{"WhereLike", func(q *orm.Query[eUser], p string) error { _, err := q.WhereLike(p, "%a%").Get(ctx); return err }},
		{"WhereColumn", func(q *orm.Query[eUser], p string) error { _, err := q.WhereColumn(p, "=", "id").Get(ctx); return err }},
		{"WhereColumn right", func(q *orm.Query[eUser], p string) error { _, err := q.WhereColumn("id", "=", p).Get(ctx); return err }},
		{"WhereJSON column", func(q *orm.Query[eUser], p string) error { _, err := q.WhereJSON(p, "a", "=", 1).Get(ctx); return err }},
		{"WhereJSON path", func(q *orm.Query[eUser], p string) error {
			_, err := q.WhereJSON("name", p, "=", 1).Get(ctx)
			return err
		}},
		{"WhereJSON operator", func(q *orm.Query[eUser], p string) error {
			_, err := q.WhereJSON("name", "a", p, 1).Get(ctx)
			return err
		}},
		{"OrderBy", func(q *orm.Query[eUser], p string) error { _, err := q.OrderBy(p, "asc").Get(ctx); return err }},
		{"OrderBy direction", func(q *orm.Query[eUser], p string) error { _, err := q.OrderBy("id", p).Get(ctx); return err }},
		{"OrderByDesc", func(q *orm.Query[eUser], p string) error { _, err := q.OrderByDesc(p).Get(ctx); return err }},
		{"Latest", func(q *orm.Query[eUser], p string) error { _, err := q.Latest(p).Get(ctx); return err }},
		{"Oldest", func(q *orm.Query[eUser], p string) error { _, err := q.Oldest(p).Get(ctx); return err }},
		{"GroupBy", func(q *orm.Query[eUser], p string) error { _, err := q.GroupBy(p).Get(ctx); return err }},
		{"Sum", func(q *orm.Query[eUser], p string) error { _, err := q.Sum(ctx, p); return err }},
		{"Max", func(q *orm.Query[eUser], p string) error { _, err := q.Max(ctx, p); return err }},
		{"FirstWhere", func(q *orm.Query[eUser], p string) error { _, err := q.FirstWhere(ctx, p, 1); return err }},
	}
	for _, a := range attempts {
		for _, p := range payloads {
			t.Run(a.name, func(t *testing.T) {
				db, _ := eSeed(t) // a fresh database per attempt: one successful injection must not hide the rest
				var seen []string
				off := db.Listen(func(e migro.QueryEvent) { seen = append(seen, e.SQL) })
				_ = a.run(orm.New[eUser](db).Query(), p) // an error is the right outcome; what matters is the SQL
				off()
				for _, sql := range seen {
					rest := strings.ToUpper(outsideQuotes(sql))
					if strings.Contains(rest, "DROP") || strings.Contains(rest, "OR (1=1") || strings.Contains(rest, "--") {
						t.Errorf("payload %q escaped identifier quoting:\n%s", p, sql)
					}
				}
				if n, err := orm.New[eUser](db).Query().Count(context.Background()); err != nil || n != 3 {
					t.Fatalf("e_users damaged after %s(%q): %d rows, %v", a.name, p, n, err)
				}
			})
		}
	}
}
