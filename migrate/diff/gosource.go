package diff

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/dialect"
)

// TypeName converts snake_case ("create_users_table") to the Go type name ("CreateUsersTable").
func TypeName(name string) string {
	var b strings.Builder
	for _, part := range strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

func colCode(c ColumnDef) string {
	var b strings.Builder
	n := fmt.Sprintf("%q", c.Name)
	switch {
	case c.Auto && c.Big:
		fmt.Fprintf(&b, "t.BigIncrements(%s)", n)
		return b.String()
	case c.Auto:
		fmt.Fprintf(&b, "t.Increments(%s)", n)
		return b.String()
	}
	switch c.Kind {
	case dialect.KindString:
		fmt.Fprintf(&b, "t.String(%s, %d)", n, c.Length)
	case dialect.KindText:
		fmt.Fprintf(&b, "t.Text(%s)", n)
	case dialect.KindSmallInt:
		fmt.Fprintf(&b, "t.SmallInteger(%s)", n)
	case dialect.KindInteger:
		fmt.Fprintf(&b, "t.Integer(%s)", n)
	case dialect.KindBigInt:
		fmt.Fprintf(&b, "t.BigInteger(%s)", n)
	case dialect.KindBoolean:
		fmt.Fprintf(&b, "t.Boolean(%s)", n)
	case dialect.KindFloat:
		fmt.Fprintf(&b, "t.Float(%s)", n)
	case dialect.KindDouble:
		fmt.Fprintf(&b, "t.Double(%s)", n)
	case dialect.KindDecimal:
		fmt.Fprintf(&b, "t.Decimal(%s, %d, %d)", n, c.Precision, c.Scale)
	case dialect.KindDate:
		fmt.Fprintf(&b, "t.Date(%s)", n)
	case dialect.KindTime:
		fmt.Fprintf(&b, "t.Time(%s)", n)
	case dialect.KindDateTime:
		fmt.Fprintf(&b, "t.DateTime(%s)", n)
	case dialect.KindStamp:
		fmt.Fprintf(&b, "t.Timestamp(%s)", n)
	case dialect.KindJSON:
		fmt.Fprintf(&b, "t.JSON(%s)", n)
	case dialect.KindUUID:
		fmt.Fprintf(&b, "t.UUID(%s)", n)
	case dialect.KindBinary:
		fmt.Fprintf(&b, "t.Binary(%s)", n)
	default:
		fmt.Fprintf(&b, "t.Text(%s)", n)
	}
	if c.Unsigned {
		b.WriteString(".Unsigned()")
	}
	if c.NotNull {
		b.WriteString(".NotNull()")
	}
	if c.HasDefault {
		fmt.Fprintf(&b, ".Default(%#v)", c.Default)
	}
	if c.Primary {
		b.WriteString(".Primary()")
	}
	return b.String()
}

func strList(cols []string) string {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = fmt.Sprintf("%q", c)
	}
	return strings.Join(q, ", ")
}

func indexCode(i IndexDef) string {
	if i.Unique {
		return fmt.Sprintf("t.Unique(%s).Named(%q)", strList(i.Columns), i.Name)
	}
	return fmt.Sprintf("t.Index(%s).Named(%q)", strList(i.Columns), i.Name)
}

func alter(table string, lines ...string) string {
	return fmt.Sprintf("if err := s.AlterTable(ctx, %q, func(t *schema.Table) {\n%s\n}); err != nil {\n\treturn err\n}\n", table, strings.Join(lines, "\n"))
}

func (op Op) goUpDown() (up, down string) {
	switch op.Kind {
	case CreateTable:
		var lines []string
		for _, c := range op.Def.Columns {
			lines = append(lines, colCode(c))
		}
		if len(op.Def.Primary) > 1 {
			lines = append(lines, fmt.Sprintf("t.Primary(%s)", strList(op.Def.Primary)))
		}
		for _, u := range op.Def.Uniques {
			lines = append(lines, fmt.Sprintf("t.Unique(%s)", strList(u)))
		}
		for _, i := range op.Def.Indexes {
			lines = append(lines, indexCode(i))
		}
		up = fmt.Sprintf("if err := s.CreateTable(ctx, %q, func(t *schema.Table) {\n%s\n}); err != nil {\n\treturn err\n}\n", op.Table, strings.Join(lines, "\n"))
		down = fmt.Sprintf("if err := s.DropTableIfExists(ctx, %q); err != nil {\n\treturn err\n}\n", op.Table)
	case AddColumn:
		up = alter(op.Table, colCode(*op.Column))
		down = alter(op.Table, fmt.Sprintf("t.DropColumn(%q)", op.Column.Name))
	case DropColumn:
		up = alter(op.Table, fmt.Sprintf("t.DropColumn(%q)", op.From))
		down = fmt.Sprintf("// irreversible: column %s.%s was dropped and its data cannot be restored", op.Table, op.From)
		if op.Note != "" {
			down += " (" + op.Note + ")"
		}
		down += "\n"
	case RenameColumn:
		up = alter(op.Table, fmt.Sprintf("t.RenameColumn(%q, %q)", op.From, op.To))
		down = alter(op.Table, fmt.Sprintf("t.RenameColumn(%q, %q)", op.To, op.From))
	case AddIndex:
		up = alter(op.Table, indexCode(*op.Index))
		down = alter(op.Table, dropIndexCode(*op.Index))
	case DropIndex:
		up = alter(op.Table, dropIndexCode(*op.Index))
		down = alter(op.Table, indexCode(*op.Index))
	}
	return up, down
}

func dropIndexCode(i IndexDef) string {
	if i.Unique {
		return fmt.Sprintf("t.DropUnique(%q)", i.Name)
	}
	return fmt.Sprintf("t.DropIndex(%q)", i.Name)
}

// GoSource renders the plan as a Go migration file body for package pkg.
func (p Plan) GoSource(pkg, typeName string) (string, error) {
	var up, down strings.Builder
	downParts := make([]string, 0, len(p.Ops))
	for _, op := range p.Ops {
		u, d := op.goUpDown()
		up.WriteString(u)
		downParts = append(downParts, d)
	}
	for i := len(downParts) - 1; i >= 0; i-- {
		down.WriteString(downParts[i])
	}
	src := fmt.Sprintf(`package %[1]s

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

func init() { migrate.Register(&%[2]s{}) }

// %[2]s was generated by migro from the model definitions. Review it before running.
type %[2]s struct{}

func (%[2]s) Up(ctx context.Context, s *schema.Builder) error {
%[3]s
	return nil
}

func (%[2]s) Down(ctx context.Context, s *schema.Builder) error {
%[4]s
	return nil
}
`, pkg, typeName, up.String(), down.String())
	out, err := format.Source([]byte(src))
	if err != nil {
		return "", migro.Wrap(migro.DiffFailed, err, "format generated migration")
	}
	return string(out), nil
}

// WriteGoFile writes <timestamp>_<name>.go into dir (package pkg) and returns its path.
// An empty plan writes nothing and returns "".
func (p Plan) WriteGoFile(dir, pkg, name string, now time.Time) (string, error) {
	if p.Empty() {
		return "", nil
	}
	snake := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return unicode.ToLower(r)
		}
		return '_'
	}, name)
	if snake == "" {
		snake = "sync_models"
	}
	src, err := p.GoSource(pkg, TypeName(snake))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", migro.Wrap(migro.DiffFailed, err, "create %s", dir)
	}
	path := filepath.Join(dir, now.UTC().Format("20060102150405")+"_"+snake+".go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		return "", migro.Wrap(migro.DiffFailed, err, "write %s", path)
	}
	return path, nil
}
