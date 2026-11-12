// Package sqlite registers SQLite using the pure-Go (no cgo) glebarez driver.
package sqlite

import (
	"fmt"

	"github.com/LouisRiverstone/migro/dialect"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type liteDialect struct{ dialect.Base }

func init() { dialect.Register(&liteDialect{}, "sqlite3") }

func (d *liteDialect) Name() string                        { return "sqlite" }
func (d *liteDialect) Dialector(dsn string) gorm.Dialector { return sqlite.Open(dsn) }
func (d *liteDialect) DriverName() string                  { return "sqlite" }
func (d *liteDialect) FromConn(conn gorm.ConnPool) gorm.Dialector {
	return &sqlite.Dialector{Conn: conn}
}
func (d *liteDialect) Quote(i string) string { return dialect.QuoteWith(i, `"`, `"`, false) }

func (d *liteDialect) ColumnType(c dialect.Column) string {
	switch c.Kind {
	case dialect.KindString, dialect.KindEnum:
		return fmt.Sprintf("varchar(%d)", c.Length)
	case dialect.KindText, dialect.KindJSON:
		return "text"
	case dialect.KindSmallInt, dialect.KindInteger, dialect.KindBigInt:
		return "integer"
	case dialect.KindBoolean:
		return "boolean"
	case dialect.KindFloat, dialect.KindDouble:
		return "real"
	case dialect.KindDecimal:
		return fmt.Sprintf("decimal(%d,%d)", c.Precision, c.Scale)
	case dialect.KindDate:
		return "date"
	case dialect.KindTime:
		return "time"
	case dialect.KindDateTime, dialect.KindStamp:
		return "datetime"
	case dialect.KindUUID:
		return "varchar(36)"
	case dialect.KindBinary:
		return "blob"
	}
	return ""
}

func (d *liteDialect) AutoIncrement(bool) string { return "integer primary key autoincrement" }

func (d *liteDialect) RenameTable(from, to string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", d.Quote(from), d.Quote(to))
}
func (d *liteDialect) RenameColumn(t, from, to string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", d.Quote(t), d.Quote(from), d.Quote(to))
}
func (d *liteDialect) DropIndex(_, name string) string      { return "DROP INDEX " + d.Quote(name) }
func (d *liteDialect) DropConstraint(_, name string) string { return "DROP INDEX " + d.Quote(name) }
func (d *liteDialect) DropForeign(_, _ string) string       { return "" }

// SQLite cannot ALTER TABLE ADD CONSTRAINT.
func (d *liteDialect) SupportsAddConstraint() bool { return false }

func (d *liteDialect) LockSQL(dialect.LockMode) string { return "" } // SQLite locks the whole database

func (d *liteDialect) JSONExtract(col string, path []string) string {
	return "json_extract(" + col + ", '" + dialect.JSONPathString(path) + "')"
}

func (d *liteDialect) DatePart(col string, part dialect.DatePart) string {
	switch part {
	case dialect.PartYear:
		return "CAST(strftime('%Y', " + col + ") AS INTEGER)"
	case dialect.PartMonth:
		return "CAST(strftime('%m', " + col + ") AS INTEGER)"
	case dialect.PartDay:
		return "CAST(strftime('%d', " + col + ") AS INTEGER)"
	}
	return "date(" + col + ")"
}
