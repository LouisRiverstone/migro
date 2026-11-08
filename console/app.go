// Package console is the artisan/ace-style command line: migration:run, migration:rollback --step=N,
// make:migration, db:seed and friends. Embed it in your app (App.Run) or use the `migro` launcher.
package console

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/seed"
)

// Connection opens a database on demand, so make:* commands never need one.
type Connection func(ctx context.Context) (*migro.DB, error)

// Exit codes returned by Run.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// App wires commands to your project. Zero values get defaults in Run.
type App struct {
	// Connections by name; Default names the one used without --database.
	Connections map[string]Connection
	Default     string

	// Env is the application environment ("production" triggers the safety prompt).
	// Defaults to MIGRO_ENV, then APP_ENV, then "development".
	Env string

	// Directories, relative to Root, used by make:* and migration:diff.
	Root          string
	MigrationsDir string // database/migrations
	SeedersDir    string // database/seeders
	FactoriesDir  string // database/factories
	ModelsDir     string // app/models

	// Models are the GORM models migration:diff compares with the database.
	Models []any

	// Migrations and Seeders default to everything registered via Register in init().
	Migrations func() ([]migrate.Migration, error)
	Seeders    func() []seed.Seeder

	Out, Err    io.Writer
	In          io.Reader
	Interactive bool // true when In is a terminal; enables the production confirmation prompt
	Now         func() time.Time
}

func (a *App) defaults() {
	if a.Root == "" {
		a.Root = "."
	}
	set := func(p *string, v string) {
		if *p == "" {
			*p = v
		}
	}
	set(&a.MigrationsDir, "database/migrations")
	set(&a.SeedersDir, "database/seeders")
	set(&a.FactoriesDir, "database/factories")
	set(&a.ModelsDir, "app/models")
	set(&a.Default, "default")
	if a.Env == "" {
		for _, k := range []string{"MIGRO_ENV", "APP_ENV"} {
			if v := os.Getenv(k); v != "" {
				a.Env = v
				break
			}
		}
	}
	set(&a.Env, "development")
	if a.Migrations == nil {
		a.Migrations = migrate.Registered
	}
	if a.Seeders == nil {
		a.Seeders = seed.Registered
	}
	if a.Out == nil {
		a.Out = os.Stdout
	}
	if a.Err == nil {
		a.Err = os.Stderr
	}
	if a.In == nil {
		a.In = os.Stdin
	}
	if a.Now == nil {
		a.Now = time.Now
	}
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error { return usageError{fmt.Sprintf(format, args...)} }

// Run executes one command and returns a process exit code.
func (a *App) Run(ctx context.Context, args []string) int {
	a.defaults()
	if len(args) == 0 || args[0] == "help" || args[0] == "list" || args[0] == "--help" || args[0] == "-h" {
		a.printHelp()
		return ExitOK
	}
	cmd := findCommand(args[0])
	if cmd == nil {
		fmt.Fprintf(a.Err, "ERROR  Unknown command %q. Run `help` to list commands.\n", args[0])
		return ExitUsage
	}
	flags, err := parseArgs(cmd.flags, args[1:])
	if err == nil {
		err = cmd.run(&call{app: a, ctx: ctx, args: flags.args, flags: flags})
	}
	if err == nil {
		return ExitOK
	}
	fmt.Fprintf(a.Err, "ERROR  %s\n", err)
	if _, isUsage := err.(usageError); isUsage {
		fmt.Fprintf(a.Err, "Usage: %s\n", cmd.usage)
		return ExitUsage
	}
	return ExitError
}

// Main runs a command with a background context. Convenient for a `main` function:
//
//	os.Exit(app.Main(os.Args[1:]))
func (a *App) Main(args []string) int { return a.Run(context.Background(), args) }

func (a *App) printHelp() {
	cmds := allCommands()
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].name < cmds[j].name })
	fmt.Fprintln(a.Out, "Available commands:")
	for _, c := range cmds {
		fmt.Fprintf(a.Out, "  %-22s %s\n", c.name, c.desc)
	}
	fmt.Fprintln(a.Out, "\nCommon flags: --database=<name>  --force  --no-interaction (-n)")
}

