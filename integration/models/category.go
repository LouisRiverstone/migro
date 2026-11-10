package models

import "github.com/LouisRiverstone/migro/orm"

type Category struct {
	orm.BaseModel
	ParentID *uint
	Name     string

	Parent   orm.BelongsTo[Category] `orm:"foreignKey:parent_id"`
	Children orm.HasMany[Category]   `orm:"foreignKey:parent_id"`
	Posts    orm.HasMany[Post]
}
