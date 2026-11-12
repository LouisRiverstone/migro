// Package schema is a Knex-style DDL builder compiled per dialect.
package schema

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/dialect"
)

const maxIdentLen = 63

// Table collects column and constraint definitions for CreateTable / AlterTable.
type Table struct {
	name  string
	alter bool

	columns  []*Column
	indexes  []*Index
	uniques  []*Index
	fulltext []*Index
	foreigns []*Foreign
	primary  []string

	dropColumns  []string
	renames      [][2]string
	dropIndexes  []string
	dropUniques  []string
	dropForeigns []string
	err          error
}

func (t *Table) fail(format string, args ...any) {
	if t.err == nil {
		t.err = migro.Newf(migro.InvalidSchema, format, args...)
	}
}

// Index is a (possibly unique) index definition.
type Index struct {
	cols []string
	name string
}

// Named overrides the generated index name.
func (i *Index) Named(name string) *Index { i.name = name; return i }

// Foreign is a foreign key definition.
type Foreign struct {
	cols     []string
	refTable string
	refCols  []string
	onDelete string
	onUpdate string
	name     string
}

// References sets the referenced table and columns.
func (f *Foreign) References(table string, cols ...string) *Foreign {
	f.refTable, f.refCols = table, cols
	return f
}

// OnDelete sets CASCADE, SET NULL, RESTRICT, NO ACTION or SET DEFAULT.
func (f *Foreign) OnDelete(action string) *Foreign { f.onDelete = action; return f }

// OnUpdate sets CASCADE, SET NULL, RESTRICT, NO ACTION or SET DEFAULT.
func (f *Foreign) OnUpdate(action string) *Foreign { f.onUpdate = action; return f }

// Named overrides the generated constraint name.
func (f *Foreign) Named(name string) *Foreign { f.name = name; return f }

// Column is a single column definition. Chain modifiers on it.
type Column struct {
	t          *Table
	name       string
	spec       dialect.Column
	auto, big  bool
	notNull    bool
	primary    bool
	hasDefault bool
	def        any
	defRaw     string
}

func (c *Column) NotNull() *Column  { c.notNull = true; return c }
func (c *Column) Nullable() *Column { c.notNull = false; return c }
func (c *Column) Unsigned() *Column { c.spec.Unsigned = true; return c }

// Default sets a literal default (string, ints, floats, bool, nil).
func (c *Column) Default(v any) *Column { c.hasDefault, c.def = true, v; return c }

// DefaultRaw sets a raw SQL default expression. The caller must trust it.
func (c *Column) DefaultRaw(expr string) *Column { c.defRaw = expr; return c }

// DefaultNow defaults to CURRENT_TIMESTAMP.
func (c *Column) DefaultNow() *Column { return c.DefaultRaw("CURRENT_TIMESTAMP") }

// Primary marks this single column as the primary key.
func (c *Column) Primary() *Column { c.primary = true; return c }

// Unique adds a unique constraint on this column.
func (c *Column) Unique() *Column { c.t.Unique(c.name); return c }

// Index adds an index on this column.
func (c *Column) Index() *Column { c.t.Index(c.name); return c }

// References adds a foreign key from this column to table(column) and returns it for
// chaining: t.Integer("user_id").Unsigned().References("users", "id").OnDelete("CASCADE").
func (c *Column) References(table, column string) *Foreign {
	return c.t.Foreign(c.name).References(table, column)
}

// Constrained adds a foreign key to table(id). With no argument the table is inferred from the
// column name (user_id -> users), like Laravel. It returns the Foreign for .OnDelete()/.OnUpdate().
func (c *Column) Constrained(table ...string) *Foreign {
	target := inferTable(c.name)
	if len(table) > 0 {
		target = table[0]
	}
	return c.References(target, "id")
}

func inferTable(column string) string {
	base := strings.TrimSuffix(column, "_id")
	if strings.HasSuffix(base, "y") && !strings.HasSuffix(base, "ey") {
		return base[:len(base)-1] + "ies"
	}
	if strings.HasSuffix(base, "s") {
		return base + "es"
	}
	return base + "s"
}

func (t *Table) add(name string, spec dialect.Column) *Column {
	c := &Column{t: t, name: name, spec: spec}
	t.columns = append(t.columns, c)
	return c
}

func (t *Table) Increments(name string) *Column {
	c := t.add(name, dialect.Column{Kind: dialect.KindInteger})
	c.auto = true
	return c
}

func (t *Table) BigIncrements(name string) *Column {
	c := t.add(name, dialect.Column{Kind: dialect.KindBigInt})
	c.auto, c.big = true, true
	return c
}

// String is a varchar; length defaults to 255.
func (t *Table) String(name string, length ...int) *Column {
	n := 255
	if len(length) > 0 {
		n = length[0]
	}
	return t.add(name, dialect.Column{Kind: dialect.KindString, Length: n})
}

