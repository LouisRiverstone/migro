package console

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/migrate/diff"
	"github.com/LouisRiverstone/migro/seed"
)

type command struct {
	name    string
	aliases []string
	desc    string
	usage   string
	flags   flagSpec
	run     func(*call) error
}

func spec(extra map[string]bool, shorts map[string]string) flagSpec {
	v := map[string]bool{"database": true, "force": false, "no-interaction": false}
	for k, takes := range extra {
		v[k] = takes
	}
	s := map[string]string{"n": "no-interaction"}
	for k, full := range shorts {
		s[k] = full
	}
	return flagSpec{values: v, shorts: s}
}

func allCommands() []command {
	return []command{
		{name: "migration:run", aliases: []string{"migrate"}, desc: "Run pending migrations",
			usage: "migration:run [--step] [--pretend] [--seed] [--database=name] [--force]",
			flags: spec(map[string]bool{"step": false, "pretend": false, "seed": false}, nil), run: runMigrations},
		{name: "migration:rollback", aliases: []string{"migrate:rollback"}, desc: "Roll back the last batch (or --step=N migrations, or --batch=N)",
			usage: "migration:rollback [--step=N | --batch=N] [--pretend] [--database=name] [--force]",
			flags: spec(map[string]bool{"step": true, "batch": true, "pretend": false}, nil), run: rollbackMigrations},
		{name: "migration:reset", aliases: []string{"migrate:reset"}, desc: "Roll back every migration",
			usage: "migration:reset [--pretend] [--database=name] [--force]",
			flags: spec(map[string]bool{"pretend": false}, nil), run: resetMigrations},
		{name: "migration:refresh", aliases: []string{"migrate:refresh"}, desc: "Roll back everything, then migrate again",
			usage: "migration:refresh [--seed] [--database=name] [--force]",
			flags: spec(map[string]bool{"seed": false}, nil), run: refreshMigrations},
		{name: "migration:fresh", aliases: []string{"migrate:fresh"}, desc: "Drop ALL tables, then migrate",
			usage: "migration:fresh [--seed] [--database=name] [--force]",
			flags: spec(map[string]bool{"seed": false}, nil), run: freshMigrations},
		{name: "migration:status", aliases: []string{"migrate:status"}, desc: "Show which migrations ran",
			usage: "migration:status [--database=name]", flags: spec(nil, nil), run: statusMigrations},
		{name: "migration:install", aliases: []string{"migrate:install"}, desc: "Create the migration tracking tables",
			usage: "migration:install [--database=name]", flags: spec(nil, nil), run: installMigrations},
		{name: "migration:unlock", desc: "Release a stale migration lock",
			usage: "migration:unlock [--database=name]", flags: spec(nil, nil), run: unlockMigrations},
		{name: "migration:diff", desc: "Generate a migration from the differences between your models and the database",
			usage: "migration:diff [name] [--allow-destructive] [--rename=table.old=new] [--path=dir] [--database=name]",
			flags: spec(map[string]bool{"allow-destructive": false, "rename": true, "path": true}, nil), run: diffMigration},
		{name: "schema:dump", desc: "Squash the migrations into one SQL file new databases load instead of replaying them",
			usage: "schema:dump [--path=dir] [--database=name]",
			flags: spec(map[string]bool{"path": true}, nil), run: dumpSchema},
		{name: "db:wipe", desc: "Drop ALL tables",
			usage: "db:wipe [--database=name] [--force]", flags: spec(nil, nil), run: wipeDatabase},
		{name: "db:show", desc: "Show the database and its tables",
			usage: "db:show [--counts] [--database=name]", flags: spec(map[string]bool{"counts": false}, nil), run: showDatabase},
		{name: "db:table", desc: "Show a table's columns and indexes",
			usage: "db:table <name> [--database=name]", flags: spec(nil, nil), run: showTable},
		{name: "model:show", desc: "Show a model's table, columns, relations and behaviours",
			usage: "model:show <Name> [--database=name]", flags: spec(nil, nil), run: showModel},
		{name: "model:prune", desc: "Delete the stale rows of every registered Prunable model",
			usage: "model:prune [--database=name] [--force]", flags: spec(nil, nil), run: pruneModels},
		{name: "db:seed", desc: "Run database seeders",
			usage: "db:seed [--class=Name] [--once] [--database=name] [--force]",
			flags: spec(map[string]bool{"class": true, "once": false}, nil), run: seedDatabase},
		{name: "make:migration", desc: "Create a migration file",
			usage: "make:migration <name> [--create=table | --table=table] [--path=dir]",
			flags: spec(map[string]bool{"create": true, "table": true, "path": true}, nil), run: makeMigration},
		{name: "make:seeder", desc: "Create a seeder",
			usage: "make:seeder <Name> [--path=dir]", flags: spec(map[string]bool{"path": true}, nil), run: makeSeeder},
		{name: "make:factory", desc: "Create a model factory",
			usage: "make:factory <Name> [--model=import/path.Type] [--path=dir]",
			flags: spec(map[string]bool{"model": true, "path": true}, nil), run: makeFactory},
		{name: "make:observer", desc: "Create a model observer",
			usage: "make:observer <Name> [--model=import/path.Type] [--path=dir]",
			flags: spec(map[string]bool{"model": true, "path": true}, nil), run: makeObserver},
		{name: "make:scope", desc: "Create a local query scope for a model in the models package",
			usage: "make:scope <Name> --model=Type [--path=dir]",
			flags: spec(map[string]bool{"model": true, "path": true}, nil), run: makeScope},
		{name: "make:resource", desc: "Create an API resource for a model in the models package",
			usage: "make:resource <Name> [--model=Type] [--path=dir]",
			flags: spec(map[string]bool{"model": true, "path": true}, nil), run: makeResource},
		{name: "make:cast", desc: "Create a custom attribute cast",
			usage: "make:cast <Name> [--path=dir]", flags: spec(map[string]bool{"path": true}, nil), run: makeCast},
		{name: "make:model", desc: "Create a model (-m migration, -f factory, -s seeder, -a all)",
			usage: "make:model <Name> [-m] [-f] [-s] [-a] [--uuid|--ulid] [--path=dir]",
			flags: spec(map[string]bool{"migration": false, "factory": false, "seeder": false, "all": false, "uuid": false, "ulid": false, "path": true},
				map[string]string{"m": "migration", "f": "factory", "s": "seeder", "a": "all"}), run: makeModel},
	}
}

