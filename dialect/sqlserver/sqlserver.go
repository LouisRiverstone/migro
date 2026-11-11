// Package sqlserver registers Microsoft SQL Server.
package sqlserver

import (
	"fmt"

	"github.com/LouisRiverstone/migro/dialect"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

type msDialect struct{ dialect.Base }

func init() { dialect.Register(&msDialect{}, "mssql") }

func (d *msDialect) Name() string                        { return "sqlserver" }
func (d *msDialect) Dialector(dsn string) gorm.Dialector { return sqlserver.Open(dsn) }
func (d *msDialect) DriverName() string                  { return "sqlserver" }
func (d *msDialect) FromConn(conn gorm.ConnPool) gorm.Dialector {
	return sqlserver.New(sqlserver.Config{Conn: conn})
}
func (d *msDialect) Quote(i string) string { return dialect.QuoteWith(i, "[", "]", false) }

func (d *msDialect) ColumnType(c dialect.Column) string {
	switch c.Kind {
	case dialect.KindString, dialect.KindEnum:
		if c.Length > 4000 {
			return "nvarchar(max)"
		}
		return fmt.Sprintf("nvarchar(%d)", c.Length)
	case dialect.KindText, dialect.KindJSON:
		return "nvarchar(max)"
	case dialect.KindSmallInt:
		return "smallint"
	case dialect.KindInteger:
		return "int"
	case dialect.KindBigInt:
		return "bigint"
	case dialect.KindBoolean:
		return "bit"
	case dialect.KindFloat:
		return "real"
	case dialect.KindDouble:
		return "float"
	case dialect.KindDecimal:
		return fmt.Sprintf("decimal(%d,%d)", c.Precision, c.Scale)
	case dialect.KindDate:
		return "date"
	case dialect.KindTime:
		return "time"
	case dialect.KindDateTime:
		return "datetime2"
	case dialect.KindStamp:
		return "datetimeoffset"
	case dialect.KindUUID:
		return "uniqueidentifier"
	case dialect.KindBinary:
		return "varbinary(max)"
	}
	return ""
}

func (d *msDialect) AutoIncrement(big bool) string {
	if big {
		return "bigint identity(1,1) not null primary key"
	}
	return "int identity(1,1) not null primary key"
}

func (d *msDialect) AddColumnKeyword() string { return "ADD" }

func (d *msDialect) RenameTable(from, to string) string {
	return fmt.Sprintf("EXEC sp_rename N'%s', N'%s'", from, to)
}
func (d *msDialect) RenameColumn(t, from, to string) string {
	return fmt.Sprintf("EXEC sp_rename N'%s.%s', N'%s', N'COLUMN'", t, from, to)
}
func (d *msDialect) DropIndex(t, name string) string {
	return fmt.Sprintf("DROP INDEX %s ON %s", d.Quote(name), d.Quote(t))
}
func (d *msDialect) DropConstraint(t, name string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", d.Quote(t), d.Quote(name))
}
func (d *msDialect) DropForeign(t, name string) string { return d.DropConstraint(t, name) }

func (d *msDialect) LockSQL(dialect.LockMode) string { return "" } // needs table hints, not a clause

func (d *msDialect) JSONExtract(col string, path []string) string {
	return "JSON_VALUE(" + col + ", '" + dialect.JSONPathString(path) + "')"
}

func (d *msDialect) DatePart(col string, part dialect.DatePart) string {
	switch part {
	case dialect.PartYear:
		return "YEAR(" + col + ")"
	case dialect.PartMonth:
		return "MONTH(" + col + ")"
	case dialect.PartDay:
		return "DAY(" + col + ")"
	}
	return "CAST(" + col + " AS DATE)"
}
