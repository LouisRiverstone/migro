package console

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (c *call) newObserver(base, modelFlag string) (string, error) {
	importPath, qualified, err := c.modelRef(base, modelFlag)
	if err != nil {
		return "", err
	}
	dir := c.dir("path", c.app.ObserversDir)
	typ := base + "Observer"
	path := filepath.Join(dir, snake(typ)+".go")
	src := fmt.Sprintf(`package %[1]s

import (
	"context"

	%[2]q
)

// %[3]s reacts to %[4]s lifecycle events. Register it once at startup:
//
//	off, err := events.Observe[%[4]s](bus, &%[3]s{})
//
// A hook that returns an error from Creating, Updating, Saving or Deleting aborts the write. Delete the
// hooks you do not need; the compiler checks the ones you keep against %[4]s.
type %[3]s struct{}

func (o *%[3]s) Creating(ctx context.Context, m *%[4]s) error { return nil }
func (o *%[3]s) Created(ctx context.Context, m *%[4]s) error  { return nil }
func (o *%[3]s) Updating(ctx context.Context, m *%[4]s) error { return nil }
func (o *%[3]s) Updated(ctx context.Context, m *%[4]s) error  { return nil }
func (o *%[3]s) Deleting(ctx context.Context, m *%[4]s) error { return nil }
func (o *%[3]s) Deleted(ctx context.Context, m *%[4]s) error  { return nil }
`, detectPackage(dir, "observers"), importPath, typ, qualified)
	return path, writeNew(path, src)
}

func makeObserver(c *call) error {
	if len(c.args) != 1 {
		return usagef("exactly one observer name is required")
	}
	base, err := baseName(c.args[0], "Observer")
	if err != nil {
		return err
	}
	path, err := c.newObserver(base, c.flags.str("model"))
	if err != nil {
		return err
	}
	c.printf("Created Observer: %s\n", relTo(c.app.Root, path))
	return nil
}

// inModels writes a file into the models package, where unexported helpers and the model type need no import.
func (c *call) inModels(file, body string) (string, error) {
	dir := c.dir("path", c.app.ModelsDir)
	path := filepath.Join(dir, file)
	return path, writeNew(path, fmt.Sprintf("package %s\n\n%s", detectPackage(dir, "models"), body))
}

func makeScope(c *call) error {
	if len(c.args) != 1 {
		return usagef("exactly one scope name is required")
	}
	model := c.flags.str("model")
	if model == "" {
		return usagef("--model=Type is required: a scope is a function over one model's query")
	}
	name, err := baseName(c.args[0], "Scope")
	if err != nil {
		return err
	}
	path, err := c.inModels(snake(name)+"_scope.go", fmt.Sprintf(`import "github.com/LouisRiverstone/migro/orm"

// %[1]s is a local scope of %[2]s. Apply it with:
//
//	posts.Query().Apply(%[1]s).Get(ctx)
func %[1]s(q *orm.Query[%[2]s]) *orm.Query[%[2]s] {
	return q // .Where("column = ?", value)
}
`, name, model))
	if err != nil {
		return err
	}
	c.printf("Created Scope: %s\n", relTo(c.app.Root, path))
	return nil
}

func makeResource(c *call) error {
	if len(c.args) != 1 {
		return usagef("exactly one resource name is required")
	}
	base, err := baseName(c.args[0], "Resource")
	if err != nil {
		return err
	}
	model := c.flags.str("model")
	if model == "" {
		model = base
	}
	path, err := c.inModels(snake(base)+"_resource.go", fmt.Sprintf(`import "github.com/LouisRiverstone/migro/orm"

// %[1]sResource is the API shape of a %[2]s: list only what clients may see.
var %[1]sResource = orm.NewResource(func(m *%[2]s, r *orm.R) {
	r.Set("id", m.ID)
	// r.Set("name", m.Name)
	// r.When(m.Role == "admin", "role", m.Role)
	// r.WhenLoaded("Posts", func() (any, error) { return PostResource.List(m.Posts.Items()) })
})
`, base, model))
	if err != nil {
		return err
	}
	c.printf("Created Resource: %s\n", relTo(c.app.Root, path))
	return nil
}

func makeCast(c *call) error {
	if len(c.args) != 1 {
		return usagef("exactly one cast name is required")
	}
	base, err := baseName(c.args[0], "Cast")
	if err != nil {
		return err
	}
	typ := strings.ToLower(base[:1]) + base[1:] + "Cast"
	path, err := c.inModels(snake(base)+"_cast.go", fmt.Sprintf(`import "github.com/LouisRiverstone/migro/orm"

// %[1]s converts a column on the way out of the model (Get: ToMap, JSON, GetAttribute) and on the way in
// (Set: Fill, Update, Make, SetAttribute). Plain assignment and Save never run it. Use it from a model:
//
//	func (User) Casts() map[string]orm.Cast { return map[string]orm.Cast{"column": %[1]s{}} }
type %[1]s struct{}

func (%[1]s) Get(raw any) (any, error) { return raw, nil }
func (%[1]s) Set(v any) (any, error)   { return v, nil }

var _ orm.Cast = %[1]s{}
`, typ))
	if err != nil {
		return err
	}
	c.printf("Created Cast: %s\n", relTo(c.app.Root, path))
	return nil
}