func findCommand(name string) *command {
	for _, c := range allCommands() {
		if c.name == name || slices.Contains(c.aliases, name) {
			return &c
		}
	}
	return nil
}

// ---- migration commands --------------------------------------------------------------------

func (c *call) migrator(db *migro.DB, conn string, quiet bool) (*migrate.Migrator, error) {
	ms, err := c.app.Migrations()
	if err != nil {
		return nil, err
	}
	opts := []migrate.Option{migrate.WithConnection(conn, c.app.Default)}
	dump, err := c.loadDump(db)
	if err != nil {
		return nil, err
	}
	if dump != nil {
		opts = append(opts, migrate.WithDump(*dump))
	}
	if !quiet {
		opts = append(opts, migrate.WithObserver(c.observe))
	}
	m := migrate.New(db, opts...)
	if err := m.Add(ms...); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *call) observe(e migrate.Event) {
	switch e.Kind {
	case migrate.Migrating:
		c.printf("Migrating:  %s\n", e.Name)
	case migrate.Migrated:
		c.printf("Migrated:   %s (%s)\n", e.Name, round(e.Duration))
	case migrate.Reverting:
		c.printf("Rolling back: %s\n", e.Name)
	case migrate.Reverted:
		c.printf("Rolled back:  %s (%s)\n", e.Name, round(e.Duration))
	}
}

