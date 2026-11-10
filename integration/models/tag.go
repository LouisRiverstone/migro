package models

import "github.com/LouisRiverstone/migro/orm"

type Tag struct {
	orm.BaseModel
	Name string

	Posts  orm.MorphedByMany[Post]  `orm:"morph:taggable"`
	Videos orm.MorphedByMany[Video] `orm:"morph:taggable"`
}
