package models

import "github.com/LouisRiverstone/migro/orm"

type Video struct {
	orm.BaseModel
	UserID  uint
	Title   string
	Seconds int

	User     orm.BelongsTo[User]    `gorm:"-"`
	Comments orm.MorphMany[Comment] `gorm:"-" orm:"morph:commentable"`
	Tags     orm.MorphToMany[Tag]   `gorm:"-" orm:"morph:taggable"`
}
