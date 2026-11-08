// Package diff compares GORM models with a live database and writes the difference
// as migration files (the same .up.sql / .down.sql format migrate.FromFS reads).
//
// It detects new tables, added/removed/renamed columns and added/removed indexes.
// It deliberately does NOT detect column type or nullability changes (type names differ per
// database and would cause false positives); those are reported as warnings.
package diff

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/dialect"
	"github.com/LouisRiverstone/migro/schema"
	"gorm.io/gorm"
	gschema "gorm.io/gorm/schema"
)

type OpKind string

const (
	CreateTable  OpKind = "create_table"
	AddColumn    OpKind = "add_column"
	DropColumn   OpKind = "drop_column"
	RenameColumn OpKind = "rename_column"
	AddIndex     OpKind = "add_index"
	DropIndex    OpKind = "drop_index"
)

// ColumnDef is a database-agnostic column description.
type ColumnDef struct {
	Name       string
	Kind       dialect.Kind
	Length     int
	Precision  int
	Scale      int
	Unsigned   bool
	NotNull    bool
	Primary    bool
	Auto       bool
	Big        bool
	HasDefault bool
	Default    any
}

type IndexDef struct {
	Name    string
	Columns []string
	Unique  bool
}

type TableDef struct {
	Name    string
	Columns []ColumnDef
	Primary []string // composite primary key columns (len > 1)
	Uniques [][]string
	Indexes []IndexDef
}

// Op is one planned change.
type Op struct {
	Kind   OpKind
	Table  string
	Def    *TableDef  // CreateTable
	Column *ColumnDef // AddColumn
	From   string     // RenameColumn old name; DropColumn name
	To     string     // RenameColumn new name
	Index  *IndexDef  // AddIndex / DropIndex
	Note   string     // human hint, rendered as a SQL comment in the down file
}

// Plan is the result of Generate.
type Plan struct {
	Ops []Op
	// Blocked holds destructive ops (drops) that were not applied because AllowDestructive is false.
	Blocked  []Op
	Warnings []string
	dialect  dialect.Dialect
}

// Options tune Generate.
type Options struct {
	// AllowDestructive includes DropColumn and DropIndex in Ops.
	AllowDestructive bool
	// Renames maps "table.old_column" to "new_column" so a rename is not planned as drop+add.
	Renames map[string]string
}

// Generate computes the plan for models (pass pointers or values, e.g. &User{}).
func Generate(ctx context.Context, db *migro.DB, opts Options, models ...any) (Plan, error) {
	plan := Plan{dialect: db.Dialect}
	m := db.Gorm.WithContext(ctx).Migrator()
	for _, model := range models {
		stmt := &gorm.Statement{DB: db.Gorm}
		if err := stmt.Parse(model); err != nil {
			return Plan{}, migro.Wrap(migro.DiffFailed, err, "parse model %T", model)
		}
		sch := stmt.Schema
		def, warns := tableDef(sch)
		plan.Warnings = append(plan.Warnings, warns...)

		if !m.HasTable(sch.Table) {
			plan.Ops = append(plan.Ops, Op{Kind: CreateTable, Table: sch.Table, Def: &def})
			continue
		}
		if err := diffExisting(m, &plan, def, opts); err != nil {
			return Plan{}, err
		}
	}
	return plan, nil
}

