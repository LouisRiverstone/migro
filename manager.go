package migro

import (
	"context"
	"errors"
	"slices"
	"sort"
	"sync"
)

// Manager holds named database connections and opens each one the first time it is asked for
// (Laravel's DB::connection). Use cluster.Cluster instead to route by key across shards.
type Manager struct {
	mu    sync.Mutex
	def   string
	cfgs  map[string]Config
	conns map[string]*DB
}

// NewManager registers connections by name; def is the one Default and Connection("") return.
// Nothing connects until a connection is first used.
func NewManager(def string, cfgs map[string]Config) (*Manager, error) {
	if _, ok := cfgs[def]; !ok {
		return nil, Newf(ConnectionNotFound, "default connection %q is not configured", def)
	}
	m := &Manager{def: def, cfgs: make(map[string]Config, len(cfgs)), conns: map[string]*DB{}}
	for n, c := range cfgs {
		m.cfgs[n] = c
	}
	return m, nil
}

// Connection returns the named database, opening it on first use. An empty name means the default.
func (m *Manager) Connection(ctx context.Context, name string) (*DB, error) {
	if name == "" {
		name = m.def
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if db, ok := m.conns[name]; ok {
		return db, nil
	}
	cfg, ok := m.cfgs[name]
	if !ok {
		return nil, Newf(ConnectionNotFound, "connection %q is not configured (have: %v)", name, m.namesLocked())
	}
	db, err := OpenContext(ctx, cfg)
	if err != nil {
		return nil, err
	}
	db.name = name
	m.conns[name] = db
	return db, nil
}

// Default returns the default connection.
func (m *Manager) Default(ctx context.Context) (*DB, error) { return m.Connection(ctx, "") }

// Names lists the configured connections, opened or not.
func (m *Manager) Names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.namesLocked()
}

func (m *Manager) namesLocked() []string {
	out := make([]string, 0, len(m.cfgs))
	for n := range m.cfgs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Purge closes a connection and forgets it; the next Connection call reopens it (Laravel's DB::purge).
func (m *Manager) Purge(name string) error {
	m.mu.Lock()
	db, ok := m.conns[name]
	delete(m.conns, name)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return db.Close()
}

// Close closes every opened connection.
func (m *Manager) Close() error {
	m.mu.Lock()
	open := make([]*DB, 0, len(m.conns))
	for _, db := range m.conns {
		open = append(open, db)
	}
	m.conns = map[string]*DB{}
	m.mu.Unlock()
	var errs []error
	for _, db := range slices.Clone(open) {
		if err := db.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Name is the name the database was registered under in a Manager, or "" for a standalone one.
func (d *DB) Name() string { return d.name }
