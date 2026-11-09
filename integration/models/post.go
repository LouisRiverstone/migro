package models

import "github.com/LouisRiverstone/migro/orm"

type Post struct {
	orm.BaseModel
	UserID     uint
	CategoryID *uint
	Slug       string
	Title      string
	Published  bool

	User     orm.BelongsTo[User]     `gorm:"-"`
	Category orm.BelongsTo[Category] `gorm:"-"`
	Comments orm.MorphMany[Comment]  `gorm:"-" orm:"morph:commentable"`
	Tags     orm.MorphToMany[Tag]    `gorm:"-" orm:"morph:taggable"`
}