// ---- argument parsing ----------------------------------------------------------------------

// flagSpec maps a flag name to whether it takes a value. Short aliases are written as "x=name" in shorts.
type flagSpec struct {
	values map[string]bool
	shorts map[string]string
}

type parsedArgs struct {
	args  []string
	flags map[string][]string
}

func parseArgs(spec flagSpec, in []string) (parsedArgs, error) {
	p := parsedArgs{flags: map[string][]string{}}
	for i := 0; i < len(in); i++ {
		tok := in[i]
		if tok == "--" {
			p.args = append(p.args, in[i+1:]...)
			break
		}
		if !strings.HasPrefix(tok, "-") || tok == "-" {
			p.args = append(p.args, tok)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(tok, "-"), "=")
		if !strings.HasPrefix(tok, "--") {
			full, ok := spec.shorts[name]
			if !ok {
				return p, usagef("unknown flag %q", tok)
			}
			name = full
		}
		takes, known := spec.values[name]
		if !known {
			return p, usagef("unknown flag %q", tok)
		}
		switch {
		case !takes && hasVal:
			p.flags[name] = append(p.flags[name], val)
		case !takes:
			p.flags[name] = append(p.flags[name], "true")
		case hasVal:
			p.flags[name] = append(p.flags[name], val)
		case i+1 < len(in):
			i++
			p.flags[name] = append(p.flags[name], in[i])
		default:
			return p, usagef("flag --%s needs a value", name)
		}
	}
	return p, nil
}

func (p parsedArgs) has(name string) bool { _, ok := p.flags[name]; return ok }

func (p parsedArgs) str(name string) string {
	v := p.flags[name]
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

func (p parsedArgs) isTrue(name string) bool {
	v := p.str(name)
	return v == "true" || v == "1" || v == "yes"
}

func (p parsedArgs) integer(name string) (int, bool, error) {
	if !p.has(name) {
		return 0, false, nil
	}
	n, err := strconv.Atoi(p.str(name))
	if err != nil || n < 0 {
		return 0, true, usagef("--%s must be a non-negative integer, got %q", name, p.str(name))
	}
	return n, true, nil
}

// call is the context handed to a command.
type call struct {
	app   *App
	ctx   context.Context
	args  []string
	flags parsedArgs
}

func (c *call) printf(format string, args ...any) { fmt.Fprintf(c.app.Out, format, args...) }

// openDB resolves --database (or the default) and opens it.
func (c *call) openDB() (*migro.DB, string, error) {
	name := c.flags.str("database")
	if name == "" {
		name = c.app.Default
	}
	open, ok := c.app.Connections[name]
	if !ok {
		known := make([]string, 0, len(c.app.Connections))
		for n := range c.app.Connections {
			known = append(known, n)
		}
		sort.Strings(known)
		return nil, name, migro.Newf(migro.InvalidConfig, "unknown connection %q (configured: %s)", name, strings.Join(known, ", "))
	}
	db, err := open(c.ctx)
	return db, name, err
}

// guard asks before touching a production database. --force skips it; without a terminal it refuses.
func (c *call) guard() error {
	a := c.app
	if a.Env != "production" || c.flags.isTrue("force") {
		return nil
	}
	if !a.Interactive || c.flags.isTrue("no-interaction") {
		return migro.Newf(migro.ProductionBlocked, "application is in production; re-run with --force to confirm")
	}
	fmt.Fprint(a.Out, "Application is in production! Do you really wish to run this command? (yes/no) [no]: ")
	line, _ := bufio.NewReader(a.In).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}
	return migro.Newf(migro.ProductionBlocked, "command cancelled")
}
