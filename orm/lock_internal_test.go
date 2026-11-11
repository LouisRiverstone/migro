package orm

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// SQLite ignores row locks, so the clause is checked here on a dry run instead.
func TestLockClauseRendersOnce(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ ID uint }
	for _, sql := range []string{"FOR UPDATE", "FOR SHARE", "LOCK IN SHARE MODE"} {
		var rows []row
		got := db.Model(&row{}).Where("id = ?", 1).Clauses(lockClause(sql)).Find(&rows).Statement.SQL.String()
		if !strings.HasSuffix(got, " "+sql) || strings.Count(got, "FOR") > 1 {
			t.Errorf("%s rendered as %q", sql, got)
		}
	}
}
