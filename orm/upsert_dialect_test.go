package orm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/orm"
)

func TestUpsertSQLPerDialect(t *testing.T) {
	ctx := context.Background()
	capture := func(name string, run func(*orm.Model[eUser]) error) ([]string, error) {
		db := dryDB(t, name)
		var got []string
		db.Listen(func(e migro.QueryEvent) { got = append(got, e.SQL) })
		err := run(orm.New[eUser](db))
		return got, err
	}
	up := func(m *orm.Model[eUser]) error {
		return m.Upsert(ctx, []eUser{{Name: "a", Email: "x"}, {Name: "b", Email: "y"}}, []string{"name"}, []string{"email"})
	}

	for name, want := range map[string][]string{
		"mysql":    {"ON DUPLICATE KEY UPDATE"},
		"postgres": {`ON CONFLICT ("name") DO UPDATE SET "email"="excluded"."email"`},
		"sqlserver": {
			`MERGE INTO [e_users] AS t USING (VALUES (`, `) AS s (`, `ON t.[name] = s.[name]`,
			`WHEN MATCHED THEN UPDATE SET t.[email] = s.[email]`, `WHEN NOT MATCHED THEN INSERT (`, `);`,
		},
		"oracle": {"MERGE INTO", " t USING (SELECT ", "FROM dual UNION ALL SELECT", "WHEN MATCHED THEN UPDATE SET", "WHEN NOT MATCHED THEN INSERT"},
	} {
		sql, err := capture(name, up)
		if err != nil || len(sql) != 1 {
			t.Fatalf("%s: %v %v", name, err, sql)
		}
		for _, w := range want {
			if !strings.Contains(sql[0], w) {
				t.Errorf("%s lacks %q:\n%s", name, w, sql[0])
			}
		}
		if (name == "sqlserver" || name == "oracle") && strings.Contains(sql[0], "ON CONFLICT") {
			t.Errorf("%s must not use ON CONFLICT: %s", name, sql[0])
		}
	}
	// the auto-increment key is left out of the MERGE source
	sql, _ := capture("sqlserver", up)
	if strings.Contains(sql[0], "[id]") {
		t.Errorf("auto id in MERGE: %s", sql[0])
	}

	for _, name := range []string{"sqlserver", "oracle", "redshift"} {
		_, err := capture(name, func(m *orm.Model[eUser]) error { return m.InsertOrIgnore(ctx, []eUser{{Name: "a"}}) })
		if migro.CodeOf(err) != migro.UnsupportedFeature {
			t.Errorf("%s InsertOrIgnore should be refused: %v", name, err)
		}
	}
	if _, err := capture("redshift", up); migro.CodeOf(err) != migro.UnsupportedFeature {
		t.Errorf("redshift Upsert should be refused: %v", err)
	}
}
