package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LouisRiverstone/migro"
	. "github.com/LouisRiverstone/migro/integration/models"
	"github.com/LouisRiverstone/migro/orm"
)

func rawColumn(t *testing.T, db *migro.DB, table, col string, id uint) string {
	t.Helper()
	var v *string
	if err := db.Gorm.Table(table).Select(col).Where("id = ?", id).Scan(&v).Error; err != nil {
		t.Fatalf("read %s.%s: %v", table, col, err)
	}
	if v == nil {
		return ""
	}
	return *v
}

func TestHashedCastStoresOnlyTheHash(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	users := orm.New[User](db)

	u, err := users.CreateFrom(ctx, map[string]any{"country_id": 1, "email": "x@example.com", "name": "X", "password": "s3cret"})
	ok(t, err)

	stored := rawColumn(t, db, "users", "password", u.ID)
	if stored == "s3cret" || !strings.HasPrefix(stored, "$2") {
		t.Fatalf("password stored as %q", stored)
	}
	if !orm.CompareHash(stored, "s3cret") || orm.CompareHash(stored, "wrong") {
		t.Fatal("CompareHash disagrees with the stored hash")
	}

	// filling the stored hash again must not hash it twice
	ok(t, u.Update(ctx, map[string]any{"password": stored}))
	if again := rawColumn(t, db, "users", "password", u.ID); again != stored {
		t.Error("an already hashed value was hashed again")
	}
	// a new plain password replaces it
	ok(t, u.Update(ctx, map[string]any{"password": "n3w"}))
	if now := rawColumn(t, db, "users", "password", u.ID); now == stored || !orm.CompareHash(now, "n3w") {
		t.Error("changing the password did not rehash")
	}

	// the hash never leaves through serialization
	m, err := orm.ToMap(u)
	ok(t, err)
	if _, leaked := m["password"]; leaked {
		t.Error("password is hidden but ToMap returned it")
	}
}

func TestEncryptedCastIsCiphertextAtRestAndPlainOnRead(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	ana := must(orm.New[User](db).Find(ctx, 1))

	ok(t, ana.SetAttribute("api_token", "tok-123"))
	ok(t, ana.Save(ctx))

	stored := rawColumn(t, db, "users", "api_token", ana.ID)
	if stored == "" || strings.Contains(stored, "tok-123") {
		t.Fatalf("token stored as %q", stored)
	}
	// two writes of the same plaintext differ (random nonce)
	ok(t, ana.SetAttribute("api_token", "tok-123"))
	ok(t, ana.Save(ctx))
	if rawColumn(t, db, "users", "api_token", ana.ID) == stored {
		t.Error("ciphertext repeated: the nonce is not random")
	}

	fresh := must(orm.New[User](db).Find(ctx, 1))
	plain, err := fresh.GetAttribute("api_token")
	ok(t, err)
	if plain != "tok-123" {
		t.Errorf("decrypted to %v", plain)
	}

	// the wrong key is an error, never garbage
	_, err = orm.CastEncrypted([]byte("ffffffffffffffffffffffffffffffff")).Get(fresh.APIToken)
	if err == nil {
		t.Error("decrypting with another key must fail")
	}
	// a key of the wrong size fails with CAST_FAILED through the model API
	bad := orm.CastEncrypted([]byte("short"))
	if _, err := bad.Set("x"); err == nil {
		t.Error("a 5-byte key must be rejected")
	}
}

func TestEnumCastRejectsBadValuesBeforeTheInsert(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	users := orm.New[User](db)
	before := count(t, db, "users")

	_, err := users.CreateFrom(ctx, map[string]any{"country_id": 1, "email": "e@example.com", "name": "E", "role": "root"})
	if migro.CodeOf(err) != migro.CastFailed {
		t.Fatalf("invalid role: %v", err)
	}
	if count(t, db, "users") != before {
		t.Error("a rejected value must not reach the database")
	}

	u, err := users.CreateFrom(ctx, map[string]any{"country_id": 1, "email": "e@example.com", "name": "E", "role": "editor"})
	ok(t, err)
	if rawColumn(t, db, "users", "role", u.ID) != "editor" {
		t.Error("valid role not stored")
	}
	// no role given: the database default applies
	d := must(users.CreateFrom(ctx, map[string]any{"country_id": 1, "email": "d@example.com", "name": "D"}))
	if got := rawColumn(t, db, "users", "role", d.ID); got != "member" {
		t.Errorf("default role %q", got)
	}
}

