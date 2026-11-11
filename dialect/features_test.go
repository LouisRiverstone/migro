package dialect_test

import (
	"strings"
	"testing"

	"github.com/LouisRiverstone/migro/dialect"
	_ "github.com/LouisRiverstone/migro/dialect/mysql"
	_ "github.com/LouisRiverstone/migro/dialect/oracle"
	_ "github.com/LouisRiverstone/migro/dialect/postgres"
	_ "github.com/LouisRiverstone/migro/dialect/sqlite"
	_ "github.com/LouisRiverstone/migro/dialect/sqlserver"
)

func features(t *testing.T, name string) dialect.Features {
	t.Helper()
	d, err := dialect.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	return dialect.FeaturesOf(d)
}

func TestLocks(t *testing.T) {
	cases := []struct {
		dialect string
		mode    dialect.LockMode
		want    string
	}{
		{"postgres", dialect.LockUpdate, "FOR UPDATE"}, {"postgres", dialect.LockShare, "FOR SHARE"},
		{"mysql", dialect.LockUpdate, "FOR UPDATE"}, {"mysql", dialect.LockShare, "LOCK IN SHARE MODE"},
		{"mariadb", dialect.LockShare, "LOCK IN SHARE MODE"},
		{"oracle", dialect.LockUpdate, "FOR UPDATE"}, {"oracle", dialect.LockShare, ""},
		{"sqlserver", dialect.LockUpdate, ""}, {"redshift", dialect.LockUpdate, ""},
	}
	for _, c := range cases {
		if got := features(t, c.dialect).LockSQL(c.mode); got != c.want {
			t.Errorf("%s %s: got %q want %q", c.dialect, c.mode, got, c.want)
		}
	}
}

func TestJSONAndDateAndFullText(t *testing.T) {
	path := []string{"address", "0", "city"}
	json := map[string]string{
		"mysql":     "JSON_UNQUOTE(JSON_EXTRACT(`m`, '$.address[0].city'))",
		"postgres":  "(`m`::jsonb #>> '{address,0,city}')",
		"sqlite":    "json_extract(`m`, '$.address[0].city')",
		"sqlserver": "JSON_VALUE(`m`, '$.address[0].city')",
	}
	for name, want := range json {
		if got := features(t, name).JSONExtract("`m`", path); got != want {
			t.Errorf("json %s: got %q want %q", name, got, want)
		}
	}
	date := map[string][2]string{
		"postgres":  {"CAST(c AS DATE)", "EXTRACT(MONTH FROM c)"},
		"sqlite":    {"date(c)", "CAST(strftime('%m', c) AS INTEGER)"},
		"sqlserver": {"CAST(c AS DATE)", "MONTH(c)"},
	}
	for name, want := range date {
		f := features(t, name)
		if f.DatePart("c", dialect.PartDate) != want[0] || f.DatePart("c", dialect.PartMonth) != want[1] {
			t.Errorf("date %s: %q %q", name, f.DatePart("c", dialect.PartDate), f.DatePart("c", dialect.PartMonth))
		}
	}

	cols := []string{`"a"`, `"b"`}
	if got := features(t, "mysql").FullTextMatch(cols, true); !strings.Contains(got, "AGAINST (? IN BOOLEAN MODE)") {
		t.Errorf("mysql boolean: %s", got)
	}
	if got := features(t, "postgres").FullTextMatch(cols, true); !strings.Contains(got, "websearch_to_tsquery('simple', ?)") {
		t.Errorf("postgres boolean: %s", got)
	}
	if got := features(t, "postgres").FullTextMatch(cols, false); !strings.Contains(got, "plainto_tsquery('simple', ?)") {
		t.Errorf("postgres natural: %s", got)
	}
	for _, n := range []string{"sqlite", "sqlserver", "oracle", "redshift"} {
		if features(t, n).FullTextMatch(cols, false) != "" {
			t.Errorf("%s must report no full-text", n)
		}
	}
	if dialect.JSONPathString([]string{"a", "2", "b"}) != "$.a[2].b" {
		t.Error("json path rendering")
	}
}
