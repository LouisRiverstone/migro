// Package postgres registers PostgreSQL, CockroachDB and Redshift.
package postgres

import (
	"fmt"
	"strings"

	"github.com/LouisRiverstone/migro/dialect"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type variant int

const (
	pg variant = iota
	cockroach
	redshift
)

type pgDialect struct {
	dialect.Base
	name string
	v    variant
}

func init() {
	dialect.Register(&pgDialect{name: "postgres", v: pg}, "postgresql", "pg", "pgx")
	dialect.Register(&pgDialect{name: "cockroachdb", v: cockroach}, "cockroach")
	dialect.Register(&pgDialect{name: "redshift", v: redshift})
}

func (d *pgDialect) Name() string { return d.name }

func (d *pgDialect) Dialector(dsn string) gorm.Dialector {
	return postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: d.v == redshift})
}

func (d *pgDialect) DriverName() string { return "pgx" }

func (d *pgDialect) FromConn(conn gorm.ConnPool) gorm.Dialector {
	return postgres.New(postgres.Config{Conn: conn, PreferSimpleProtocol: d.v == redshift})
}

// DriverDSN makes pgx use the simple protocol on Redshift, which lacks extended-protocol support.
func (d *pgDialect) DriverDSN(dsn string) string {
	if d.v != redshift {
		return dsn
	}
	if strings.Contains(dsn, "://") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		return dsn + sep + "default_query_exec_mode=simple_protocol"
	}
	return dsn + " default_query_exec_mode=simple_protocol"
}

func (d *pgDialect) Quote(i string) string { return dialect.QuoteWith(i, `"`, `"`, false) }

func (d *pgDialect) ColumnType(c dialect.Column) string {
	switch c.Kind {
	case dialect.KindString, dialect.KindEnum:
		return fmt.Sprintf("varchar(%d)", c.Length)
	case dialect.KindText:
		if d.v == redshift {
			return "varchar(65535)"
		}
		return "text"
	case dialect.KindSmallInt:
		return "smallint"
	case dialect.KindInteger:
		return "integer"
	case dialect.KindBigInt:
		return "bigint"
	case dialect.KindBoolean:
		return "boolean"
	case dialect.KindFloat:
		return "real"
	case dialect.KindDouble:
		return "double precision"
	case dialect.KindDecimal:
		return fmt.Sprintf("numeric(%d,%d)", c.Precision, c.Scale)
	case dialect.KindDate:
		return "date"
	case dialect.KindTime:
		return "time"
	case dialect.KindDateTime:
		return "timestamp"
	case dialect.KindStamp:
		return "timestamptz"
	case dialect.KindJSON:
		if d.v == redshift {
			return "super"
		}
		return "jsonb"
	case dialect.KindUUID:
		if d.v == redshift {
			return "varchar(36)"
		}
		return "uuid"
	case dialect.KindBinary:
		if d.v == redshift {
			return "varbyte"
		}
		return "bytea"
	}
	return ""
}

func (d *pgDialect) AutoIncrement(big bool) string {
	if d.v == redshift {
		if big {
			return "bigint identity(1,1) primary key"
		}
		return "integer identity(1,1) primary key"
	}
	if big {
		return "bigserial primary key"
	}
	return "serial primary key"
}

func (d *pgDialect) BoolLiteral(b bool) string {
	return strings.ToUpper(fmt.Sprint(b))
}

func (d *pgDialect) RenameTable(from, to string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", d.Quote(from), d.Quote(to))
}
func (d *pgDialect) RenameColumn(t, from, to string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", d.Quote(t), d.Quote(from), d.Quote(to))
}
func (d *pgDialect) DropIndex(_, name string) string { return "DROP INDEX " + d.Quote(name) }
func (d *pgDialect) DropConstraint(t, name string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", d.Quote(t), d.Quote(name))
}
func (d *pgDialect) DropForeign(t, name string) string { return d.DropConstraint(t, name) }

// Redshift has no CREATE INDEX.
func (d *pgDialect) SupportsIndexes() bool { return d.v != redshift }

// CockroachDB restricts mixing schema changes in explicit transactions.
func (d *pgDialect) TransactionalDDL() bool { return d.v != cockroach }

func (d *pgDialect) JSONExtract(col string, path []string) string {
	return "(" + col + "::jsonb #>> '{" + strings.Join(path, ",") + "}')"
}

// tsvector builds the document the search runs against; the index uses the same expression.
func tsvector(cols []string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = "coalesce(" + c + "::text, '')"
	}
	return "to_tsvector('simple', " + strings.Join(parts, " || ' ' || ") + ")"
}

func (d *pgDialect) FullTextMatch(cols []string, boolean bool) string {
	if d.v == redshift {
		return ""
	}
	query := "plainto_tsquery"
	if boolean {
		query = "websearch_to_tsquery"
	}
	return tsvector(cols) + " @@ " + query + "('simple', ?)"
}

func (d *pgDialect) FullTextIndex(table, name string, cols []string) string {
	if d.v == redshift {
		return ""
	}
	return "CREATE INDEX " + name + " ON " + table + " USING GIN (" + tsvector(cols) + ")"
}

func (d *pgDialect) LockSQL(mode dialect.LockMode) string {
	if d.v == redshift {
		return ""
	}
	return d.Base.LockSQL(mode)
}
