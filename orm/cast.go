package orm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LouisRiverstone/migro"
	"golang.org/x/crypto/bcrypt"
)

// Casts are Eloquent's mutators and accessors in one place. A model lists them per column:
//
//	func (User) Casts() map[string]orm.Cast {
//	    return map[string]orm.Cast{"password": orm.CastHashed(), "settings": orm.CastJSON()}
//	}
//
// Set runs when a value enters the model through Fill, Update, Make, CreateFrom or SetAttribute;
// Get runs when it leaves through ToMap, JSON or GetAttribute. Plain Go assignment (user.Name = "x")
// and Save are untouched: the struct field always holds the stored value.
type Cast interface {
	Get(raw any) (any, error)
	Set(v any) (any, error)
}

// Casted is implemented by models that declare casts, keyed by column name.
type Casted interface{ Casts() map[string]Cast }

// CastFunc builds a cast from two functions; a nil side passes the value through. It is the
// way to write a custom accessor or mutator.
//
//	"email": orm.CastFunc{OnSet: func(v any) (any, error) { return strings.ToLower(fmt.Sprint(v)), nil }}
type CastFunc struct {
	OnGet func(raw any) (any, error)
	OnSet func(v any) (any, error)
}

func (c CastFunc) Get(raw any) (any, error) {
	if c.OnGet == nil {
		return raw, nil
	}
	return c.OnGet(raw)
}

func (c CastFunc) Set(v any) (any, error) {
	if c.OnSet == nil {
		return v, nil
	}
	return c.OnSet(v)
}

func castFor(self any, col string) Cast {
	c, ok := self.(Casted)
	if !ok {
		return nil
	}
	return c.Casts()[col]
}

func castErr(col string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*migro.Error); ok {
		return err
	}
	return migro.Wrap(migro.CastFailed, err, "cast on column %q", col)
}

// ---- built-in casts ---------------------------------------------------------------------------

// CastJSON stores any value as JSON text in a string column (Eloquent's array / json cast). Set
// encodes, Get decodes into map[string]any, []any or a scalar. For a typed value use JSONColumn[T].
func CastJSON() Cast { return jsonCast{} }

type jsonCast struct{}

func (jsonCast) Set(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		return string(x), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (jsonCast) Get(raw any) (any, error) {
	var b []byte
	switch x := raw.(type) {
	case nil:
		return nil, nil
	case string:
		b = []byte(x)
	case []byte:
		b = x
	case *string:
		if x == nil {
			return nil, nil
		}
		b = []byte(*x)
	default:
		return nil, fmt.Errorf("json cast: cannot decode %T", raw)
	}
	if len(b) == 0 {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CastDate parses strings into time.Time on the way in and formats times on the way out, using
// layout (Eloquent's date / datetime:Y-m-d cast).
func CastDate(layout string) Cast { return dateCast{layout} }

type dateCast struct{ layout string }

func (c dateCast) Set(v any) (any, error) {
	switch x := v.(type) {
	case string:
		return time.Parse(c.layout, x)
	case time.Time, *time.Time, nil:
		return v, nil
	}
	return nil, fmt.Errorf("date cast: cannot parse %T", v)
}

func (c dateCast) Get(raw any) (any, error) {
	switch x := raw.(type) {
	case time.Time:
		return x.Format(c.layout), nil
	case *time.Time:
		if x == nil {
			return nil, nil
		}
		return x.Format(c.layout), nil
	}
	return raw, nil
}

// CastBool coerces on both sides: "1", "true", "on", "yes" and non-zero numbers are true.
func CastBool() Cast { return CastFunc{OnGet: toBool, OnSet: toBool} }

// CastInt coerces numeric strings and floats to int64 on both sides.
func CastInt() Cast { return CastFunc{OnGet: toInt, OnSet: toInt} }

// CastFloat coerces numeric strings and integers to float64 on both sides.
func CastFloat() Cast { return CastFunc{OnGet: toFloat, OnSet: toFloat} }

func toBool(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case bool:
		return x, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "1", "t", "true", "on", "yes", "y":
			return true, nil
		case "0", "f", "false", "off", "no", "n", "":
			return false, nil
		}
		return nil, fmt.Errorf("bool cast: %q is not a boolean", x)
	}
	f, err := toFloat(v)
	if err != nil {
		return nil, err
	}
	return f.(float64) != 0, nil
}

func toInt(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	if s, ok := v.(string); ok {
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("int cast: %q is not an integer", s)
		}
		return n, nil
	}
	f, err := toFloat(v)
	if err != nil {
		return nil, err
	}
	return int64(f.(float64)), nil
}

