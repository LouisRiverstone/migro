package migro

import (
	"context"
	"strings"
	"sync/atomic"

	"gorm.io/gorm"
	"gorm.io/plugin/dbresolver"
)

type stickyKey struct{}

type stickyFlag struct{ wrote atomic.Bool }

// Sticky returns a context that sends reads to the primary once a write has run with it (Laravel's
// "sticky" connection option). Use one per request or job: reads that come before the first write still
// go to a replica, reads after it see the data just written.
//
// It only matters with Config.Replicas. Pass the returned context to every call of the unit of work.
func Sticky(ctx context.Context) context.Context {
	if ctx.Value(stickyKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, stickyKey{}, &stickyFlag{})
}

func stickyOf(db *gorm.DB) *stickyFlag {
	if db.Statement == nil || db.Statement.Context == nil {
		return nil
	}
	f, _ := db.Statement.Context.Value(stickyKey{}).(*stickyFlag)
	return f
}

func isSelect(sql string) bool {
	return len(sql) >= 6 && strings.EqualFold(strings.TrimSpace(sql)[:6], "select")
}

func markWrite(db *gorm.DB) {
	if f := stickyOf(db); f != nil {
		f.wrote.Store(true)
	}
}

func markRawWrite(db *gorm.DB) {
	if !isSelect(db.Statement.SQL.String()) {
		markWrite(db)
	}
}

func readFromPrimary(db *gorm.DB) {
	if f := stickyOf(db); f != nil && f.wrote.Load() {
		dbresolver.Write.ModifyStatement(db.Statement)
	}
}

type registrar interface {
	Register(string, func(*gorm.DB)) error
}

// registerSticky hooks the write marker and the read redirect. It must run before dbresolver is
// registered: both want to run first, and the earlier registration wins.
func registerSticky(g *gorm.DB) error {
	cb := g.Callback()
	for _, r := range []registrar{cb.Query().Before("*"), cb.Row().Before("*"), cb.Raw().Before("*")} {
		if err := r.Register("migro:sticky_read", readFromPrimary); err != nil {
			return err
		}
	}
	for _, r := range []registrar{cb.Create().After("*"), cb.Update().After("*"), cb.Delete().After("*")} {
		if err := r.Register("migro:sticky_write", markWrite); err != nil {
			return err
		}
	}
	return cb.Raw().After("*").Register("migro:sticky_raw", markRawWrite)
}
