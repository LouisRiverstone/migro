package models

import "github.com/LouisRiverstone/migro/orm"

type Comment struct {
	orm.BaseModel
	UserID          uint
	CommentableType string
	CommentableID   uint
	Body            string

	User        orm.BelongsTo[User] `gorm:"-"`
	Commentable orm.MorphTo         `gorm:"-" orm:"morph:commentable"`
}
