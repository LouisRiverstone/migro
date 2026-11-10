package models

import "github.com/LouisRiverstone/migro/orm"

type Role struct {
	orm.BaseModel
	Name string

	Users orm.BelongsToMany[User]
}
