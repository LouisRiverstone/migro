package console

import (
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/migrate/diff"
)

// snake turns "UserSeeder", "create-users" or "Create Users" into "user_seeder", "create_users".
func snake(s string) string {
	var b strings.Builder
	var prev rune
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			if prev != 0 && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsLower(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "_") {
				b.WriteByte('_')
			}
		}
		prev = r
	}
	return strings.Trim(b.String(), "_")
}

func pluralize(word string) string {
	switch {
	case strings.HasSuffix(word, "y") && !strings.HasSuffix(word, "ey") && !strings.HasSuffix(word, "ay") && !strings.HasSuffix(word, "oy"):
		return word[:len(word)-1] + "ies"
	case strings.HasSuffix(word, "s"), strings.HasSuffix(word, "x"), strings.HasSuffix(word, "ch"), strings.HasSuffix(word, "sh"):
		return word + "es"
	}
	return word + "s"
}

// detectPackage reuses the package of existing files in dir, else derives one from the folder name.
func detectPackage(dir, fallback string) string {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		if af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.PackageClauseOnly); err == nil {
			return af.Name.Name
		}
	}
	base := snake(filepath.Base(dir))
	base = strings.ReplaceAll(base, "_", "")
	if base == "" || unicode.IsDigit(rune(base[0])) || base == "main" {
		return fallback
	}
	return base
}

func readModule(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`), nil
		}
	}
	return "", fmt.Errorf("no module line in go.mod")
}

// writeNew formats src and writes it, refusing to overwrite.
func writeNew(path, src string) error {
	out, err := format.Source([]byte(src))
	if err != nil {
		return migro.Wrap(migro.InvalidSchema, err, "generated code for %s is invalid", path)
	}
	if _, err := os.Stat(path); err == nil {
		return migro.Newf(migro.InvalidConfig, "%s already exists", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return migro.Wrap(migro.InvalidConfig, err, "create %s", filepath.Dir(path))
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return migro.Wrap(migro.InvalidConfig, err, "write %s", path)
	}
	return nil
}

var (
	createRe = regexp.MustCompile(`^create_(.+?)(?:_table)?$`)
	alterRe  = regexp.MustCompile(`^(?:add|remove|drop|change|update|modify|rename)_.+?_(?:to|from|in|on)_(.+?)(?:_table)?$`)
)

type migrationKind int

const (
	blankMigration migrationKind = iota
	createMigration
	alterMigration
)

func migrationSource(pkg, typ string, kind migrationKind, table string) string {
	head := fmt.Sprintf(`package %s

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

func init() { migrate.Register(&%s{}) }

type %s struct{}
`, pkg, typ, typ)
	switch kind {
	case createMigration:
		return head + fmt.Sprintf(`
func (%[1]s) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, %[2]q, func(t *schema.Table) {
		t.BigIncrements("id")
		t.Timestamps()
	})
}

func (%[1]s) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, %[2]q)
}
`, typ, table)
	case alterMigration:
		return head + fmt.Sprintf(`
func (%[1]s) Up(ctx context.Context, s *schema.Builder) error {
	return s.AlterTable(ctx, %[2]q, func(t *schema.Table) {
		// t.String("column")
		// t.Index("column")
	})
}

func (%[1]s) Down(ctx context.Context, s *schema.Builder) error {
	return s.AlterTable(ctx, %[2]q, func(t *schema.Table) {
		// t.DropIndex("...")
		// t.DropColumn("column")
	})
}
`, typ, table)
	}
	return head + fmt.Sprintf(`
func (%[1]s) Up(ctx context.Context, s *schema.Builder) error {
	return nil
}

