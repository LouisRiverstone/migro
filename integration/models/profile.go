package models

import "github.com/LouisRiverstone/migro/orm"

type Profile struct {
	orm.BaseModel
	UserID uint
	Bio    *string

	User orm.BelongsTo[User] `gorm:"-"`
}