func round(d time.Duration) string {
	if d < time.Millisecond {
		return d.Round(time.Microsecond).String()
	}
	return d.Round(100 * time.Microsecond).String()
}

func (c *call) printPretend(res migrate.Result) {
	for _, name := range res.Names {
		c.printf("-- %s\n", name)
		for _, stmt := range res.SQL[name] {
			c.printf("%s;\n", strings.TrimRight(strings.TrimSpace(stmt), ";"))
		}
		c.printf("\n")
	}
}

// withMigrator opens the database, guards production (unless read-only) and hands over a Migrator.
func (c *call) withMigrator(readOnly bool, fn func(m *migrate.Migrator, db *migro.DB) error) error {
	if !readOnly {
		if err := c.guard(); err != nil {
			return err
		}
	}
	db, conn, err := c.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	m, err := c.migrator(db, conn, readOnly)
	if err != nil {
		return err
	}
	return fn(m, db)
}

func runMigrations(c *call) error {
	pretend := c.flags.isTrue("pretend")
	return c.withMigrator(pretend, func(m *migrate.Migrator, db *migro.DB) error {
		res, err := m.Run(c.ctx, migrate.RunOptions{Step: c.flags.isTrue("step"), Pretend: pretend})
		if err != nil {
			return err
		}
		if len(res.Names) == 0 {
			c.printf("Nothing to migrate.\n")
			return nil
		}
		if pretend {
			c.printPretend(res)
			return nil
		}
		return c.maybeSeed(db)
	})
}

func rollbackMigrations(c *call) error {
	steps, hasStep, err := c.flags.integer("step")
	if err != nil {
		return err
	}
	batch, hasBatch, err := c.flags.integer("batch")
	if err != nil {
		return err
	}
	if hasStep && hasBatch {
		return usagef("use either --step or --batch, not both")
	}
	if hasStep && steps == 0 {
		return usagef("--step must be at least 1")
	}
	opts := migrate.RevertOptions{Steps: steps, Pretend: c.flags.isTrue("pretend")}
	if hasBatch {
		opts.ToBatch = &batch
	}
	return c.revert(opts)
}

func resetMigrations(c *call) error {
	return c.revert(migrate.RevertOptions{All: true, Pretend: c.flags.isTrue("pretend")})
}

func (c *call) revert(opts migrate.RevertOptions) error {
	return c.withMigrator(opts.Pretend, func(m *migrate.Migrator, _ *migro.DB) error {
		res, err := m.Revert(c.ctx, opts)
		if err != nil {
			return err
		}
		if len(res.Names) == 0 {
			c.printf("Nothing to roll back.\n")
			return nil
		}
		if opts.Pretend {
			c.printPretend(res)
		}
		return nil
	})
}

func refreshMigrations(c *call) error {
	return c.withMigrator(false, func(m *migrate.Migrator, db *migro.DB) error {
		if _, err := m.Refresh(c.ctx); err != nil {
			return err
		}
		return c.maybeSeed(db)
	})
}

func freshMigrations(c *call) error {
	return c.withMigrator(false, func(m *migrate.Migrator, db *migro.DB) error {
		if _, err := m.Fresh(c.ctx); err != nil {
			return err
		}
		return c.maybeSeed(db)
	})
}

func (c *call) maybeSeed(db *migro.DB) error {
	if !c.flags.isTrue("seed") {
		return nil
	}
	return c.runSeeders(db, "", false)
}

func statusMigrations(c *call) error {
	return c.withMigrator(true, func(m *migrate.Migrator, _ *migro.DB) error {
		st, err := m.Status(c.ctx)
		if err != nil {
			return err
		}
		if len(st) == 0 {
			c.printf("No migrations found.\n")
			return nil
		}
		c.printf("%-5s %-60s %s\n", "Ran?", "Migration", "Batch")
		for _, s := range st {
			if s.Applied {
				c.printf("%-5s %-60s %d\n", "Yes", s.Name, s.Batch)
				continue
			}
			c.printf("%-5s %-60s\n", "No", s.Name)
		}
		return nil
	})
}