func (%[1]s) Down(ctx context.Context, s *schema.Builder) error {
	return nil
}
`, typ)
}

// newMigration creates the file and returns its path.
func (c *call) newMigration(rawName, create, table string) (string, error) {
	name := snake(rawName)
	if name == "" || !unicode.IsLetter(rune(name[0])) {
		return "", usagef("migration name must start with a letter, got %q", rawName)
	}
	kind := blankMigration
	switch {
	case create != "":
		kind, table = createMigration, create
	case table != "":
		kind = alterMigration
	default:
		if m := createRe.FindStringSubmatch(name); m != nil {
			kind, table = createMigration, m[1]
		} else if m := alterRe.FindStringSubmatch(name); m != nil {
			kind, table = alterMigration, m[1]
		}
	}
	dir := c.dir("path", c.app.MigrationsDir)
	if existing, _ := filepath.Glob(filepath.Join(dir, "*_"+name+".go")); len(existing) > 0 {
		return "", migro.Newf(migro.InvalidConfig, "a migration named %q already exists (%s)", name, filepath.Base(existing[0]))
	}
	path := filepath.Join(dir, nextTimestamp(dir, c.app.Now())+"_"+name+".go")
	src := migrationSource(detectPackage(dir, "migrations"), diff.TypeName(name), kind, table)
	return path, writeNew(path, src)
}

const stampLayout = "20060102150405"

// nextTimestamp returns now, or one second after the newest migration in dir when that is later,
// so migrations created in quick succession keep the order they were made in.
func nextTimestamp(dir string, now time.Time) string {
	ts := now.UTC().Truncate(time.Second) // file stamps have whole-second precision
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, f := range files {
		base := filepath.Base(f)
		if len(base) < len(stampLayout)+1 || base[len(stampLayout)] != '_' {
			continue
		}
		if t, err := time.ParseInLocation(stampLayout, base[:len(stampLayout)], time.UTC); err == nil && !t.Before(ts) {
			ts = t.Add(time.Second)
		}
	}
	return ts.Format(stampLayout)
}

func makeMigration(c *call) error {
	if len(c.args) != 1 {
		return usagef("exactly one migration name is required")
	}
	if c.flags.has("create") && c.flags.has("table") {
		return usagef("use either --create or --table, not both")
	}
	path, err := c.newMigration(c.args[0], c.flags.str("create"), c.flags.str("table"))
	if err != nil {
		return err
	}
	c.printf("Created Migration: %s\n", relTo(c.app.Root, path))
	return nil
}

// ---- seeder / factory / model ----------------------------------------------------------------

func baseName(raw, suffix string) (string, error) {
	n := diff.TypeName(snake(raw))
	n = strings.TrimSuffix(n, suffix)
	if n == "" || !unicode.IsLetter(rune(n[0])) {
		return "", usagef("invalid name %q", raw)
	}
	return n, nil
}

func (c *call) newSeeder(base string) (string, error) {
	dir := c.dir("path", c.app.SeedersDir)
	typ := base + "Seeder"
	path := filepath.Join(dir, snake(typ)+".go")
	src := fmt.Sprintf(`package %s

import (
	"context"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/seed"
)

func init() { seed.Register(&%[2]s{}) }

type %[2]s struct{}

// Run seeds data. Write it so running twice is harmless, or use `+"`db:seed --once`"+`.
func (%[2]s) Run(ctx context.Context, db *migro.DB) error {
	// _, err := factories.%[3]sFactory().CreateMany(ctx, db, 10)
	// return err
	return nil
}
`, detectPackage(dir, "seeders"), typ, base)
	return path, writeNew(path, src)
}

func makeSeeder(c *call) error {
	if len(c.args) != 1 {
		return usagef("exactly one seeder name is required")
	}
	base, err := baseName(c.args[0], "Seeder")
	if err != nil {
		return err
	}
	path, err := c.newSeeder(base)
	if err != nil {
		return err
	}
	c.printf("Created Seeder: %s\n", relTo(c.app.Root, path))
	return nil
}

// modelRef returns the import path and qualified type of the model for a factory.
func (c *call) modelRef(base, flag string) (importPath, qualified string, err error) {
	if flag != "" {
		slash := strings.LastIndex(flag, "/")
		dot := strings.LastIndex(flag, ".")
		if dot <= slash || dot == len(flag)-1 {
			return "", "", usagef("--model must look like github.com/me/app/models.User, got %q", flag)
		}
		importPath = flag[:dot]
		return importPath, filepath.Base(importPath) + "." + flag[dot+1:], nil
	}
	mod, err := readModule(c.app.Root)
	if err != nil {
		return "", "", migro.Wrap(migro.InvalidConfig, err, "cannot infer the model package (no readable go.mod); pass --model")
	}
	importPath = mod + "/" + filepath.ToSlash(c.app.ModelsDir)
	pkg := detectPackage(filepath.Join(c.app.Root, c.app.ModelsDir), "models")
	return importPath, pkg + "." + base, nil
}

func (c *call) newFactory(base, modelFlag string) (string, error) {
	importPath, qualified, err := c.modelRef(base, modelFlag)
	if err != nil {
		return "", err
	}
	dir := c.dir("path", c.app.FactoriesDir)
	path := filepath.Join(dir, snake(base)+"_factory.go")
	src := fmt.Sprintf(`package %[1]s

