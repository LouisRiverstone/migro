// Package mysql registers MySQL and MariaDB.
package mysql

import (
	"fmt"
	"strings"

	"github.com/LouisRiverstone/migro/dialect"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type myDialect struct {
	dialect.Base
	name string
}

func init() {
	dialect.Register(&myDialect{name: "mysql"})
	dialect.Register(&myDialect{name: "mariadb"})
}

func (d *myDialect) Name() string { return d.name }
func (d *myDialect) Dialector(dsn string) gorm.Dialector {
	return mysql.Open(dsn)
}
func (d *myDialect) DriverName() string { return "mysql" }
func (d *myDialect) FromConn(conn gorm.ConnPool) gorm.Dialector {
	return mysql.New(mysql.Config{Conn: conn})
}
func (d *myDialect) Quote(i string) string { return dialect.QuoteWith(i, "`", "`", false) }

func (d *myDialect) ColumnType(c dialect.Column) string {
	u := ""
	if c.Unsigned {
		u = " unsigned"
	}
	switch c.Kind {
	case dialect.KindString:
		return fmt.Sprintf("varchar(%d)", c.Length)
	case dialect.KindText:
		return "text"
	case dialect.KindSmallInt:
		return "smallint" + u
	case dialect.KindInteger:
		return "int" + u
	case dialect.KindBigInt:
		return "bigint" + u
	case dialect.KindBoolean:
		return "tinyint(1)"
	case dialect.KindFloat:
		return "float"
	case dialect.KindDouble:
		return "double"
	case dialect.KindDecimal:
		return fmt.Sprintf("decimal(%d,%d)", c.Precision, c.Scale)
	case dialect.KindDate:
		return "date"
	case dialect.KindTime:
		return "time"
	case dialect.KindDateTime, dialect.KindStamp:
		return "datetime(3)"
	case dialect.KindJSON:
		return "json"
	case dialect.KindUUID:
		return "char(36)"
	case dialect.KindBinary:
		return "longblob"
	case dialect.KindEnum:
		vals := make([]string, len(c.Enum))
		for i, v := range c.Enum {
			vals[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
		}
		return "enum(" + strings.Join(vals, ",") + ")"
	}
	return ""
}

func (d *myDialect) AutoIncrement(big bool) string {
	if big {
		return "bigint unsigned not null auto_increment primary key"
	}
	return "int unsigned not null auto_increment primary key"
}

func (d *myDialect) RenameTable(from, to string) string {
	return fmt.Sprintf("RENAME TABLE %s TO %s", d.Quote(from), d.Quote(to))
}
func (d *myDialect) RenameColumn(t, from, to string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", d.Quote(t), d.Quote(from), d.Quote(to))
}
func (d *myDialect) DropIndex(t, name string) string {
	return fmt.Sprintf("DROP INDEX %s ON %s", d.Quote(name), d.Quote(t))
}
func (d *myDialect) DropConstraint(t, name string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP INDEX %s", d.Quote(t), d.Quote(name))
}
func (d *myDialect) DropForeign(t, name string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP FOREIGN KEY %s", d.Quote(t), d.Quote(name))
}
func (d *myDialect) BoolLiteral(b bool) string { return d.Base.BoolLiteral(b) }
func (d *myDialect) NativeEnum() bool          { return true }

// MySQL implicitly commits DDL.
func (d *myDialect) TransactionalDDL() bool { return false }

func (d *myDialect) LockSQL(mode dialect.LockMode) string {
	if mode == dialect.LockShare {
		return "LOCK IN SHARE MODE" // MariaDB has no FOR SHARE
	}
	return "FOR UPDATE"
}

func (d *myDialect) JSONExtract(col string, path []string) string {
	return "JSON_UNQUOTE(JSON_EXTRACT(" + col + ", '" + dialect.JSONPathString(path) + "'))"
}

func (d *myDialect) FullTextMatch(cols []string, boolean bool) string {
	mode := "NATURAL LANGUAGE"
	if boolean {
		mode = "BOOLEAN"
	}
	return "MATCH (" + strings.Join(cols, ", ") + ") AGAINST (? IN " + mode + " MODE)"
}

func (d *myDialect) FullTextIndex(table, name string, cols []string) string {
	return "CREATE FULLTEXT INDEX " + name + " ON " + table + " (" + strings.Join(cols, ", ") + ")"
}
