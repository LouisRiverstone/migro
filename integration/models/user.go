package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/LouisRiverstone/migro/orm"
	"golang.org/x/crypto/bcrypt"
)

// UserSettings is the typed payload of users.settings.
type UserSettings struct {
	Theme         string `json:"theme"`
	Notifications bool   `json:"notifications"`
}

// TokenKey encrypts users.api_token (AES-256: 32 bytes). Real applications read it from config.
var TokenKey = []byte("0123456789abcdef0123456789abcdef")

type User struct {
	orm.SoftDeleteModel
	CountryID uint
	Email     string
	Name      string
	Password  string
	Role      string `gorm:"default:member"`
	APIToken  string
	Birthday  *time.Time
	Settings  orm.JSONColumn[UserSettings]

	Country  orm.BelongsTo[Country]
	Profile  orm.HasOne[Profile]
	Posts    orm.HasMany[Post]
	Videos   orm.HasMany[Video]
	Comments orm.HasMany[Comment]
	Orders   orm.HasMany[Order]
	Roles    orm.BelongsToMany[Role]
}

// Fillable is the mass-assignment allow-list (Eloquent's $fillable).
func (User) Fillable() []string {
	return []string{"country_id", "email", "name", "password", "role", "api_token", "birthday", "settings"}
}

// Hidden keeps secrets out of ToMap / JSON (Eloquent's $hidden).
func (User) Hidden() []string { return []string{"password", "api_token"} }

// Casts are the model's mutators and accessors (Eloquent's $casts and Attribute::make).
func (User) Casts() map[string]orm.Cast {
	return map[string]orm.Cast{
		"password":  orm.CastHashedCost(bcrypt.MinCost),
		"api_token": orm.CastEncrypted(TokenKey),
		"role":      orm.CastEnum("admin", "editor", "member"),
		"birthday":  orm.CastDate("2006-01-02"),
		"email": orm.CastFunc{OnSet: func(v any) (any, error) {
			return strings.ToLower(strings.TrimSpace(fmt.Sprint(v))), nil
		}},
	}
}
