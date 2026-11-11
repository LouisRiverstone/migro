package migro

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Strict turns silent mistakes into errors. It is off by default, so enabling it is opt-in per
// database (Eloquent's Model::shouldBeStrict()).
//
// Mass assignment and unknown columns are always strict in migro: Fill fails with MASS_ASSIGNMENT and
// GetAttribute fails with INVALID_SCHEMA, so the only switch that remains is lazy loading.
type Strict struct {
	// LazyLoading makes a relation's Get fail with LAZY_LOADING_VIOLATION when the relation was not
	// eager loaded (Eloquent's preventLazyLoading). Models that are not persisted yet are exempt.
	LazyLoading bool
	// OnLazyLoad, when set, is called instead of failing. Use it to log the violation in production
	// while still failing in development (Eloquent's handleLazyLoadingViolationUsing).
	OnLazyLoad func(model, relation string)
}

// QueryEvent describes one executed statement.
type QueryEvent struct {
	SQL      string
	Rows     int64
	Duration time.Duration
	Err      error
}

type dbState struct {
	mu        sync.RWMutex
	strict    Strict
	next      uint64
	listeners map[uint64]func(QueryEvent)
	logging   bool
	log       []QueryEvent
}

func newState() *dbState { return &dbState{listeners: map[uint64]func(QueryEvent){}} }

func (s *dbState) active() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.logging || len(s.listeners) > 0
}

func (s *dbState) emit(ev QueryEvent) {
	s.mu.Lock()
	if s.logging {
		s.log = append(s.log, ev)
	}
	fns := make([]func(QueryEvent), 0, len(s.listeners))
	for _, fn := range s.listeners {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	for _, fn := range fns { // outside the lock: a listener may call back into the DB
		fn(ev)
	}
}

// SetStrict replaces the strictness settings. Transactions and WriteDB handles share them.
func (d *DB) SetStrict(s Strict) {
	if d.st == nil {
		return
	}
	d.st.mu.Lock()
	d.st.strict = s
	d.st.mu.Unlock()
}

// ShouldBeStrict switches every strictness check on or off.
func (d *DB) ShouldBeStrict(on bool) { d.SetStrict(Strict{LazyLoading: on}) }

// Strictness returns the current settings.
func (d *DB) Strictness() Strict {
	if d.st == nil {
		return Strict{}
	}
	d.st.mu.RLock()
	defer d.st.mu.RUnlock()
	return d.st.strict
}

// Listen calls fn after every statement the database runs (Laravel's DB::listen). The returned
// function unsubscribes. fn runs on the goroutine that ran the statement and must not block.
func (d *DB) Listen(fn func(QueryEvent)) (off func()) {
	if d.st == nil || fn == nil {
		return func() {}
	}
	d.st.mu.Lock()
	d.st.next++
	id := d.st.next
	d.st.listeners[id] = fn
	d.st.mu.Unlock()
	return func() {
		d.st.mu.Lock()
		delete(d.st.listeners, id)
		d.st.mu.Unlock()
	}
}

// EnableQueryLog starts recording statements (Laravel's DB::enableQueryLog).
func (d *DB) EnableQueryLog() {
	if d.st != nil {
		d.st.mu.Lock()
		d.st.logging = true
		d.st.mu.Unlock()
	}
}

// DisableQueryLog stops recording; the recorded statements are kept until FlushQueryLog.
func (d *DB) DisableQueryLog() {
	if d.st != nil {
		d.st.mu.Lock()
		d.st.logging = false
		d.st.mu.Unlock()
	}
}

// QueryLog returns a copy of the recorded statements.
func (d *DB) QueryLog() []QueryEvent {
	if d.st == nil {
		return nil
	}
	d.st.mu.RLock()
	defer d.st.mu.RUnlock()
	return append([]QueryEvent(nil), d.st.log...)
}

// FlushQueryLog discards the recorded statements.
func (d *DB) FlushQueryLog() {
	if d.st != nil {
		d.st.mu.Lock()
		d.st.log = nil
		d.st.mu.Unlock()
	}
}

// WithGorm returns a handle on g that shares this database's dialect, strictness and listeners.
func (d *DB) WithGorm(g *gorm.DB) *DB {
	return &DB{Gorm: g, Dialect: d.Dialect, st: d.st, name: d.name}
}

// tracer reports every statement to the state while delegating to the user's logger.
type tracer struct {
	logger.Interface
	st *dbState
}

func (t tracer) LogMode(l logger.LogLevel) logger.Interface {
	return tracer{Interface: t.Interface.LogMode(l), st: t.st}
}

func (t tracer) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	t.Interface.Trace(ctx, begin, fc, err)
	if !t.st.active() {
		return
	}
	sql, rows := fc()
	t.st.emit(QueryEvent{SQL: sql, Rows: rows, Duration: time.Since(begin), Err: err})
}

func traced(l logger.Interface, st *dbState) logger.Interface {
	if l == nil {
		l = logger.Default
	}
	return tracer{Interface: l, st: st}
}
