package models

import "github.com/LouisRiverstone/migro/orm"

type Tag struct {
	orm.BaseModel
	Name string

	Posts  orm.MorphedByMany[Post]  `gorm:"-" orm:"morph:taggable"`
	Videos orm.MorphedByMany[Video] `gorm:"-" orm:"morph:taggable"`
}
