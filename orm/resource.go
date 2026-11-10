package orm

import (
	"maps"
	"net/url"
	"reflect"
	"strconv"

	"github.com/LouisRiverstone/migro"
)

// Resource turns a model into the exact shape your API returns, the way Laravel's JsonResource
// does. Unlike ToMap, nothing is exposed unless you list it.
//
//	var UserResource = orm.NewResource(func(u *User, r *orm.R) {
//	    r.Set("id", u.ID)
//	    r.Set("name", u.Name)
//	    r.When(isAdmin, "email", u.Email)
//	    r.WhenLoaded("Posts", func() (any, error) { return PostResource.List(u.Posts.Items()) })
//	})
//
//	body, err := UserResource.Make(user)            // {"data": {...}}
//	body, err = UserResource.Collection(users)      // {"data": [...]}
//	body, err = UserResource.Paginated(page)        // {"data": [...], "meta": {...}}
type Resource[T any] struct {
	fields     func(*T, *R)
	wrapKey    string
	noWrap     bool
	additional map[string]any
}

// NewResource builds a resource from a function that describes one model.
func NewResource[T any](fields func(m *T, r *R)) *Resource[T] {
	return &Resource[T]{fields: fields, wrapKey: "data"}
}

// Wrap changes the key the payload sits under ("data" by default).
func (r *Resource[T]) Wrap(key string) *Resource[T] {
	c := *r
	c.wrapKey, c.noWrap = key, false
	return &c
}

// WithoutWrapping returns the bare item (Make) or array (Collection). Extra top-level keys
// (Additional, Paginated's meta) merge into a bare item; a bare array that has to carry them
// keeps its wrapper.
func (r *Resource[T]) WithoutWrapping() *Resource[T] {
	c := *r
	c.noWrap = true
	return &c
}

// Additional adds top-level keys next to the wrapped payload (Laravel's additional()).
func (r *Resource[T]) Additional(extra map[string]any) *Resource[T] {
	c := *r
	c.additional = maps.Clone(r.additional)
	if c.additional == nil {
		c.additional = map[string]any{}
	}
	maps.Copy(c.additional, extra)
	return &c
}

// R collects the fields of one resource item.
type R struct {
	out   map[string]any
	model any
	err   error
}

// Set always includes the key.
func (r *R) Set(key string, v any) { r.out[key] = v }

// When includes the key only if cond is true.
func (r *R) When(cond bool, key string, v any) {
	if cond {
		r.out[key] = v
	}
}

// Merge copies every entry of fields into the item.
func (r *R) Merge(fields map[string]any) { maps.Copy(r.out, fields) }

// MergeWhen merges fields only if cond is true.
func (r *R) MergeWhen(cond bool, fields map[string]any) {
	if cond {
		r.Merge(fields)
	}
}

// WhenLoaded sets key (the relation's snake_case name) to fn() only when the relation is loaded on
// the model, so a resource never triggers lazy queries. relation is the Go field name.
func (r *R) WhenLoaded(relation string, fn func() (any, error)) {
	if !r.loaded(relation) {
		return
	}
	v, err := fn()
	if err != nil {
		r.Fail(err)
		return
	}
	r.out[snake(relation)] = v
}

// Loaded reports whether the named relation is loaded on the model.
func (r *R) Loaded(relation string) bool { return r.loaded(relation) }

func (r *R) loaded(relation string) bool {
	rv := reflect.ValueOf(r.model)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return false
	}
	f := rv.Elem().FieldByName(relation)
	if !f.IsValid() || !f.CanAddr() {
		return false
	}
	rf, ok := f.Addr().Interface().(relField)
	return ok && rf.rm().loaded
}

// Fail records an error; Make / Collection return the first one.
func (r *R) Fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *Resource[T]) item(m *T) (map[string]any, error) {
	c := &R{out: map[string]any{}, model: m}
	r.fields(m, c)
	if c.err != nil {
		return nil, c.err
	}
	return c.out, nil
}

// Map describes one model without any wrapping: use it to nest a resource inside another.
func (r *Resource[T]) Map(m *T) (map[string]any, error) {
	if m == nil {
		return nil, migro.Newf(migro.QueryFailed, "resource: nil model")
	}
	return r.item(m)
}

// List describes many models without wrapping.
func (r *Resource[T]) List(items []T) ([]map[string]any, error) {
	out := make([]map[string]any, len(items))
	for i := range items {
		m, err := r.item(&items[i])
		if err != nil {
			return nil, err
		}
		out[i] = m
	}
	return out, nil
}

func (r *Resource[T]) envelope(payload any, extra map[string]any) any {
	if r.noWrap && len(extra) == 0 && len(r.additional) == 0 {
		return payload
	}
	out := map[string]any{}
	m, isItem := payload.(map[string]any)
	if r.noWrap && isItem {
		maps.Copy(out, m)
	} else {
		// a bare list has no room for siblings (meta, additional), so it keeps its wrapper then
		out[r.wrapKey] = payload
	}
	maps.Copy(out, extra)
	maps.Copy(out, r.additional)
	return out
}

// Make is the response for one model.
func (r *Resource[T]) Make(m *T) (any, error) {
	item, err := r.Map(m)
	if err != nil {
		return nil, err
	}
	return r.envelope(item, nil), nil
}

// Collection is the response for a list.
func (r *Resource[T]) Collection(items []T) (any, error) {
	list, err := r.List(items)
	if err != nil {
		return nil, err
	}
	return r.envelope(list, nil), nil
}

// Paginated is the response for a page: the items plus "meta" (current_page, last_page, per_page,
// total, from, to). Pass a base URL to also get "links" (first, last, prev, next).
func (r *Resource[T]) Paginated(page Page[T], baseURL ...string) (any, error) {
	list, err := r.List(page.Data)
	if err != nil {
		return nil, err
	}
	from, to := 0, 0
	if len(page.Data) > 0 {
		from = (page.CurrentPage-1)*page.PerPage + 1
		to = from + len(page.Data) - 1
	}
	extra := map[string]any{"meta": map[string]any{
		"current_page": page.CurrentPage, "last_page": page.LastPage, "per_page": page.PerPage,
		"total": page.Total, "from": from, "to": to,
	}}
	if len(baseURL) > 0 {
		extra["links"] = pageLinks(baseURL[0], page.CurrentPage, page.LastPage)
	}
	return r.envelope(list, extra), nil
}

func pageLinks(base string, current, last int) map[string]any {
	link := func(n int) any {
		u, err := url.Parse(base)
		if err != nil {
			return nil
		}
		q := u.Query()
		q.Set("page", strconv.Itoa(n))
		u.RawQuery = q.Encode()
		return u.String()
	}
	if last < 1 {
		last = 1
	}
	links := map[string]any{"first": link(1), "last": link(last), "prev": nil, "next": nil}
	if current > 1 {
		links["prev"] = link(current - 1)
	}
	if current < last {
		links["next"] = link(current + 1)
	}
	return links
}
