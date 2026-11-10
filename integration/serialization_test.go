package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	. "github.com/LouisRiverstone/migro/integration/models"
	"github.com/LouisRiverstone/migro/orm"
)

func TestSerializationOptionsAdjustOneCall(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	ana := must(orm.New[User](db).Find(ctx, 1))
	ok(t, ana.SetAttribute("password", "pw"))
	ok(t, ana.Save(ctx))
	ana = must(orm.New[User](db).Find(ctx, 1))

	base, err := orm.ToMap(ana)
	ok(t, err)
	if _, has := base["password"]; has {
		t.Fatal("password must be hidden by default")
	}

	shown, err := orm.ToMapWith(ana, orm.MakeVisible("password"), orm.MakeHidden("email"))
	ok(t, err)
	if _, has := shown["password"]; !has {
		t.Error("MakeVisible did not reveal the column")
	}
	if _, has := shown["email"]; has {
		t.Error("MakeHidden did not hide the column")
	}
	// options are per call: the next plain call is back to the model's rules
	again, _ := orm.ToMap(ana)
	if _, has := again["password"]; has {
		t.Error("an option leaked into the next call")
	}
	// hiding wins over showing
	both, _ := orm.ToMapWith(ana, orm.MakeVisible("password"), orm.MakeHidden("password"))
	if _, has := both["password"]; has {
		t.Error("MakeHidden must win over MakeVisible")
	}

	// dates: formatted at the root and inside loaded relations
	users, err := orm.New[User](db).Query().Where("id = ?", 1).With("Posts").Get(ctx)
	ok(t, err)
	m, err := orm.ToMapWith(&users[0], orm.DateFormat("2006-01-02"))
	ok(t, err)
	today := users[0].CreatedAt.Format("2006-01-02")
	if m["created_at"] != today {
		t.Errorf("created_at = %v, want %s", m["created_at"], today)
	}
	posts := m["posts"].([]any)
	if posts[0].(map[string]any)["created_at"] != posts[0].(map[string]any)["created_at"].(string) || len(posts[0].(map[string]any)["created_at"].(string)) != 10 {
		t.Errorf("nested date not formatted: %v", posts[0])
	}

	raw, err := orm.JSONWith(ana, orm.MakeVisible("password"))
	ok(t, err)
	if !strings.Contains(string(raw), `"password"`) {
		t.Errorf("JSONWith: %s", raw)
	}
}

func TestSerializingCollectionsAndLoadedRelations(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	users, err := orm.New[User](db).Query().With("Posts.Comments").WithCount("Roles").OrderBy("id", "asc").Get(ctx)
	ok(t, err)

	maps, err := users.ToMaps(orm.MakeHidden("country_id"))
	ok(t, err)
	if len(maps) != 3 {
		t.Fatalf("%d maps", len(maps))
	}
	if _, has := maps[0]["country_id"]; has {
		t.Error("options must reach every model")
	}
	if maps[0]["roles_count"] != int64(2) && maps[0]["roles_count"] != 2 {
		t.Errorf("extras are part of the output: %v", maps[0]["roles_count"])
	}
	posts := maps[0]["posts"].([]any)
	if len(posts) != 2 || posts[0].(map[string]any)["comments"] == nil {
		t.Errorf("nested relations: %v", maps[0]["posts"])
	}

	raw, err := users.JSON()
	ok(t, err)
	var decoded []map[string]any
	ok(t, json.Unmarshal(raw, &decoded))
	if len(decoded) != 3 || decoded[0]["email"] != "ana@example.com" {
		t.Errorf("Collection.JSON: %s", raw)
	}
}