func (t *Table) Text(name string) *Column { return t.add(name, dialect.Column{Kind: dialect.KindText}) }
func (t *Table) SmallInteger(name string) *Column {
	return t.add(name, dialect.Column{Kind: dialect.KindSmallInt})
}
func (t *Table) Integer(name string) *Column {
	return t.add(name, dialect.Column{Kind: dialect.KindInteger})
}
func (t *Table) BigInteger(name string) *Column {
	return t.add(name, dialect.Column{Kind: dialect.KindBigInt})
}
func (t *Table) Boolean(name string) *Column {
	return t.add(name, dialect.Column{Kind: dialect.KindBoolean})
}
func (t *Table) Float(name string) *Column {
	return t.add(name, dialect.Column{Kind: dialect.KindFloat})
}
func (t *Table) Double(name string) *Column {
	return t.add(name, dialect.Column{Kind: dialect.KindDouble})
}

// Decimal defaults to (8,2) when precision is 0.
func (t *Table) Decimal(name string, precision, scale int) *Column {
	if precision == 0 {
		precision, scale = 8, 2
	}
	return t.add(name, dialect.Column{Kind: dialect.KindDecimal, Precision: precision, Scale: scale})
}

func (t *Table) Date(name string) *Column { return t.add(name, dialect.Column{Kind: dialect.KindDate}) }
func (t *Table) Time(name string) *Column { return t.add(name, dialect.Column{Kind: dialect.KindTime}) }
func (t *Table) DateTime(name string) *Column {
	return t.add(name, dialect.Column{Kind: dialect.KindDateTime})
}
func (t *Table) Timestamp(name string) *Column {
	return t.add(name, dialect.Column{Kind: dialect.KindStamp})
}
func (t *Table) JSON(name string) *Column { return t.add(name, dialect.Column{Kind: dialect.KindJSON}) }
func (t *Table) UUID(name string) *Column { return t.add(name, dialect.Column{Kind: dialect.KindUUID}) }
func (t *Table) Binary(name string) *Column {
	return t.add(name, dialect.Column{Kind: dialect.KindBinary})
}

// Enum is native on MySQL/MariaDB and varchar+CHECK elsewhere.
func (t *Table) Enum(name string, values ...string) *Column {
	if len(values) == 0 {
		t.fail("enum %q needs at least one value", name)
	}
	longest := 1
	for _, v := range values {
		if len(v) > longest {
			longest = len(v)
		}
	}
	return t.add(name, dialect.Column{Kind: dialect.KindEnum, Enum: values, Length: longest})
}

// ForeignID adds an unsigned, NOT NULL bigint meant to reference a BigIncrements key
// (Laravel's foreignId). Chain .Constrained() to add the foreign key or .Nullable() to allow NULL.
func (t *Table) ForeignID(name string) *Column {
	return t.BigInteger(name).Unsigned().NotNull()
}

// Morphs adds <name>_type and <name>_id (NOT NULL) plus a composite index, for polymorphic relations.
func (t *Table) Morphs(name string) { t.morphs(name, true) }

// NullableMorphs is Morphs with nullable columns.
func (t *Table) NullableMorphs(name string) { t.morphs(name, false) }

func (t *Table) morphs(name string, notNull bool) {
	typ := t.String(name + "_type")
	id := t.BigInteger(name + "_id").Unsigned()
	if notNull {
		typ.NotNull()
		id.NotNull()
	}
	t.Index(name+"_type", name+"_id")
}

// Timestamps adds nullable created_at and updated_at.
func (t *Table) Timestamps() {
	t.Timestamp("created_at")
	t.Timestamp("updated_at")
}

// SoftDeletes adds an indexed, nullable deleted_at (matches gorm.DeletedAt).
func (t *Table) SoftDeletes() { t.Timestamp("deleted_at").Index() }

// Primary sets a (composite) primary key.
func (t *Table) Primary(cols ...string) { t.primary = cols }

func (t *Table) Unique(cols ...string) *Index {
	i := &Index{cols: cols}
	t.uniques = append(t.uniques, i)
	return i
}

func (t *Table) Index(cols ...string) *Index {
	i := &Index{cols: cols}
	t.indexes = append(t.indexes, i)
	return i
}

// FullText adds a full-text index over cols, for Query.WhereFullText. MySQL gets a FULLTEXT index and
// PostgreSQL a GIN index on to_tsvector. SQLite, SQL Server and Oracle fail with UNSUPPORTED_FEATURE:
// they need a virtual table or a catalog that a column list cannot describe.
func (t *Table) FullText(cols ...string) *Index {
	i := &Index{cols: cols}
	t.fulltext = append(t.fulltext, i)
	return i
}