func diffExisting(m gorm.Migrator, plan *Plan, def TableDef, opts Options) error {
	cols, err := m.ColumnTypes(def.Name)
	if err != nil {
		return migro.Wrap(migro.DiffFailed, err, "introspect %s", def.Name)
	}
	live := map[string]gorm.ColumnType{}
	for _, c := range cols {
		live[c.Name()] = c
	}
	want := map[string]ColumnDef{}
	for _, c := range def.Columns {
		want[c.Name] = c
	}

	renamedFrom := map[string]bool{}
	for _, c := range def.Columns {
		if _, ok := live[c.Name]; ok {
			if lc := live[c.Name]; c.NotNull && !c.Primary {
				if nullable, known := lc.Nullable(); known && nullable {
					plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s.%s is NOT NULL in the model but nullable in the database (not migrated)", def.Name, c.Name))
				}
			}
			continue
		}
		if from := renameSource(def.Name, c.Name, opts.Renames, live, want); from != "" {
			renamedFrom[from] = true
			plan.Ops = append(plan.Ops, Op{Kind: RenameColumn, Table: def.Name, From: from, To: c.Name})
			continue
		}
		cc := c
		plan.Ops = append(plan.Ops, Op{Kind: AddColumn, Table: def.Name, Column: &cc})
		if c.NotNull && !c.HasDefault && !c.Auto {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s.%s is NOT NULL without a default: adding it fails when the table already has rows", def.Name, c.Name))
		}
	}

	liveNames := make([]string, 0, len(live))
	for n := range live {
		liveNames = append(liveNames, n)
	}
	sort.Strings(liveNames)
	for _, n := range liveNames {
		if _, ok := want[n]; ok || renamedFrom[n] {
			continue
		}
		op := Op{Kind: DropColumn, Table: def.Name, From: n}
		if t := live[n].DatabaseTypeName(); t != "" {
			op.Note = "type " + t
		}
		if opts.AllowDestructive {
			plan.Ops = append(plan.Ops, op)
			continue
		}
		plan.Blocked = append(plan.Blocked, op)
	}

	return diffIndexes(m, plan, def, opts)
}

func renameSource(table, newName string, renames map[string]string, live map[string]gorm.ColumnType, want map[string]ColumnDef) string {
	for key, to := range renames {
		if to != newName {
			continue
		}
		tbl, old, ok := strings.Cut(key, ".")
		if !ok || tbl != table {
			continue
		}
		if _, exists := live[old]; exists {
			if _, stillWanted := want[old]; !stillWanted {
				return old
			}
		}
	}
	return ""
}

func diffIndexes(m gorm.Migrator, plan *Plan, def TableDef, opts Options) error {
	idx, err := m.GetIndexes(def.Name)
	if err != nil {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s: could not introspect indexes (%v); index diff skipped", def.Name, err))
		return nil
	}
	// Match by column set, not name: constraint-backed unique indexes get driver-generated names.
	sig := func(cols []string, unique bool) string {
		c := append([]string(nil), cols...)
		return fmt.Sprintf("%v|%v", c, unique)
	}
	have := map[string]bool{}
	for _, i := range idx {
		u, _ := i.Unique()
		have[sig(i.Columns(), u)] = true
	}
	wantSigs := map[string]bool{}
	for _, want := range def.Indexes {
		k := sig(want.Columns, want.Unique)
		wantSigs[k] = true
		// Constraint-backed uniques may be invisible to GetIndexes; fall back to name lookups.
		if have[k] || m.HasConstraint(def.Name, want.Name) || m.HasIndex(def.Name, want.Name) {
			continue
		}
		w := want
		plan.Ops = append(plan.Ops, Op{Kind: AddIndex, Table: def.Name, Index: &w})
	}
	for _, u := range def.Uniques {
		wantSigs[sig(u, true)] = true
	}
	for _, i := range idx {
		cols := i.Columns()
		unique, _ := i.Unique()
		if wantSigs[sig(cols, unique)] || systemIndex(i) {
			continue
		}
		op := Op{Kind: DropIndex, Table: def.Name, Index: &IndexDef{Name: i.Name(), Columns: cols, Unique: unique}}
		if opts.AllowDestructive {
			plan.Ops = append(plan.Ops, op)
			continue
		}
		plan.Blocked = append(plan.Blocked, op)
	}
	return nil
}

// systemIndex hides primary keys and constraint-backed indexes GORM creates implicitly.
func systemIndex(i gorm.Index) bool {
	if pk, ok := i.PrimaryKey(); ok && pk {
		return true
	}
	n := strings.ToLower(i.Name())
	return strings.HasPrefix(n, "sqlite_autoindex") || strings.HasPrefix(n, "uni_") || strings.HasSuffix(n, "_pkey") || n == "primary"
}

