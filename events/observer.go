package events

import (
	"context"

	"github.com/LouisRiverstone/migro"
)

// Observer hooks. A type implements any of them for the model T it observes and registers once with
// Observe; the compiler checks every method signature against T (Eloquent's observers, type safe).
//
//	type UserObserver struct{ mailer Mailer }
//	func (o *UserObserver) Creating(ctx context.Context, u *User) error { ... }
//	func (o *UserObserver) Created(ctx context.Context, u *User) error  { return o.mailer.Welcome(u) }
//
//	events.Observe[User](bus, &UserObserver{mailer})
//
// Hooks named Creating, Updating, Saving, Deleting, Restoring, ForceDeleting and Replicating run
// before the operation and abort it by returning an error; the others run after it.
type (
	Creating[T any] interface {
		Creating(context.Context, *T) error
	}
	Created[T any] interface {
		Created(context.Context, *T) error
	}
	Updating[T any] interface {
		Updating(context.Context, *T) error
	}
	Updated[T any] interface {
		Updated(context.Context, *T) error
	}
	Saving[T any] interface {
		Saving(context.Context, *T) error
	}
	Saved[T any] interface {
		Saved(context.Context, *T) error
	}
	Deleting[T any] interface {
		Deleting(context.Context, *T) error
	}
	Deleted[T any] interface {
		Deleted(context.Context, *T) error
	}
	Retrieved[T any] interface {
		Retrieved(context.Context, *T) error
	}
	Restoring[T any] interface {
		Restoring(context.Context, *T) error
	}
	Restored[T any] interface {
		Restored(context.Context, *T) error
	}
	ForceDeleting[T any] interface {
		ForceDeleting(context.Context, *T) error
	}
	ForceDeleted[T any] interface {
		ForceDeleted(context.Context, *T) error
	}
	Replicating[T any] interface {
		Replicating(context.Context, *T) error
	}
)

// Observe subscribes every hook obs implements to events of model T. It returns a function that
// removes them all. An observer that implements no hook is an error (usually a misspelled method name).
func Observe[T any](b *Bus, obs any) (off func(), err error) {
	var offs []func()
	add := func(event string, fn func(context.Context, *T) error) {
		offs = append(offs, OnModel[T](b, event, fn))
	}
	if h, ok := obs.(Creating[T]); ok {
		add(BeforeCreate, h.Creating)
	}
	if h, ok := obs.(Created[T]); ok {
		add(AfterCreate, h.Created)
	}
	if h, ok := obs.(Updating[T]); ok {
		add(BeforeUpdate, h.Updating)
	}
	if h, ok := obs.(Updated[T]); ok {
		add(AfterUpdate, h.Updated)
	}
	if h, ok := obs.(Saving[T]); ok {
		add(BeforeSave, h.Saving)
	}
	if h, ok := obs.(Saved[T]); ok {
		add(AfterSave, h.Saved)
	}
	if h, ok := obs.(Deleting[T]); ok {
		add(BeforeDelete, h.Deleting)
	}
	if h, ok := obs.(Deleted[T]); ok {
		add(AfterDelete, h.Deleted)
	}
	if h, ok := obs.(Retrieved[T]); ok {
		add(AfterFind, h.Retrieved)
	}
	if h, ok := obs.(Restoring[T]); ok {
		add(BeforeRestore, h.Restoring)
	}
	if h, ok := obs.(Restored[T]); ok {
		add(AfterRestore, h.Restored)
	}
	if h, ok := obs.(ForceDeleting[T]); ok {
		add(BeforeForceDelete, h.ForceDeleting)
	}
	if h, ok := obs.(ForceDeleted[T]); ok {
		add(AfterForceDelete, h.ForceDeleted)
	}
	if h, ok := obs.(Replicating[T]); ok {
		add(BeforeReplicate, h.Replicating)
	}
	if len(offs) == 0 {
		return nil, migro.Newf(migro.InvalidConfig, "observer %T implements no hook for %T (Creating, Created, Updating, ...): check the method names and signatures", obs, *new(T))
	}
	return func() {
		for _, off := range offs {
			off()
		}
	}, nil
}