import (
	"github.com/LouisRiverstone/migro/seed"
	%[2]q
)

// %[3]sFactory builds %[4]s values. Fill in the defaults, then use it from seeders:
//
//	%[3]sFactory().CreateMany(ctx, db, 10)
func %[3]sFactory() *seed.Factory[%[4]s] {
	return seed.Define(func(f *seed.Faker) %[4]s {
		return %[4]s{
			// Name:  f.Name(),
			// Email: f.Email(),
		}
	})
	// .State("admin", func(m *%[4]s) { /* ... */ })
	// .Relation("posts", func(ctx context.Context, db *migro.DB, parent *%[4]s, n int, f *seed.Faker) error { ... })
}
`, detectPackage(dir, "factories"), importPath, base, qualified)
	return path, writeNew(path, src)
}

func makeFactory(c *call) error {
	if len(c.args) != 1 {
		return usagef("exactly one factory name is required")
	}
	base, err := baseName(c.args[0], "Factory")
	if err != nil {
		return err
	}
	path, err := c.newFactory(base, c.flags.str("model"))
	if err != nil {
		return err
	}
	c.printf("Created Factory: %s\n", relTo(c.app.Root, path))
	return nil
}

func makeModel(c *call) error {
	if len(c.args) != 1 {
		return usagef("exactly one model name is required")
	}
	base, err := baseName(c.args[0], "")
	if err != nil {
		return err
	}
	dir := c.dir("path", c.app.ModelsDir)
	table := pluralize(snake(base))
	path := filepath.Join(dir, snake(base)+".go")
	src := fmt.Sprintf(`package %s

import "github.com/LouisRiverstone/migro/orm"

// %[2]s maps to the %[3]q table (GORM's default naming).
type %[2]s struct {
	orm.BaseModel
	// Name  string
	// Email string `+"`gorm:\"uniqueIndex\"`"+`
}
`, detectPackage(dir, "models"), base, table)
	if err := writeNew(path, src); err != nil {
		return err
	}
	c.printf("Created Model: %s\n", relTo(c.app.Root, path))

	all := c.flags.isTrue("all")
	if all || c.flags.isTrue("migration") {
		// The model's own --path must not leak into the migration directory.
		sub := &call{app: c.app, ctx: c.ctx, flags: parsedArgs{flags: map[string][]string{}}}
		p, err := sub.newMigration("create_"+table+"_table", table, "")
		if err != nil {
			return err
		}
		c.printf("Created Migration: %s\n", relTo(c.app.Root, p))
	}
	if all || c.flags.isTrue("factory") {
		sub := &call{app: c.app, ctx: c.ctx, flags: parsedArgs{flags: map[string][]string{}}}
		p, err := sub.newFactory(base, "")
		if err != nil {
			return err
		}
		c.printf("Created Factory: %s\n", relTo(c.app.Root, p))
	}
	if all || c.flags.isTrue("seeder") {
		sub := &call{app: c.app, ctx: c.ctx, flags: parsedArgs{flags: map[string][]string{}}}
		p, err := sub.newSeeder(base)
		if err != nil {
			return err
		}
		c.printf("Created Seeder: %s\n", relTo(c.app.Root, p))
	}
	return nil
}
