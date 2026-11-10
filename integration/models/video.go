package models

import "github.com/LouisRiverstone/migro/orm"

type Video struct {
	orm.BaseModel
	UserID  uint
	Title   string
	Seconds int

	User     orm.BelongsTo[User]
	Comments orm.MorphMany[Comment] `orm:"morph:commentable"`
	Tags     orm.MorphToMany[Tag]   `orm:"morph:taggable"`
}
