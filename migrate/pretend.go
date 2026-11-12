package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
)

// recorder is a ConnPool that records write statements instead of running them.
// Reads pass through so a migration can still inspect the database.
type recorder struct {
	base gorm.ConnPool
	d    gorm.Dialector
	mu   sync.Mutex
	sql  []string
}

type noResult struct{}

func (noResult) LastInsertId() (int64, error) { return 0, nil }
func (noResult) RowsAffected() (int64, error) { return 0, nil }

var _ driver.Result = noResult{}

func readOnly(q string) bool {
	q = strings.ToUpper(strings.TrimSpace(q))
	for _, p := range []string{"SELECT", "SHOW", "PRAGMA", "DESCRIBE", "EXPLAIN"} {
		if strings.HasPrefix(q, p) {
			return true
		}
	}
	return false
}

func (r *recorder) record(q string, args []any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sql = append(r.sql, r.d.Explain(q, args...))
}

func (r *recorder) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	return r.base.PrepareContext(ctx, q)
}

func (r *recorder) ExecContext(_ context.Context, q string, args ...any) (sql.Result, error) {
	r.record(q, args)
	return noResult{}, nil
}

func (r *recorder) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if readOnly(q) {
		return r.base.QueryContext(ctx, q, args...)
	}
	r.record(q, args)
	return nil, migro.Newf(migro.UnsupportedFeature, "pretend cannot emulate a statement that returns rows: %s", q)
}

func (r *recorder) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return r.base.QueryRowContext(ctx, q, args...)
}

func (r *recorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.sql
	r.sql = nil
	return out
}

// pretendDB returns a DB whose writes are recorded, not executed.
func pretendDB(ctx context.Context, db *migro.DB) (*migro.DB, *recorder) {
	rec := &recorder{base: db.Gorm.Statement.ConnPool, d: db.Gorm.Dialector}
	// Passing a Context forces Session to clone the Statement; without it the Statement is shared
	// with db.Gorm and swapping its ConnPool would silently disable the real database.
	g := db.Gorm.Session(&gorm.Session{NewDB: true, Context: ctx})
	g.Statement.ConnPool = rec
	return db.WithGorm(g), rec
}