func (t *Table) Foreign(cols ...string) *Foreign {
	f := &Foreign{cols: cols}
	t.foreigns = append(t.foreigns, f)
	return f
}

// AlterTable-only operations.
func (t *Table) DropColumn(names ...string) { t.dropColumns = append(t.dropColumns, names...) }
func (t *Table) RenameColumn(from, to string) {
	t.renames = append(t.renames, [2]string{from, to})
}
func (t *Table) DropIndex(name string)   { t.dropIndexes = append(t.dropIndexes, name) }
func (t *Table) DropUnique(name string)  { t.dropUniques = append(t.dropUniques, name) }
func (t *Table) DropForeign(name string) { t.dropForeigns = append(t.dropForeigns, name) }

func (t *Table) objectName(cols []string, suffix string) string {
	raw := strings.ReplaceAll(t.name, ".", "_") + "_" + strings.Join(cols, "_") + "_" + suffix
	if len(raw) <= maxIdentLen {
		return raw
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(raw))
	return fmt.Sprintf("%s_%08x", raw[:maxIdentLen-9], h.Sum32())
}

func named(custom string, t *Table, cols []string, suffix string) string {
	if custom != "" {
		return custom
	}
	return t.objectName(cols, suffix)
}

func quoteAll(d dialect.Dialect, cols []string) string {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = d.Quote(c)
	}
	return strings.Join(q, ", ")
}

var fkActions = map[string]bool{"CASCADE": true, "SET NULL": true, "RESTRICT": true, "NO ACTION": true, "SET DEFAULT": true}

func (f *Foreign) sql(d dialect.Dialect, t *Table) (string, error) {
	if f.refTable == "" || len(f.refCols) == 0 || len(f.cols) == 0 {
		return "", migro.Newf(migro.InvalidSchema, "foreign key on %q needs columns and References(table, cols...)", t.name)
	}
	if len(f.cols) != len(f.refCols) {
		return "", migro.Newf(migro.InvalidSchema, "foreign key on %q: column count mismatch", t.name)
	}
	s := fmt.Sprintf("CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
		d.Quote(named(f.name, t, f.cols, "foreign")), quoteAll(d, f.cols), d.Quote(f.refTable), quoteAll(d, f.refCols))
	for _, a := range [][2]string{{"ON DELETE", f.onDelete}, {"ON UPDATE", f.onUpdate}} {
		if a[1] == "" {
			continue
		}
		up := strings.ToUpper(a[1])
		if !fkActions[up] {
			return "", migro.Newf(migro.InvalidSchema, "foreign key on %q: invalid action %q", t.name, a[1])
		}
		s += " " + a[0] + " " + up
	}
	return s, nil
}

func literal(d dialect.Dialect, v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "NULL", nil
	case string:
		return "'" + strings.ReplaceAll(x, "'", "''") + "'", nil
	case bool:
		return d.BoolLiteral(x), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(x), nil
	}
	return "", migro.Newf(migro.InvalidSchema, "unsupported default value type %T (use DefaultRaw)", v)
}

func (c *Column) sql(d dialect.Dialect, create bool) (string, error) {
	q := d.Quote(c.name)
	if c.auto {
		if !create {
			return "", migro.Newf(migro.InvalidSchema, "column %q: auto-increment columns cannot be added with AlterTable", c.name)
		}
		return q + " " + d.AutoIncrement(c.big), nil
	}
	typ := d.ColumnType(c.spec)
	if typ == "" {
		return "", migro.Newf(migro.UnsupportedFeature, "column %q: type %s unsupported on %s", c.name, c.spec.Kind, d.Name())
	}
	s := q + " " + typ
	if c.hasDefault {
		lit, err := literal(d, c.def)
		if err != nil {
			return "", err
		}
		s += " DEFAULT " + lit
	} else if c.defRaw != "" {
		s += " DEFAULT " + c.defRaw
	}
	if c.notNull || c.primary {
		s += " NOT NULL"
	}
	if c.spec.Kind == dialect.KindEnum && !d.NativeEnum() {
		vals := make([]string, len(c.spec.Enum))
		for i, v := range c.spec.Enum {
			vals[i] = "'" + strings.ReplaceAll(v, "'", "''") + "'"
		}
		s += fmt.Sprintf(" CHECK (%s IN (%s))", q, strings.Join(vals, ", "))
	}
	if c.primary {
		if !create {
			return "", migro.Newf(migro.InvalidSchema, "column %q: use Table.Primary() with AlterTable", c.name)
		}
		s += " PRIMARY KEY"
	}
	return s, nil
}

