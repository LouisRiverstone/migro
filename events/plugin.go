package events

import (
	"context"
	"strings"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
)

type plugin struct{ bus *Bus }

func (plugin) Name() string { return "migro:events" }

// Attach wires lifecycle events for every model operation made through db.
//
// Like GORM hooks, events do not fire for Updates with raw SQL, UpdateColumn(s),
// or statements without a model (query.Builder, Exec/Raw).
func (b *Bus) Attach(db *migro.DB) error {
	if err := db.Gorm.Use(plugin{b}); err != nil {
		return migro.Wrap(migro.InvalidConfig, err, "attach events")
	}
	return nil
}

// From returns the bus attached to db, or nil when Attach was never called.
func From(db *migro.DB) *Bus {
	if db == nil {
		return nil
	}
	p, ok := db.Gorm.Plugins[plugin{}.Name()].(plugin)
	if !ok {
		return nil
	}
	return p.bus
}

// EmitModel emits a lifecycle event for model on db's bus. It is a no-op without a bus.
func EmitModel(ctx context.Context, db *migro.DB, name, table string, model any) error {
	b := From(db)
	if b == nil {
		return nil
	}
	return b.Emit(ctx, Event{Name: name, Table: table, Model: model})
}

func (p plugin) Initialize(db *gorm.DB) error {
	emit := func(names ...string) func(*gorm.DB) {
		return func(tx *gorm.DB) {
			if tx.Error != nil || tx.Statement.Schema == nil || strings.HasPrefix(tx.Statement.Schema.Table, "migro_") {
				return
			}
			var model any
			if tx.Statement.Dest != nil {
				model = tx.Statement.Dest
			}
			for _, n := range names {
				e := Event{Name: n, Table: tx.Statement.Schema.Table, Model: model}
				if err := p.bus.Emit(tx.Statement.Context, e); err != nil {
					_ = tx.AddError(err)
					return
				}
			}
		}
	}
	reg := func(err error, e error) error {
		if err != nil {
			return err
		}
		return e
	}
	cb := db.Callback()
	var err error
	err = reg(err, cb.Create().Before("gorm:before_create").Register("migro:before_create", emit(BeforeSave, BeforeCreate)))
	err = reg(err, cb.Create().After("gorm:after_create").Register("migro:after_create", emit(AfterCreate, AfterSave)))
	err = reg(err, cb.Update().Before("gorm:before_update").Register("migro:before_update", emit(BeforeSave, BeforeUpdate)))
	err = reg(err, cb.Update().After("gorm:after_update").Register("migro:after_update", emit(AfterUpdate, AfterSave)))
	err = reg(err, cb.Delete().Before("gorm:before_delete").Register("migro:before_delete", emit(BeforeDelete)))
	err = reg(err, cb.Delete().After("gorm:after_delete").Register("migro:after_delete", emit(AfterDelete)))
	err = reg(err, cb.Query().After("gorm:after_query").Register("migro:after_find", emit(AfterFind)))
	return err
}
