// Package dialect defines the per-database adapter contract and a registry.
package dialect

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"gorm.io/gorm"
)

// Kind is a database-agnostic column type.
type Kind string

const (
	KindString   Kind = "string"
	KindText     Kind = "text"
	KindSmallInt Kind = "smallint"
	KindInteger  Kind = "integer"
	KindBigInt   Kind = "bigint"
	KindBoolean  Kind = "boolean"
	KindFloat    Kind = "float"
	KindDouble   Kind = "double"
	KindDecimal  Kind = "decimal"
	KindDate     Kind = "date"
	KindTime     Kind = "time"
	KindDateTime Kind = "datetime"
	KindStamp    Kind = "timestamp"
	KindJSON     Kind = "json"
	KindUUID     Kind = "uuid"
	KindBinary   Kind = "binary"
	KindEnum     Kind = "enum"
)

// Column carries what a dialect needs to render a type.
type Column struct {
	Kind      Kind
	Length    int
	Precision int
	Scale     int
	Unsigned  bool
	Enum      []string
}

// Dialect is implemented once per supported database family.
// Methods returning SQL return "" when the operation is unsupported.
type Dialect interface {
	Name() string
	Dialector(dsn string) gorm.Dialector
	// DriverName is the database/sql driver name; DriverDSN adapts the DSN for it.
	// FromConn builds a Dialector on top of an existing pool (used for AfterCreate hooks,
	// acquire timeouts and replicas).
	DriverName() string
	DriverDSN(dsn string) string
	FromConn(conn gorm.ConnPool) gorm.Dialector
	Quote(ident string) string
	ColumnType(c Column) string
	// AutoIncrement returns the full column definition (after the column name) for an auto-increment primary key.
	AutoIncrement(big bool) string
	BoolLiteral(b bool) string
	AddColumnKeyword() string
	RenameTable(from, to string) string
	RenameColumn(table, from, to string) string
	DropIndex(table, name string) string
	DropConstraint(table, name string) string
	DropForeign(table, name string) string
	SupportsAddConstraint() bool
	SupportsIndexes() bool
	NativeEnum() bool
	TransactionalDDL() bool
}

var (
	mu       sync.RWMutex
	registry = map[string]Dialect{}
)

// Register makes a dialect available under its name and any aliases.
func Register(d Dialect, aliases ...string) {
	mu.Lock()
	defer mu.Unlock()
	registry[strings.ToLower(d.Name())] = d
	for _, a := range aliases {
		registry[strings.ToLower(a)] = d
	}
}

// Get resolves a registered dialect. The error tells the caller which package to import.
func Get(name string) (Dialect, error) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := registry[strings.ToLower(name)]
	if ok {
		return d, nil
	}
	return nil, fmt.Errorf("dialect %q not registered (import its package under migro/dialect/, registered: %s)", name, namesLocked())
}

func namesLocked() string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// QuoteWith quotes each dot-separated part using open/close delimiters, escaping the close delimiter.
func QuoteWith(ident, open, closing string, upper bool) string {
	parts := strings.Split(ident, ".")
	for i, p := range parts {
		if upper {
			p = strings.ToUpper(p)
		}
		parts[i] = open + strings.ReplaceAll(p, closing, closing+closing) + closing
	}
	return strings.Join(parts, ".")
}

// Base provides ANSI defaults; dialects embed it and override what differs.
type Base struct{}

func (Base) DriverDSN(dsn string) string { return dsn }
func (Base) AddColumnKeyword() string    { return "ADD COLUMN" }
func (Base) BoolLiteral(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
func (Base) SupportsAddConstraint() bool { return true }
func (Base) SupportsIndexes() bool       { return true }
func (Base) NativeEnum() bool            { return false }
func (Base) TransactionalDDL() bool      { return true }