// ---- model → definition ------------------------------------------------------------------

var sizedType = regexp.MustCompile(`^(\w+)\((\d+)(?:\s*,\s*(\d+))?\)`)

func tableDef(sch *gschema.Schema) (TableDef, []string) {
	def := TableDef{Name: sch.Table}
	var warns []string
	var primaries []string
	for _, f := range sch.Fields {
		if f.DBName == "" || f.IgnoreMigration {
			continue
		}
		c, w := columnDef(sch.Table, f)
		warns = append(warns, w...)
		def.Columns = append(def.Columns, c)
		if f.PrimaryKey {
			primaries = append(primaries, f.DBName)
		}
		if f.Unique {
			def.Uniques = append(def.Uniques, []string{f.DBName})
		}
	}
	if len(primaries) > 1 {
		def.Primary = primaries
		for i := range def.Columns {
			def.Columns[i].Primary = false
		}
	}
	idx := sch.ParseIndexes()
	sort.Slice(idx, func(a, b int) bool { return idx[a].Name < idx[b].Name })
	for _, i := range idx {
		d := IndexDef{Name: i.Name, Unique: strings.EqualFold(i.Class, "UNIQUE")}
		for _, o := range i.Fields {
			d.Columns = append(d.Columns, o.DBName)
		}
		def.Indexes = append(def.Indexes, d)
	}
	return def, warns
}

func columnDef(table string, f *gschema.Field) (ColumnDef, []string) {
	c := ColumnDef{Name: f.DBName, NotNull: f.NotNull || f.PrimaryKey, Primary: f.PrimaryKey}
	var warns []string
	if f.HasDefaultValue && f.DefaultValueInterface != nil {
		c.HasDefault, c.Default = true, f.DefaultValueInterface
	} else if f.DefaultValue != "" {
		warns = append(warns, fmt.Sprintf("%s.%s: default %q is an expression and was skipped; add it by hand", table, f.DBName, f.DefaultValue))
	}

	raw := strings.ToLower(string(f.DataType))
	size := f.Size
	if m := sizedType.FindStringSubmatch(raw); m != nil {
		raw = m[1]
		n, _ := strconv.Atoi(m[2])
		if m[3] != "" {
			c.Precision, c.Scale = n, atoi(m[3])
		} else {
			size = n
		}
	}
	if f.Precision > 0 {
		c.Precision, c.Scale = f.Precision, f.Scale
	}

	switch raw {
	case "int", "uint", "integer", "bigint", "smallint", "tinyint", "mediumint":
		fillInteger(&c, raw, size, f)
	case "float", "double", "real":
		if c.Precision > 0 {
			c.Kind = dialect.KindDecimal
		} else if raw == "float" && size == 32 || raw == "real" {
			c.Kind = dialect.KindFloat
		} else {
			c.Kind = dialect.KindDouble
		}
	case "decimal", "numeric":
		c.Kind = dialect.KindDecimal
	case "bool", "boolean":
		c.Kind = dialect.KindBoolean
	case "string", "varchar", "char", "nvarchar", "varchar2":
		if size == 0 {
			size = 255
		}
		c.Kind, c.Length = dialect.KindString, size
		if size > 4000 {
			c.Kind, c.Length = dialect.KindText, 0
		}
	case "text", "longtext", "mediumtext", "clob":
		c.Kind = dialect.KindText
	case "time": // gorm's DataType for time.Time and gorm.DeletedAt
		c.Kind = dialect.KindStamp
	case "date":
		c.Kind = dialect.KindDate
	case "datetime":
		c.Kind = dialect.KindDateTime
	case "timestamp", "timestamptz":
		c.Kind = dialect.KindStamp
	case "bytes", "blob", "bytea", "binary", "varbinary":
		c.Kind = dialect.KindBinary
	case "json", "jsonb":
		c.Kind = dialect.KindJSON
	case "uuid":
		c.Kind = dialect.KindUUID
	default:
		c.Kind = dialect.KindText
		warns = append(warns, fmt.Sprintf("%s.%s: unknown type %q mapped to text", table, f.DBName, f.DataType))
	}
	return c, warns
}

