package orm_test

import (
	"strings"
	"testing"
	"time"

	"github.com/LouisRiverstone/migro/orm"
)

func setOK(t *testing.T, c orm.Cast, v any) any {
	t.Helper()
	out, err := c.Set(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return out
}

func TestScalarCasts(t *testing.T) {
	for _, tc := range []struct {
		name string
		cast orm.Cast
		in   any
		want any
		fail bool
	}{
		{"bool true word", orm.CastBool(), "yes", true, false},
		{"bool false word", orm.CastBool(), "off", false, false},
		{"bool number", orm.CastBool(), 2, true, false},
		{"bool zero", orm.CastBool(), 0, false, false},
		{"bool garbage", orm.CastBool(), "maybe", nil, true},
		{"bool nil", orm.CastBool(), nil, nil, false},
		{"int from string", orm.CastInt(), " 42 ", int64(42), false},
		{"int from float truncates", orm.CastInt(), 3.9, int64(3), false},
		{"int garbage", orm.CastInt(), "4x", nil, true},
		{"float from string", orm.CastFloat(), "1.5", 1.5, false},
		{"float from int", orm.CastFloat(), 2, 2.0, false},
		{"float from bool", orm.CastFloat(), true, 1.0, false},
		{"float unsupported", orm.CastFloat(), []int{1}, nil, true},
	} {
		got, err := tc.cast.Set(tc.in)
		if tc.fail {
			if err == nil {
				t.Errorf("%s: expected an error", tc.name)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got %v (%v), want %v", tc.name, got, err, tc.want)
		}
		// the same coercion applies when reading
		if back, err := tc.cast.Get(tc.in); err != nil || back != tc.want {
			t.Errorf("%s (Get): got %v (%v), want %v", tc.name, back, err, tc.want)
		}
	}
}

func TestEnumCast(t *testing.T) {
	c := orm.CastEnum("a", "b")
	if v, err := c.Set("a"); err != nil || v != "a" {
		t.Errorf("valid: %v %v", v, err)
	}
	if _, err := c.Set("z"); err == nil || !strings.Contains(err.Error(), "not one of") {
		t.Errorf("invalid: %v", err)
	}
	if v, err := c.Set(nil); v != nil || err != nil {
		t.Errorf("nil passes: %v %v", v, err)
	}
	if v, _ := c.Get("anything"); v != "anything" {
		t.Error("Get is a passthrough")
	}
}

func TestJSONCast(t *testing.T) {
	c := orm.CastJSON()
	s, err := c.Set(map[string]any{"a": 1})
	if err != nil || s != `{"a":1}` {
		t.Fatalf("Set: %v %v", s, err)
	}
	got, err := c.Get(s)
	if err != nil || got.(map[string]any)["a"] != float64(1) {
		t.Fatalf("Get: %v %v", got, err)
	}
	if v, _ := c.Get(""); v != nil {
		t.Error("empty text decodes to nil")
	}
	if v, _ := c.Get(nil); v != nil {
		t.Error("nil decodes to nil")
	}
	if _, err := c.Get("{broken"); err == nil {
		t.Error("broken JSON must fail")
	}
	if _, err := c.Get(12); err == nil {
		t.Error("a non-text raw value must fail")
	}
}

func TestDateCast(t *testing.T) {
	c := orm.CastDate("2006-01-02")
	got, err := c.Set("2024-02-29")
	if err != nil || !got.(time.Time).Equal(time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("Set: %v %v", got, err)
	}
	if _, err := c.Set("2024-13-40"); err == nil {
		t.Error("invalid date must fail")
	}
	if _, err := c.Set(5); err == nil {
		t.Error("an int is not a date")
	}
	now := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if v, _ := c.Set(now); v != now {
		t.Error("times pass through")
	}
	if v, _ := c.Get(now); v != "2020-01-02" {
		t.Errorf("Get: %v", v)
	}
	var none *time.Time
	if v, _ := c.Get(none); v != nil {
		t.Error("nil pointer stays nil")
	}
}

func TestHashedCast(t *testing.T) {
	c := orm.CastHashedCost(4)
	h1 := setOK(t, c, "pw")
	h2 := setOK(t, c, "pw")
	if h1 == h2 || h1 == "pw" {
		t.Error("hashes are salted and never the plain text")
	}
	if !orm.CompareHash(h1.(string), "pw") || orm.CompareHash(h1.(string), "other") {
		t.Error("CompareHash")
	}
	if again := setOK(t, c, h1); again != h1 {
		t.Error("an existing hash is kept")
	}
	// a 60-char string that merely looks like a hash is still hashed
	fake := "$2a$" + strings.Repeat("x", 56)
	if got := setOK(t, c, fake); got == fake {
		t.Error("a malformed hash must be hashed, not trusted")
	}
	if got := setOK(t, c, []byte("pw")); !orm.CompareHash(got.(string), "pw") {
		t.Error("[]byte input")
	}
	if _, err := c.Set(12); err == nil {
		t.Error("only text can be hashed")
	}
	if v, _ := c.Set(nil); v != nil {
		t.Error("nil stays nil")
	}
	if v, _ := c.Get("x"); v != "x" {
		t.Error("Get returns the stored hash")
	}
}

func TestEncryptedCast(t *testing.T) {
	key := []byte("0123456789abcdef")
	c := orm.CastEncrypted(key)
	ct := setOK(t, c, "secret").(string)
	if ct == "secret" || strings.Contains(ct, "secret") {
		t.Fatal("not encrypted")
	}
	if pt, err := c.Get(ct); err != nil || pt != "secret" {
		t.Fatalf("round trip: %v %v", pt, err)
	}
	if pt, _ := c.Get(""); pt != "" {
		t.Error("empty stays empty")
	}
	if pt, _ := c.Get(nil); pt != nil {
		t.Error("nil stays nil")
	}
	if _, err := c.Get("not base64 !!"); err == nil {
		t.Error("garbage must fail")
	}
	if _, err := c.Get("QUJD"); err == nil {
		t.Error("too short for a nonce must fail")
	}
	if _, err := orm.CastEncrypted([]byte("0123456789abcdeX")).Get(ct); err == nil {
		t.Error("another key must fail")
	}
	// flipping a byte is detected (GCM authenticates)
	b := []byte(ct)
	mid := len(b) / 2 // a middle character always carries 6 real bits, unlike the padded tail
	if b[mid] == 'A' {
		b[mid] = 'B'
	} else {
		b[mid] = 'A'
	}
	if _, err := c.Get(string(b)); err == nil {
		t.Error("tampered ciphertext must fail")
	}
	for _, size := range []int{16, 24, 32} {
		k := []byte(strings.Repeat("k", size))
		if _, err := orm.CastEncrypted(k).Set("x"); err != nil {
			t.Errorf("key size %d: %v", size, err)
		}
	}
	if _, err := orm.CastEncrypted([]byte("short")).Set("x"); err == nil {
		t.Error("bad key size")
	}
	if _, err := c.Set(5); err == nil {
		t.Error("only text can be encrypted")
	}
}

func TestCastFunc(t *testing.T) {
	up := orm.CastFunc{OnSet: func(v any) (any, error) { return strings.ToUpper(v.(string)), nil }}
	if v, _ := up.Set("a"); v != "A" {
		t.Error("OnSet")
	}
	if v, _ := up.Get("a"); v != "a" {
		t.Error("a missing side passes through")
	}
	get := orm.CastFunc{OnGet: func(raw any) (any, error) { return raw.(string) + "!", nil }}
	if v, _ := get.Get("a"); v != "a!" {
		t.Error("OnGet")
	}
	if v, _ := get.Set("a"); v != "a" {
		t.Error("a missing side passes through")
	}
}

func TestJSONColumnScanAndValue(t *testing.T) {
	type cfg struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	var j orm.JSONColumn[cfg]
	if v, err := j.Value(); err != nil || v == nil {
		t.Errorf("a struct always encodes: %v %v", v, err)
	}
	if err := j.Scan(`{"a":1,"b":"x"}`); err != nil || j.Data != (cfg{1, "x"}) {
		t.Fatalf("scan string: %+v %v", j.Data, err)
	}
	if err := j.Scan([]byte(`{"a":2}`)); err != nil || j.Data != (cfg{2, ""}) {
		t.Fatalf("scan bytes: %+v %v", j.Data, err)
	}
	if err := j.Scan(nil); err != nil || j.Data != (cfg{}) {
		t.Fatalf("scan nil resets: %+v %v", j.Data, err)
	}
	if err := j.Scan(""); err != nil || j.Data != (cfg{}) {
		t.Fatalf("scan empty resets: %+v %v", j.Data, err)
	}
	if err := j.Scan(5); err == nil {
		t.Error("an int is not JSON")
	}
	if err := j.Scan("{broken"); err == nil {
		t.Error("broken JSON must fail")
	}

	var m orm.JSONColumn[map[string]int]
	if v, _ := m.Value(); v != nil {
		t.Errorf("a nil map is NULL, got %v", v)
	}
	m.Data = map[string]int{"x": 1}
	if v, _ := m.Value(); v != `{"x":1}` {
		t.Errorf("value %v", v)
	}
	if b, _ := m.MarshalJSON(); string(b) != `{"x":1}` {
		t.Errorf("MarshalJSON %s", b)
	}
	var back orm.JSONColumn[map[string]int]
	if err := back.UnmarshalJSON([]byte(`{"y":2}`)); err != nil || back.Data["y"] != 2 {
		t.Errorf("UnmarshalJSON %v %v", back.Data, err)
	}
}
