package seed

import (
	"context"
	"io/fs"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/events"
	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

// Seeder is one unit of seed data. Like Lucid's, it may be limited to environments:
// with no Environments it runs everywhere except "production"; to seed production,
// list "production" explicitly.
type Seeder struct {
	Name         string
	Environments []string
	Run          func(ctx context.Context, db *migro.DB) error
}

// Runner executes seeders in name order, each in its own transaction.
type Runner struct {
	db      *migro.DB
	env     string
	once    bool
	silent  bool
	table   string
	seeders []Seeder
}

type Option func(*Runner)

// WithEnvironment sets the environment (default: MIGRO_ENV, then APP_ENV, then "development").
func WithEnvironment(env string) Option { return func(r *Runner) { r.env = env } }

// Once records executed seeders in a table so re-running skips them. Without it seeders run every
// time, like Lucid — write them idempotently (e.g. with FirstOrCreate).
func Once() Option { return func(r *Runner) { r.once = true } }

// WithoutEvents runs seeders with model events muted (Laravel's WithoutModelEvents trait), so
// observers and event handlers do not fire for seeded rows.
func WithoutEvents() Option { return func(r *Runner) { r.silent = true } }

// RootSeeder is the name of the seeder `db:seed` starts from when one is registered.
const RootSeeder = "DatabaseSeeder"

func NewRunner(db *migro.DB, opts ...Option) *Runner {
	r := &Runner{db: db, table: "migro_seeds", env: firstEnv("MIGRO_ENV", "APP_ENV")}
	for _, o := range opts {
		o(r)
	}
	return r
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "development"
}

// Add registers seeders; names must be unique and Run set.
func (r *Runner) Add(ss ...Seeder) error {
	all := append(slices.Clone(r.seeders), ss...)
	seen := map[string]bool{}
	for _, s := range all {
		if s.Name == "" || s.Run == nil {
			return migro.Newf(migro.SeedFailed, "seeder %q needs a name and a Run func", s.Name)
		}
		if seen[s.Name] {
			return migro.Newf(migro.SeedFailed, "duplicate seeder name %q", s.Name)
		}
		seen[s.Name] = true
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	r.seeders = all
	return nil
}

// Result lists what a run did.
type Result struct {
	Ran     []string
	Skipped []string
}

func (s Seeder) allowed(env string) bool {
	if len(s.Environments) == 0 {
		return env != "production"
	}
	return slices.Contains(s.Environments, env)
}

type seedRow struct {
	ID    uint `gorm:"primaryKey"`
	Name  string
	RanAt time.Time
}

// Run executes every eligible seeder, in name order.
func (r *Runner) Run(ctx context.Context) (Result, error) { return r.run(ctx, nil) }

// Seed is `php artisan db:seed`: it runs the DatabaseSeeder when one is registered (which calls the
// others in the order it needs, see Call), and otherwise every seeder in name order (Adonis's db:seed).
func (r *Runner) Seed(ctx context.Context) (Result, error) {
	if slices.ContainsFunc(r.seeders, func(s Seeder) bool { return s.Name == RootSeeder }) {
		return r.RunOnly(ctx, RootSeeder)
	}
	return r.Run(ctx)
}

// RunOnly executes only the named seeders (still subject to environment rules).
func (r *Runner) RunOnly(ctx context.Context, names ...string) (Result, error) {
	for _, n := range names {
		if !slices.ContainsFunc(r.seeders, func(s Seeder) bool { return s.Name == n }) {
			return Result{}, migro.Newf(migro.SeedFailed, "unknown seeder %q", n)
		}
	}
	return r.run(ctx, names)
}

func (r *Runner) run(ctx context.Context, only []string) (Result, error) {
	var res Result
	if r.silent {
		ctx = events.WithoutEvents(ctx)
	}
	done := map[string]bool{}
	if r.once {
		if err := schema.New(r.db).CreateTableIfNotExists(ctx, r.table, func(t *schema.Table) {
			t.Increments("id")
			t.String("name", 255).NotNull().Unique()
			t.Timestamp("ran_at")
		}); err != nil {
			return res, err
		}
		var rows []seedRow
		if err := r.db.Gorm.WithContext(ctx).Table(r.table).Find(&rows).Error; err != nil {
			return res, migro.Wrap(migro.SeedFailed, err, "read %s", r.table)
		}
		for _, row := range rows {
			done[row.Name] = true
		}
	}
	for _, s := range r.seeders {
		if only != nil && !slices.Contains(only, s.Name) {
			continue
		}
		if !s.allowed(r.env) || done[s.Name] {
			res.Skipped = append(res.Skipped, s.Name)
			continue
		}
		s := s
		err := r.db.Transaction(ctx, func(tx *migro.DB) error {
			if err := s.Run(ctx, tx); err != nil {
				return err
			}
			if !r.once {
				return nil
			}
			return tx.Gorm.WithContext(ctx).Table(r.table).Create(&seedRow{Name: s.Name, RanAt: time.Now()}).Error
		})
		if err != nil {
			return res, migro.Wrap(migro.SeedFailed, err, "seeder %s", s.Name)
		}
		res.Ran = append(res.Ran, s.Name)
	}
	return res, nil
}

// FromFS loads every *.sql file in dir as a seeder named after the file. Statements are split like
// migration files.
func FromFS(fsys fs.FS, dir string) ([]Seeder, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, migro.Wrap(migro.SeedFailed, err, "read seeds dir %q", dir)
	}
	var out []Seeder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, migro.Wrap(migro.SeedFailed, err, "read %s", e.Name())
		}
		stmts := migrate.SplitStatements(string(b))
		out = append(out, Seeder{
			Name: strings.TrimSuffix(e.Name(), ".sql"),
			Run: func(ctx context.Context, db *migro.DB) error {
				for _, st := range stmts {
					if err := db.Gorm.WithContext(ctx).Exec(st).Error; err != nil {
						return err
					}
				}
				return nil
			},
		})
	}
	return out, nil
}
