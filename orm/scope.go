package orm

import (
	"context"
	"reflect"

	"gorm.io/gorm"
)

// Query scopes. A local scope is any function from a query to a query; apply it with Query.Apply:
//
//	func Published(q *orm.Query[Post]) *orm.Query[Post] { return q.Where("published = ?", true) }
//	func Popular(min int) func(*orm.Query[Post]) *orm.Query[Post] { ... } // a scope with arguments
//
//	posts.Query().Apply(Published, Popular(100)).Get(ctx)
//
// A global scope is declared by the model and applied to every query on it, like Eloquent's
// addGlobalScope or a Lucid `static boot` scope.

// NamedScope is a global scope. The name lets one query switch it off with WithoutGlobalScope.
type NamedScope struct {
	Name string
	Fn   func(*gorm.DB) *gorm.DB
}

// GlobalScoped is implemented by models that always filter (or order) their queries:
//
//	func (Post) GlobalScopes() []orm.NamedScope {
//	    return []orm.NamedScope{{Name: "visible", Fn: func(g *gorm.DB) *gorm.DB { return g.Where("hidden = ?", false) }}}
//	}
//
// Global scopes apply to Query, Find, FindMany, All, counts, updates, deletes and to eager-loaded
// relations of that model.
type GlobalScoped interface{ GlobalScopes() []NamedScope }

type skipSet struct {
	all   bool
	names map[string]bool
}

func (s *skipSet) skips(name string) bool { return s != nil && (s.all || s.names[name]) }

func globalScopesOf(t reflect.Type) []NamedScope {
	gs, ok := reflect.New(t).Interface().(GlobalScoped)
	if !ok {
		return nil
	}
	return gs.GlobalScopes()
}

// scopeFunc folds the scopes into one GORM scope that reads skip when the query runs, so
// WithoutGlobalScope called after the query was built still takes effect.
func scopeFunc(scopes []NamedScope, skip *skipSet) func(*gorm.DB) *gorm.DB {
	return func(g *gorm.DB) *gorm.DB {
		for _, s := range scopes {
			if !skip.skips(s.Name) {
				g = s.Fn(g)
			}
		}
		return g
	}
}

// scoped is the base GORM handle for Find-style calls: global scopes applied.
func (m *Model[T]) scoped(ctx context.Context) *gorm.DB {
	g := m.db.Gorm.WithContext(ctx)
	if scopes := globalScopesOf(reflect.TypeFor[T]()); len(scopes) > 0 {
		g = g.Scopes(scopeFunc(scopes, nil))
	}
	return g
}

// Apply runs local scopes against the query, in order.
func (q *Query[T]) Apply(scopes ...func(*Query[T]) *Query[T]) *Query[T] {
	for _, s := range scopes {
		q = s(q)
	}
	return q
}

// WithoutGlobalScope switches the named global scopes off for this query.
func (q *Query[T]) WithoutGlobalScope(names ...string) *Query[T] {
	if q.skip.names == nil {
		q.skip.names = map[string]bool{}
	}
	for _, n := range names {
		q.skip.names[n] = true
	}
	return q
}

// WithoutGlobalScopes switches every global scope off for this query.
func (q *Query[T]) WithoutGlobalScopes() *Query[T] {
	q.skip.all = true
	return q
}