func TestDateCastParsesInAndFormatsOut(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	users := orm.New[User](db)

	u, err := users.CreateFrom(ctx, map[string]any{"country_id": 1, "email": "b@example.com", "name": "B", "birthday": "1990-05-17"})
	ok(t, err)
	if u.Birthday == nil || u.Birthday.Year() != 1990 || u.Birthday.Month() != time.May || u.Birthday.Day() != 17 {
		t.Fatalf("birthday = %v", u.Birthday)
	}
	fresh := must(users.Find(ctx, u.ID))
	v, err := fresh.GetAttribute("birthday")
	ok(t, err)
	if v != "1990-05-17" {
		t.Errorf("accessor gave %v", v)
	}
	m, err := orm.ToMap(fresh)
	ok(t, err)
	if m["birthday"] != "1990-05-17" {
		t.Errorf("ToMap birthday %v", m["birthday"])
	}

	_, err = users.CreateFrom(ctx, map[string]any{"country_id": 1, "email": "c@example.com", "name": "C", "birthday": "17/05/1990"})
	if migro.CodeOf(err) != migro.CastFailed {
		t.Errorf("bad date: %v", err)
	}
}

func TestCustomMutatorAndSetAttribute(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	users := orm.New[User](db)

	u, err := users.CreateFrom(ctx, map[string]any{"country_id": 1, "email": "  MiXed@Example.COM ", "name": "M"})
	ok(t, err)
	if u.Email != "mixed@example.com" {
		t.Errorf("mutator left %q", u.Email)
	}

	// SetAttribute bypasses Fillable but still runs the cast
	ana := must(users.Find(ctx, 1))
	ok(t, ana.SetAttribute("email", "ANA2@Example.com"))
	if ana.Email != "ana2@example.com" || !ana.IsDirty("email") {
		t.Errorf("SetAttribute: %q dirty=%v", ana.Email, ana.IsDirty("email"))
	}
	if err := ana.SetAttribute("nope", 1); err == nil {
		t.Error("an unknown column must fail")
	}
	if _, err := ana.GetAttribute("nope"); err == nil {
		t.Error("an unknown column must fail on read")
	}

	// plain Go assignment skips mutators: the field is the stored value
	ana.Name = "Ana Maria"
	if !ana.IsDirty("name") {
		t.Error("plain assignment must still be tracked as dirty")
	}
	ok(t, ana.Save(ctx))
	if got := rawColumn(t, db, "users", "name", 1); got != "Ana Maria" {
		t.Errorf("name = %q", got)
	}
}

func TestJSONColumnRoundTripsATypedValue(t *testing.T) {
	ctx := context.Background()
	db := seeded(t)
	users := orm.New[User](db)

	u := must(users.Find(ctx, 1))
	if u.Settings.Data.Theme != "" {
		t.Fatalf("fresh row settings: %+v", u.Settings.Data)
	}
	u.Settings.Data = UserSettings{Theme: "dark", Notifications: true}
	if !u.IsDirty("settings") {
		t.Fatal("changing the JSON value must mark the column dirty")
	}
	ok(t, u.Save(ctx))

	back := must(users.Find(ctx, 1))
	if back.Settings.Data != (UserSettings{Theme: "dark", Notifications: true}) {
		t.Fatalf("settings came back as %+v", back.Settings.Data)
	}
	var stored map[string]any
	ok(t, json.Unmarshal([]byte(rawColumn(t, db, "users", "settings", 1)), &stored))
	if stored["theme"] != "dark" || stored["notifications"] != true {
		t.Errorf("stored JSON: %v", stored)
	}

	// serialization nests the value, not a wrapper
	m, err := orm.ToMap(back)
	ok(t, err)
	nested, isMap := m["settings"].(UserSettings)
	if !isMap || nested.Theme != "dark" {
		t.Errorf("ToMap settings: %#v", m["settings"])
	}
	js, err := orm.JSON(back)
	ok(t, err)
	if !strings.Contains(string(js), `"settings":{"theme":"dark","notifications":true}`) {
		t.Errorf("JSON: %s", js)
	}

	// mass assignment accepts the typed value
	ok(t, back.Update(ctx, map[string]any{"settings": orm.JSONColumn[UserSettings]{Data: UserSettings{Theme: "light"}}}))
	if got := must(users.Find(ctx, 1)).Settings.Data.Theme; got != "light" {
		t.Errorf("theme after Update: %s", got)
	}
}