func (t *Table) compileCreate(d dialect.Dialect) ([]string, error) {
	if t.err != nil {
		return nil, t.err
	}
	if len(t.columns) == 0 {
		return nil, migro.Newf(migro.InvalidSchema, "table %q has no columns", t.name)
	}
	items := make([]string, 0, len(t.columns)+len(t.uniques)+len(t.foreigns)+1)
	for _, c := range t.columns {
		s, err := c.sql(d, true)
		if err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	if len(t.primary) > 0 {
		items = append(items, fmt.Sprintf("PRIMARY KEY (%s)", quoteAll(d, t.primary)))
	}
	for _, u := range t.uniques {
		items = append(items, fmt.Sprintf("CONSTRAINT %s UNIQUE (%s)", d.Quote(named(u.name, t, u.cols, "unique")), quoteAll(d, u.cols)))
	}
	for _, f := range t.foreigns {
		s, err := f.sql(d, t)
		if err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	stmts := []string{fmt.Sprintf("CREATE TABLE %s (\n  %s\n)", d.Quote(t.name), strings.Join(items, ",\n  "))}
	idx, err := t.indexSQL(d)
	if err != nil {
		return nil, err
	}
	return append(stmts, idx...), nil
}

func (t *Table) indexSQL(d dialect.Dialect) ([]string, error) {
	if len(t.indexes) > 0 && !d.SupportsIndexes() {
		return nil, migro.Newf(migro.UnsupportedFeature, "%s does not support CREATE INDEX", d.Name())
	}
	out := make([]string, 0, len(t.indexes))
	for _, i := range t.indexes {
		out = append(out, fmt.Sprintf("CREATE INDEX %s ON %s (%s)", d.Quote(named(i.name, t, i.cols, "index")), d.Quote(t.name), quoteAll(d, i.cols)))
	}
	for _, i := range t.fulltext {
		quoted := make([]string, len(i.cols))
		for n, c := range i.cols {
			quoted[n] = d.Quote(c)
		}
		stmt := dialect.FeaturesOf(d).FullTextIndex(d.Quote(t.name), d.Quote(named(i.name, t, i.cols, "fulltext")), quoted)
		if stmt == "" {
			return nil, migro.Newf(migro.UnsupportedFeature, "%s has no full-text index", d.Name())
		}
		out = append(out, stmt)
	}
	return out, nil
}

func (t *Table) compileAlter(d dialect.Dialect) ([]string, error) {
	if t.err != nil {
		return nil, t.err
	}
	q := d.Quote(t.name)
	var out []string
	drop := func(names []string, fn func(table, name string) string, what string) error {
		for _, n := range names {
			s := fn(t.name, n)
			if s == "" {
				return migro.Newf(migro.UnsupportedFeature, "%s: drop %s unsupported", d.Name(), what)
			}
			out = append(out, s)
		}
		return nil
	}
	// Constraints and indexes go first: databases reject dropping a column that an index or
	// foreign key still uses (SQLite, MySQL) or lose the constraint along with it (PostgreSQL).
	if err := drop(t.dropForeigns, d.DropForeign, "foreign key"); err != nil {
		return nil, err
	}
	if err := drop(t.dropUniques, d.DropConstraint, "unique constraint"); err != nil {
		return nil, err
	}
	if err := drop(t.dropIndexes, d.DropIndex, "index"); err != nil {
		return nil, err
	}
	for _, c := range t.columns {
		s, err := c.sql(d, false)
		if err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("ALTER TABLE %s %s %s", q, d.AddColumnKeyword(), s))
	}
	for _, c := range t.dropColumns {
		out = append(out, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", q, d.Quote(c)))
	}
	for _, r := range t.renames {
		out = append(out, d.RenameColumn(t.name, r[0], r[1]))
	}
	if len(t.primary) > 0 {
		if !d.SupportsAddConstraint() {
			return nil, migro.Newf(migro.UnsupportedFeature, "%s cannot add a primary key to an existing table", d.Name())
		}
		out = append(out, fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s)", q, quoteAll(d, t.primary)))
	}
	for _, u := range t.uniques {
		n := d.Quote(named(u.name, t, u.cols, "unique"))
		if d.SupportsAddConstraint() {
			out = append(out, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s)", q, n, quoteAll(d, u.cols)))
			continue
		}
		out = append(out, fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", n, q, quoteAll(d, u.cols)))
	}
	idx, err := t.indexSQL(d)
	if err != nil {
		return nil, err
	}
	out = append(out, idx...)
	for _, f := range t.foreigns {
		if !d.SupportsAddConstraint() {
			return nil, migro.Newf(migro.UnsupportedFeature, "%s cannot add a foreign key to an existing table", d.Name())
		}
		s, err := f.sql(d, t)
		if err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("ALTER TABLE %s ADD %s", q, s))
	}
	if len(out) == 0 {
		return nil, migro.Newf(migro.InvalidSchema, "alter table %q: nothing to do", t.name)
	}
	return out, nil
}
