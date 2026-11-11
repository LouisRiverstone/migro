package orm_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/dialect"
	_ "github.com/LouisRiverstone/migro/dialect/oracle"
	_ "github.com/LouisRiverstone/migro/dialect/sqlserver"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/glebarez/sqlite"
	gmysql "gorm.io/driver/mysql"
	gpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// deadPool is a connection pool that is never reached: DryRun builds SQL without executing it.
type deadPool struct{}

var errDead = errors.New("dry run only")

func (deadPool) PrepareContext(context.Context, string) (*sql.Stmt, error) { return nil, errDead }
func (deadPool) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errDead
}
func (deadPool) QueryContext(context.Context, string, ...any) (*sql.Rows, error) { return nil, errDead }
func (deadPool) QueryRowContext(context.Context, string, ...any) *sql.Row        { return nil }

// dryDB opens a database for the named dialect that can render SQL but never connects.
func dryDB(t *testing.T, name string) *migro.DB {
	t.Helper()
	d, err := dialect.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	var dial gorm.Dialector
	switch name {
	case "mysql", "mariadb":
		dial = gmysql.New(gmysql.Config{Conn: deadPool{}, SkipInitializeWithVersion: true})
	case "postgres":
		dial = gpostgres.New(gpostgres.Config{Conn: deadPool{}})
	case "oracle":
		// the Oracle driver queries the server on start; SQLite stands in, and only the migro dialect matters
		dial = sqlite.Open(":memory:")
	default:
		dial = d.FromConn(deadPool{})
	}
	g, err := gorm.Open(dial, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return migro.Adopt(g, d)
}

func TestGeneratedSQLPerDialect(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		dialect string
		build   func(*orm.Query[eUser]) *orm.Query[eUser]
		want    []string
	}{
		{"mysql", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.LockForUpdate() }, []string{" FOR UPDATE"}},
		{"mysql", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.SharedLock() }, []string{" LOCK IN SHARE MODE"}},
		{"mariadb", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.SharedLock() }, []string{" LOCK IN SHARE MODE"}},
		{"postgres", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.LockForUpdate() }, []string{" FOR UPDATE"}},
		{"postgres", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.SharedLock() }, []string{" FOR SHARE"}},
		{"mysql", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.WhereJSON("name", "a.b", "=", "x") },
			[]string{"JSON_UNQUOTE(JSON_EXTRACT(`name`, '$.a.b')) = 'x'"}},
		{"postgres", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.WhereJSON("name", "a.b", "=", "x") },
			[]string{`("name"::jsonb #>> '{a,b}') = 'x'`}},
		{"mysql", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.WhereFullText("go", "name", "email") },
			[]string{"MATCH (`name`, `email`) AGAINST ('go' IN NATURAL LANGUAGE MODE)"}},
		{"mysql", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.WhereFullTextBoolean("+go", "name") },
			[]string{"AGAINST ('+go' IN BOOLEAN MODE)"}},
		{"postgres", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.WhereFullText("go", "name", "email") },
			[]string{`to_tsvector('simple', coalesce("name"::text, '') || ' ' || coalesce("email"::text, '')) @@ plainto_tsquery('simple', 'go')`}},
		{"postgres", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.WhereFullTextBoolean("go -sql", "name") },
			[]string{"websearch_to_tsquery('simple', 'go -sql')"}},
		{"mysql", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.WhereYear("created_at", ">=", 2026) },
			[]string{"EXTRACT(YEAR FROM `created_at`) >= 2026"}},
		{"postgres", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.WhereDate("created_at", "=", "2026-01-31") },
			[]string{`CAST("created_at" AS DATE) = '2026-01-31'`}},
		{"sqlserver", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.WhereMonth("created_at", "=", 3) },
			[]string{"MONTH([created_at]) = 3"}},
		{"postgres", func(q *orm.Query[eUser]) *orm.Query[eUser] {
			return q.Where("name = ?", "a").Union(orm.New[eUser](q.DB()).Query().Where("name = ?", "b"))
		}, []string{`FROM (SELECT * FROM "e_users" WHERE name = 'a' AND "e_users"."deleted_at" IS NULL UNION SELECT * FROM "e_users" WHERE name = 'b' AND "e_users"."deleted_at" IS NULL) AS migro_union`}},
		{"mysql", func(q *orm.Query[eUser]) *orm.Query[eUser] {
			return q.GroupBy("country_id").Having("COUNT(*) > ?", 1)
		}, []string{"GROUP BY `country_id` HAVING COUNT(*) > 1"}},
	}
	for _, c := range cases {
		db := dryDB(t, c.dialect)
		q := orm.New[eUser](db).Query()
		sql, err := c.build(q).ToSQL(ctx)
		if err != nil {
			t.Errorf("%s: %v", c.dialect, err)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(sql, w) {
				t.Errorf("%s: SQL lacks %q:\n%s", c.dialect, w, sql)
			}
		}
	}

	// what a database cannot do is refused, not rendered wrong
	refused := []struct {
		dialect string
		build   func(*orm.Query[eUser]) *orm.Query[eUser]
	}{
		{"sqlserver", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.LockForUpdate() }},
		{"oracle", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.SharedLock() }},
		{"redshift", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.WhereFullText("x", "name") }},
		{"oracle", func(q *orm.Query[eUser]) *orm.Query[eUser] { return q.WhereFullText("x", "name") }},
	}
	for _, c := range refused {
		db := dryDB(t, c.dialect)
		if _, err := c.build(orm.New[eUser](db).Query()).ToSQL(ctx); migro.CodeOf(err) != migro.UnsupportedFeature {
			t.Errorf("%s must refuse: %v", c.dialect, err)
		}
	}
}
