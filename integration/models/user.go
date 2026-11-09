package models

import "github.com/LouisRiverstone/migro/orm"

type User struct {
	orm.SoftDeleteModel
	CountryID uint
	Email     string
	Name      string

	Country  orm.BelongsTo[Country]  `gorm:"-"`
	Profile  orm.HasOne[Profile]     `gorm:"-"`
	Posts    orm.HasMany[Post]       `gorm:"-"`
	Videos   orm.HasMany[Video]      `gorm:"-"`
	Comments orm.HasMany[Comment]    `gorm:"-"`
	Orders   orm.HasMany[Order]      `gorm:"-"`
	Roles    orm.BelongsToMany[Role] `gorm:"-"`
}

// Fillable is the mass-assignment allow-list (Eloquent's $fillable).
func (User) Fillable() []string { return []string{"country_id", "email", "name"} }
