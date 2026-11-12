package orm

import (
	"context"
	"reflect"

	"github.com/LouisRiverstone/migro"
)

// Connected lets a model say which Manager connection it lives on (Eloquent's $connection).
type Connected interface{ Connection() string }

// Using returns the repository for T on the connection T names, or the manager's default:
//
//	func (AuditLog) Connection() string { return "audit" }
//	logs, err := orm.Using[AuditLog](ctx, manager)
func Using[T any](ctx context.Context, m *migro.Manager) (*Model[T], error) {
	name := ""
	if c, ok := any(new(T)).(Connected); ok {
		name = c.Connection()
	}
	db, err := m.Connection(ctx, name)
	if err != nil {
		return nil, err
	}
	return New[T](db), nil
}

// UseTransaction rebinds the model to tx, so its next Save, Delete or lazy load runs inside the
// transaction (Lucid's useTransaction). Relations already loaded stay loaded.
func (b *Record) UseTransaction(tx *migro.DB) {
	if b.m.self == nil || tx == nil {
		return
	}
	b.m.db = tx
	bindRelations(tx, reflect.ValueOf(b.m.self))
}
