// Package events is a Lucid-style in-process event emitter plus model lifecycle events.
//
// Event names follow Lucid's hook points: before:create, after:create, before:update,
// after:update, before:save, after:save, before:delete, after:delete, after:find.
// Handlers registered for a "before:*" event may abort the operation by returning an error.
package events

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"

	"github.com/LouisRiverstone/migro"
)

// Lifecycle event names emitted by Attach.
const (
	BeforeCreate = "before:create"
	AfterCreate  = "after:create"
	BeforeUpdate = "before:update"
	AfterUpdate  = "after:update"
	BeforeSave   = "before:save"
	AfterSave    = "after:save"
	BeforeDelete = "before:delete"
	AfterDelete  = "after:delete"
	AfterFind    = "after:find"
)

// Event is what handlers receive.
type Event struct {
	Name string
	// Table is the model's table for lifecycle events, empty for custom events.
	Table string
	// Model is the model pointer (or slice pointer for bulk operations); Payload is free-form.
	Model   any
	Payload any
}

type Handler func(ctx context.Context, e Event) error

type subscription struct {
	id      uint64
	pattern string // "" = any
	once    bool
	fn      Handler
}

// Bus dispatches events to handlers in registration order. Safe for concurrent use.
type Bus struct {
	mu   sync.RWMutex
	next uint64
	subs []subscription
	wg   sync.WaitGroup
}

func New() *Bus { return &Bus{} }

// On subscribes to events whose name matches pattern: an exact name, or a glob such as
// "before:*" or "*:create". It returns a function that unsubscribes.
func (b *Bus) On(pattern string, h Handler) (off func()) { return b.add(pattern, false, h) }

// Once is like On but unsubscribes after the first delivery.
func (b *Bus) Once(pattern string, h Handler) (off func()) { return b.add(pattern, true, h) }

// OnAny subscribes to every event.
func (b *Bus) OnAny(h Handler) (off func()) { return b.add("", false, h) }

func (b *Bus) add(pattern string, once bool, h Handler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	id := b.next
	b.subs = append(b.subs, subscription{id: id, pattern: pattern, once: once, fn: h})
	return func() { b.remove(id) }
}

func (b *Bus) remove(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.subs {
		if s.id == id {
			b.subs = append(b.subs[:i:i], b.subs[i+1:]...)
			return
		}
	}
}

func matches(pattern, name string) bool {
	if pattern == "" || pattern == name {
		return true
	}
	ok, err := path.Match(pattern, name)
	return err == nil && ok
}

func (b *Bus) snapshot(name string) []subscription {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []subscription
	for _, s := range b.subs {
		if matches(s.pattern, name) {
			out = append(out, s)
		}
	}
	return out
}

func call(ctx context.Context, h Handler, e Event) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return h(ctx, e)
}

// Emit runs matching handlers sequentially.
//   - For "before:*" events the first handler error stops dispatch and is returned as EVENT_ABORTED.
//   - For other events every handler runs and failures are joined into EVENT_HANDLER_FAILED.
func (b *Bus) Emit(ctx context.Context, e Event) error {
	var errs []error
	for _, s := range b.snapshot(e.Name) {
		if s.once {
			b.remove(s.id)
		}
		if err := call(ctx, s.fn, e); err != nil {
			if strings.HasPrefix(e.Name, "before:") {
				return migro.Wrap(migro.EventAborted, err, "%s aborted", e.Name)
			}
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return migro.Wrap(migro.EventHandlerFailed, errors.Join(errs...), "%s", e.Name)
	}
	return nil
}

// EmitAsync dispatches in a goroutine and ignores handler errors (they are returned to onErr when set).
// Call Wait during shutdown to drain in-flight events.
func (b *Bus) EmitAsync(ctx context.Context, e Event, onErr func(error)) {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		if err := b.Emit(ctx, e); err != nil && onErr != nil {
			onErr(err)
		}
	}()
}

// Wait blocks until all EmitAsync work has finished.
func (b *Bus) Wait() { b.wg.Wait() }

// OnModel subscribes to a lifecycle event for model type T only. Bulk operations
// (slices of T) invoke fn once per element.
func OnModel[T any](b *Bus, pattern string, fn func(ctx context.Context, m *T) error) (off func()) {
	return b.On(pattern, func(ctx context.Context, e Event) error {
		switch m := e.Model.(type) {
		case *T:
			return fn(ctx, m)
		case *[]T:
			for i := range *m {
				if err := fn(ctx, &(*m)[i]); err != nil {
					return err
				}
			}
		case []T:
			for i := range m {
				if err := fn(ctx, &m[i]); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