func fillInteger(c *ColumnDef, raw string, size int, f *gschema.Field) {
	switch {
	case raw == "smallint" || raw == "tinyint" || (size > 0 && size <= 16):
		c.Kind = dialect.KindSmallInt
	case raw == "bigint" || size == 0 || size > 32:
		c.Kind = dialect.KindBigInt
	default:
		c.Kind = dialect.KindInteger
	}
	c.Unsigned = f.GORMDataType == gschema.Uint
	if f.AutoIncrement && f.PrimaryKey {
		c.Auto, c.Big = true, c.Kind == dialect.KindBigInt
	}
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// ---- rendering ---------------------------------------------------------------------------

func apply(t *schema.Table, c ColumnDef) {
	var col *schema.Column
	switch {
	case c.Auto && c.Big:
		col = t.BigIncrements(c.Name)
	case c.Auto:
		col = t.Increments(c.Name)
	default:
		switch c.Kind {
		case dialect.KindString:
			col = t.String(c.Name, c.Length)
		case dialect.KindText:
			col = t.Text(c.Name)
		case dialect.KindSmallInt:
			col = t.SmallInteger(c.Name)
		case dialect.KindInteger:
			col = t.Integer(c.Name)
		case dialect.KindBigInt:
			col = t.BigInteger(c.Name)
		case dialect.KindBoolean:
			col = t.Boolean(c.Name)
		case dialect.KindFloat:
			col = t.Float(c.Name)
		case dialect.KindDouble:
			col = t.Double(c.Name)
		case dialect.KindDecimal:
			col = t.Decimal(c.Name, c.Precision, c.Scale)
		case dialect.KindDate:
			col = t.Date(c.Name)
		case dialect.KindTime:
			col = t.Time(c.Name)
		case dialect.KindDateTime:
			col = t.DateTime(c.Name)
		case dialect.KindStamp:
			col = t.Timestamp(c.Name)
		case dialect.KindJSON:
			col = t.JSON(c.Name)
		case dialect.KindUUID:
			col = t.UUID(c.Name)
		case dialect.KindBinary:
			col = t.Binary(c.Name)
		default:
			col = t.Text(c.Name)
		}
		if c.Unsigned {
			col.Unsigned()
		}
		if c.NotNull {
			col.NotNull()
		}
		if c.HasDefault {
			col.Default(c.Default)
		}
		if c.Primary {
			col.Primary()
		}
	}
}

// Render returns the up and down SQL. Down statements are in reverse order; changes that cannot
// be reversed (dropped columns) appear as SQL comments.
func (p Plan) Render() (up, down []string, err error) {
	d := p.dialect
	if d == nil {
		return nil, nil, migro.Newf(migro.DiffFailed, "plan has no dialect (use Generate)")
	}
	var downRev [][]string
	for _, op := range p.Ops {
		u, dn, err := renderOp(d, op)
		if err != nil {
			return nil, nil, err
		}
		up = append(up, u...)
		downRev = append(downRev, dn)
	}
	for i := len(downRev) - 1; i >= 0; i-- {
		down = append(down, downRev[i]...)
	}
	return up, down, nil
}

func renderOp(d dialect.Dialect, op Op) (up, down []string, err error) {
	wrapErr := func(err error) error {
		if err == nil {
			return nil
		}
		return migro.Wrap(migro.DiffFailed, err, "render %s on %s", op.Kind, op.Table)
	}
	switch op.Kind {
	case CreateTable:
		def := op.Def
		up, err = schema.CreateSQL(d, op.Table, func(t *schema.Table) {
			for _, c := range def.Columns {
				apply(t, c)
			}
			if len(def.Primary) > 1 {
				t.Primary(def.Primary...)
			}
			for _, u := range def.Uniques {
				t.Unique(u...)
			}
			for _, i := range def.Indexes {
				if i.Unique {
					t.Unique(i.Columns...).Named(i.Name)
					continue
				}
				t.Index(i.Columns...).Named(i.Name)
			}
		})
		return up, []string{"DROP TABLE " + d.Quote(op.Table)}, wrapErr(err)
	case AddColumn:
		up, err = schema.AlterSQL(d, op.Table, func(t *schema.Table) { apply(t, *op.Column) })
		return up, []string{"ALTER TABLE " + d.Quote(op.Table) + " DROP COLUMN " + d.Quote(op.Column.Name)}, wrapErr(err)
	case DropColumn:
		up, err = schema.AlterSQL(d, op.Table, func(t *schema.Table) { t.DropColumn(op.From) })
		note := "-- irreversible: column " + op.Table + "." + op.From + " was dropped; its data cannot be restored"
		if op.Note != "" {
			note += " (" + op.Note + ")"
		}
		return up, []string{note}, wrapErr(err)
	case RenameColumn:
		return []string{d.RenameColumn(op.Table, op.From, op.To)}, []string{d.RenameColumn(op.Table, op.To, op.From)}, nil
	case AddIndex:
		i := op.Index
		up, err = schema.AlterSQL(d, op.Table, func(t *schema.Table) {
			if i.Unique {
				t.Unique(i.Columns...).Named(i.Name)
				return
			}
			t.Index(i.Columns...).Named(i.Name)
		})
		drop := d.DropIndex(op.Table, i.Name)
		if i.Unique {
			drop = d.DropConstraint(op.Table, i.Name)
		}
		return up, []string{drop}, wrapErr(err)
	case DropIndex:
		i := op.Index
		drop := d.DropIndex(op.Table, i.Name)
		kind := "INDEX"
		if i.Unique {
			kind = "UNIQUE INDEX"
		}
		qs := make([]string, len(i.Columns))
		for n, c := range i.Columns {
			qs[n] = d.Quote(c)
		}
		recreate := fmt.Sprintf("CREATE %s %s ON %s (%s)", kind, d.Quote(i.Name), d.Quote(op.Table), strings.Join(qs, ", "))
		return []string{drop}, []string{recreate}, nil
	}
	return nil, nil, migro.Newf(migro.DiffFailed, "unknown op %q", op.Kind)
}

// Empty reports whether there is nothing to apply.
func (p Plan) Empty() bool { return len(p.Ops) == 0 }

// WriteFiles writes <timestamp>_<name>.up.sql and .down.sql into dir and returns their paths.
// An empty plan writes nothing and returns (nil, nil).
func (p Plan) WriteFiles(dir, name string) ([]string, error) {
	if p.Empty() {
		return nil, nil
	}
	up, down, err := p.Render()
	if err != nil {
		return nil, err
	}
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return '_'
	}, name)
	if name == "" {
		name = "auto_diff"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, migro.Wrap(migro.DiffFailed, err, "create %s", dir)
	}
	base := time.Now().UTC().Format("20060102150405") + "_" + name
	paths := []string{filepath.Join(dir, base+".up.sql"), filepath.Join(dir, base+".down.sql")}
	for i, stmts := range [][]string{up, down} {
		if err := os.WriteFile(paths[i], []byte(join(stmts)), 0o644); err != nil {
			return nil, migro.Wrap(migro.DiffFailed, err, "write %s", paths[i])
		}
	}
	return paths, nil
}

func join(stmts []string) string {
	var b strings.Builder
	for _, s := range stmts {
		b.WriteString(s)
		if !strings.HasPrefix(s, "--") {
			b.WriteString(";")
		}
		b.WriteString("\n\n")
	}
	return b.String()
}
