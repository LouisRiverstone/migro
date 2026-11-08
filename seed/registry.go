package seed

import (
	"context"
	"reflect"
	"sort"
	"sync"

	"github.com/LouisRiverstone/migro"
)

// Class is a Lucid-style seeder: a type with a Run method, registered from init():
//
//	func init() { seed.Register(&UserSeeder{}) }
//
// Add `func (UserSeeder) Environments() []string` to limit where it runs.
type Class interface {
	Run(ctx context.Context, db *migro.DB) error
}

type environmental interface{ Environments() []string }

var (
	regMu sync.Mutex
	reg   []Seeder
)

// Register records a seeder named after its type (so db:seed --class=UserSeeder finds it).
func Register(c Class) {
	t := reflect.TypeOf(c)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	s := Seeder{Name: t.Name(), Run: c.Run}
	if e, ok := c.(environmental); ok {
		s.Environments = e.Environments()
	}
	regMu.Lock()
	defer regMu.Unlock()
	reg = append(reg, s)
}

// Registered returns the registered seeders sorted by name.
func Registered() []Seeder {
	regMu.Lock()
	defer regMu.Unlock()
	out := append([]Seeder(nil), reg...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ResetRegistry clears the registry (for tests).
func ResetRegistry() {
	regMu.Lock()
	defer regMu.Unlock()
	reg = nil
}

// AddRegistered adds every seeder registered through Register.
func (r *Runner) AddRegistered() error { return r.Add(Registered()...) }
