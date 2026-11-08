package migrate

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/schema"
)

// Definition is a Lucid-style migration class: put one in each file named
// <yyyymmddhhmmss>_<snake_case_name>.go and register it from init().
//
//	func init() { migrate.Register(&CreateUsersTable{}) }
type Definition interface {
	Up(ctx context.Context, s *schema.Builder) error
	Down(ctx context.Context, s *schema.Builder) error
}

// Optional behaviors a Definition may add.
type (
	// ConnectionPinned pins a migration to a named connection (Lucid's `connection`).
	ConnectionPinned interface{ Connection() string }
	// TransactionOptOut opts a migration out of the wrapping transaction.
	TransactionOptOut interface{ DisableTransaction() bool }
)

var (
	regMu   sync.Mutex
	regList []Migration
	regErrs []error

	fileName = regexp.MustCompile(`^\d{14}_[a-z0-9_]+$`)
)

// Register records a migration whose name is taken from the calling file's name.
// Invalid or duplicate names are reported by Registered / AddRegistered, never by panicking in init().
func Register(d Definition) {
	_, file, _, _ := runtime.Caller(1)
	name := strings.TrimSuffix(filepath.Base(file), ".go")
	RegisterAs(name, d)
}

// RegisterAs registers a migration under an explicit name.
func RegisterAs(name string, d Definition) {
	regMu.Lock()
	defer regMu.Unlock()
	if !fileName.MatchString(name) {
		regErrs = append(regErrs, migro.Newf(migro.MigrationFailed,
			"migration file %q must be named <14-digit timestamp>_<snake_case>.go", name))
		return
	}
	for _, existing := range regList {
		if existing.Name == name {
			regErrs = append(regErrs, migro.Newf(migro.MigrationFailed, "duplicate migration %q", name))
			return
		}
	}
	m := Migration{Name: name, Up: Func(d.Up), Down: Func(d.Down)}
	if c, ok := d.(ConnectionPinned); ok {
		m.Connection = c.Connection()
	}
	if t, ok := d.(TransactionOptOut); ok {
		m.DisableTransaction = t.DisableTransaction()
	}
	regList = append(regList, m)
}

// Registered returns every registered migration sorted by name, or the registration errors.
func Registered() ([]Migration, error) {
	regMu.Lock()
	defer regMu.Unlock()
	if len(regErrs) > 0 {
		return nil, errors.Join(regErrs...)
	}
	out := append([]Migration(nil), regList...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ResetRegistry clears the registry (for tests).
func ResetRegistry() {
	regMu.Lock()
	defer regMu.Unlock()
	regList, regErrs = nil, nil
}

// AddRegistered adds every migration registered through Register.
func (m *Migrator) AddRegistered() error {
	ms, err := Registered()
	if err != nil {
		return err
	}
	return m.Add(ms...)
}
