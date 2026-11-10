package orm_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/LouisRiverstone/migro/orm"
)

type thing struct {
	Name  string
	Admin bool
}

var thingResource = orm.NewResource(func(m *thing, r *orm.R) {
	r.Set("name", m.Name)
	r.When(m.Admin, "admin", true)
	r.Merge(map[string]any{"a": 1})
	r.MergeWhen(m.Admin, map[string]any{"b": 2})
	r.WhenLoaded("Missing", func() (any, error) { return "never", nil }) // not a relation: skipped
})

func TestResourceBuilderMethods(t *testing.T) {
	got, err := thingResource.Map(&thing{Name: "x"})
	if err != nil || !reflect.DeepEqual(got, map[string]any{"name": "x", "a": 1}) {
		t.Fatalf("plain: %v %v", got, err)
	}
	got, _ = thingResource.Map(&thing{Name: "y", Admin: true})
	if !reflect.DeepEqual(got, map[string]any{"name": "y", "admin": true, "a": 1, "b": 2}) {
		t.Fatalf("admin: %v", got)
	}
	if _, err := thingResource.Map(nil); err == nil {
		t.Error("a nil model is an error, not a panic")
	}
}

func TestResourceFailSurfacesTheFirstError(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	res := orm.NewResource(func(m *thing, r *orm.R) {
		r.Fail(first)
		r.Fail(second)
		r.Set("name", m.Name)
	})
	if _, err := res.Make(&thing{}); !errors.Is(err, first) {
		t.Errorf("Make: %v", err)
	}
	if _, err := res.Collection([]thing{{}}); !errors.Is(err, first) {
		t.Errorf("Collection: %v", err)
	}
	if _, err := res.List([]thing{{}}); !errors.Is(err, first) {
		t.Errorf("List: %v", err)
	}
}

func TestResourceEnvelopes(t *testing.T) {
	one := &thing{Name: "n"}
	body, _ := thingResource.Make(one)
	if _, ok := body.(map[string]any)["data"]; !ok {
		t.Errorf("default wrap: %v", body)
	}
	body, _ = thingResource.Wrap("thing").Make(one)
	if _, ok := body.(map[string]any)["thing"]; !ok {
		t.Errorf("custom wrap: %v", body)
	}
	bare, _ := thingResource.WithoutWrapping().Make(one)
	if bare.(map[string]any)["name"] != "n" {
		t.Errorf("bare item: %v", bare)
	}
	if list, _ := thingResource.WithoutWrapping().Collection([]thing{*one}); len(list.([]map[string]any)) != 1 {
		t.Errorf("bare list: %v", list)
	}
	// Additional merges into a bare item, and later calls add to earlier ones
	merged, _ := thingResource.WithoutWrapping().Additional(map[string]any{"v": 1}).Additional(map[string]any{"w": 2}).Make(one)
	m := merged.(map[string]any)
	if m["name"] != "n" || m["v"] != 1 || m["w"] != 2 {
		t.Errorf("additional: %v", m)
	}
	// builders never mutate the receiver
	if _, ok := mustAny(thingResource.Make(one)).(map[string]any)["data"]; !ok {
		t.Error("receiver was changed by a builder")
	}
}

func TestBareListKeepsItsWrapperWhenItMustCarryExtras(t *testing.T) {
	list := []thing{{Name: "a"}}
	body, _ := thingResource.WithoutWrapping().Additional(map[string]any{"v": 1}).Collection(list)
	m, ok := body.(map[string]any)
	if !ok || m["v"] != 1 || len(m["data"].([]map[string]any)) != 1 {
		t.Fatalf("items lost: %v", body)
	}
	page := orm.Page[thing]{Data: list, Total: 1, PerPage: 10, CurrentPage: 1, LastPage: 1}
	pb, _ := thingResource.WithoutWrapping().Paginated(page)
	if pm := pb.(map[string]any); len(pm["data"].([]map[string]any)) != 1 || pm["meta"] == nil {
		t.Fatalf("paginated: %v", pb)
	}
}

func mustAny(v any, err error) any {
	if err != nil {
		panic(err)
	}
	return v
}
