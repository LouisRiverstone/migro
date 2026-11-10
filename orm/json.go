package orm

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// JSONColumn is a typed JSON column: the struct field holds a T and the database holds its JSON text
// (Eloquent's array / AsArrayObject cast, with the type checked by the compiler).
//
//	type User struct {
//	    orm.BaseModel
//	    Settings orm.JSONColumn[Settings]
//	}
//	user.Settings.Data.Theme = "dark"
//
// A zero T (nil map, nil slice, nil pointer) is stored as NULL.
type JSONColumn[T any] struct{ Data T }

// Value implements driver.Valuer.
func (j JSONColumn[T]) Value() (driver.Value, error) {
	b, err := json.Marshal(j.Data)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return nil, nil
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (j *JSONColumn[T]) Scan(src any) error {
	var b []byte
	switch x := src.(type) {
	case nil:
		var zero T
		j.Data = zero
		return nil
	case string:
		b = []byte(x)
	case []byte:
		b = x
	default:
		return fmt.Errorf("orm.JSONColumn: cannot scan %T", src)
	}
	if len(b) == 0 {
		var zero T
		j.Data = zero
		return nil
	}
	var v T // decode into a fresh value so fields from a previous row never leak through
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	j.Data = v
	return nil
}

// MarshalJSON makes ToMap / JSON output the nested value, not {"Data": ...}.
func (j JSONColumn[T]) MarshalJSON() ([]byte, error) { return json.Marshal(j.Data) }

// UnmarshalJSON is the inverse of MarshalJSON.
func (j *JSONColumn[T]) UnmarshalJSON(b []byte) error { return json.Unmarshal(b, &j.Data) }

// GormDataType tells GORM which column type to assume.
func (JSONColumn[T]) GormDataType() string { return "json" }

// GormDBDataType picks the native type per database.
func (JSONColumn[T]) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Name() {
	case "postgres":
		return "JSONB"
	case "mysql":
		return "JSON"
	}
	return "TEXT"
}