func installMigrations(c *call) error {
	return c.withMigrator(true, func(m *migrate.Migrator, _ *migro.DB) error {
		if err := m.Install(c.ctx); err != nil {
			return err
		}
		c.printf("Migration tables are ready.\n")
		return nil
	})
}

func unlockMigrations(c *call) error {
	return c.withMigrator(true, func(m *migrate.Migrator, _ *migro.DB) error {
		if err := m.Unlock(c.ctx); err != nil {
			return err
		}
		c.printf("Migration lock released.\n")
		return nil
	})
}

func diffMigration(c *call) error {
	if len(c.app.Models) == 0 {
		return migro.Newf(migro.InvalidConfig, "no models to compare: set App.Models, or run through the migro launcher with a models package")
	}
	name := "sync_models"
	if len(c.args) > 0 {
		name = c.args[0]
	}
	renames := map[string]string{}
	for _, r := range c.flags.flags["rename"] {
		from, to, ok := strings.Cut(r, "=")
		if !ok || !strings.Contains(from, ".") || to == "" {
			return usagef("--rename must look like table.old_column=new_column, got %q", r)
		}
		renames[from] = to
	}
	db, _, err := c.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	plan, err := diff.Generate(c.ctx, db, diff.Options{AllowDestructive: c.flags.isTrue("allow-destructive"), Renames: renames}, c.app.Models...)
	if err != nil {
		return err
	}
	for _, w := range plan.Warnings {
		c.printf("WARNING  %s\n", w)
	}
	for _, op := range plan.Blocked {
		what := op.From
		if op.Index != nil {
			what = op.Index.Name
		}
		c.printf("SKIPPED  %s %s.%s (destructive; pass --allow-destructive)\n", op.Kind, op.Table, what)
	}
	dir := c.dir("path", c.app.MigrationsDir)
	path, err := plan.WriteGoFile(dir, detectPackage(dir, "migrations"), name, c.app.Now())
	if err != nil {
		return err
	}
	if path == "" {
		c.printf("Nothing to migrate: models and database already match.\n")
		return nil
	}
	c.printf("Created Migration: %s\nReview it before running migration:run.\n", relTo(c.app.Root, path))
	return nil
}

// ---- seeding ------------------------------------------------------------------------------

func seedDatabase(c *call) error {
	if err := c.guard(); err != nil {
		return err
	}
	db, _, err := c.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	return c.runSeeders(db, c.flags.str("class"), c.flags.isTrue("once"))
}

func (c *call) runSeeders(db *migro.DB, class string, once bool) error {
	opts := []seed.Option{seed.WithEnvironment(c.app.Env)}
	if once {
		opts = append(opts, seed.Once())
	}
	r := seed.NewRunner(db, opts...)
	if err := r.Add(c.app.Seeders()...); err != nil {
		return err
	}
	var (
		res seed.Result
		err error
	)
	if class != "" {
		res, err = r.RunOnly(c.ctx, class)
	} else {
		res, err = r.Seed(c.ctx)
	}
	for _, n := range res.Ran {
		c.printf("Seeded:   %s\n", n)
	}
	for _, n := range res.Skipped {
		c.printf("Skipped:  %s\n", n)
	}
	if err == nil && len(res.Ran)+len(res.Skipped) == 0 {
		c.printf("No seeders found.\n")
	}
	return err
}

func (c *call) dir(flag, fallback string) string {
	if v := c.flags.str(flag); v != "" {
		if filepath.IsAbs(v) {
			return v
		}
		return filepath.Join(c.app.Root, v)
	}
	return filepath.Join(c.app.Root, fallback)
}

func relTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}