func toFloat(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case int:
		return float64(x), nil
	case int8:
		return float64(x), nil
	case int16:
		return float64(x), nil
	case int32:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case uint:
		return float64(x), nil
	case uint8:
		return float64(x), nil
	case uint16:
		return float64(x), nil
	case uint32:
		return float64(x), nil
	case uint64:
		return float64(x), nil
	case bool:
		if x {
			return 1.0, nil
		}
		return 0.0, nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return nil, fmt.Errorf("number cast: %q is not a number", x)
		}
		return f, nil
	}
	return nil, fmt.Errorf("number cast: cannot convert %T", v)
}

// CastEnum rejects any value outside values with CAST_FAILED before the row is written.
func CastEnum(values ...string) Cast {
	return CastFunc{OnSet: func(v any) (any, error) {
		if v == nil {
			return nil, nil
		}
		if !slices.Contains(values, fmt.Sprint(v)) {
			return nil, fmt.Errorf("%v is not one of %v", v, values)
		}
		return v, nil
	}}
}

// CastHashed bcrypt-hashes a string on Set (Eloquent's hashed cast). A value that already is a
// bcrypt hash is kept, so re-filling a loaded model does not hash twice. Get returns the hash:
// list the column in Hidden. Verify with CompareHash.
func CastHashed() Cast { return CastHashedCost(bcrypt.DefaultCost) }

// CastHashedCost is CastHashed with an explicit bcrypt cost.
func CastHashedCost(cost int) Cast {
	return CastFunc{OnSet: func(v any) (any, error) {
		var plain []byte
		switch x := v.(type) {
		case nil:
			return nil, nil
		case string:
			plain = []byte(x)
		case []byte:
			plain = x
		default:
			return nil, fmt.Errorf("hashed cast: cannot hash %T", v)
		}
		if isBcrypt(plain) {
			return string(plain), nil
		}
		h, err := bcrypt.GenerateFromPassword(plain, cost)
		if err != nil {
			return nil, err
		}
		return string(h), nil
	}}
}

func isBcrypt(b []byte) bool {
	if len(b) != 60 || b[0] != '$' || b[1] != '2' {
		return false
	}
	_, err := bcrypt.Cost(b)
	return err == nil
}

// CompareHash reports whether plain matches a bcrypt hash made by CastHashed.
func CompareHash(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// CastEncrypted encrypts a string with AES-GCM on Set and decrypts it on Get (Eloquent's encrypted
// cast). key must be 16, 24 or 32 bytes; a wrong size fails every use with CAST_FAILED. The stored
// value is base64 of nonce+ciphertext, so the column must be text. Changing the key makes existing
// rows unreadable.
func CastEncrypted(key []byte) Cast {
	return CastFunc{
		OnSet: func(v any) (any, error) {
			if v == nil {
				return nil, nil
			}
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("encrypted cast: cannot encrypt %T", v)
			}
			gcm, err := newGCM(key)
			if err != nil {
				return nil, err
			}
			nonce := make([]byte, gcm.NonceSize())
			if _, err := rand.Read(nonce); err != nil {
				return nil, err
			}
			return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(s), nil)), nil
		},
		OnGet: func(raw any) (any, error) {
			if raw == nil {
				return nil, nil
			}
			s, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("encrypted cast: cannot decrypt %T", raw)
			}
			if s == "" {
				return "", nil
			}
			gcm, err := newGCM(key)
			if err != nil {
				return nil, err
			}
			b, err := base64.StdEncoding.DecodeString(s)
			if err != nil || len(b) < gcm.NonceSize() {
				return nil, fmt.Errorf("encrypted cast: value is not ciphertext")
			}
			plain, err := gcm.Open(nil, b[:gcm.NonceSize()], b[gcm.NonceSize():], nil)
			if err != nil {
				return nil, fmt.Errorf("encrypted cast: wrong key or corrupted value")
			}
			return string(plain), nil
		},
	}
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
