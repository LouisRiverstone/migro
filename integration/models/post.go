package models

import (
	"github.com/LouisRiverstone/migro/orm"
	"gorm.io/gorm"
)

type Post struct {
	orm.BaseModel
	UserID     uint
	CategoryID *uint
	Slug       string
	Title      string
	Published  bool
	Archived   bool

	User     orm.BelongsTo[User]
	Category orm.BelongsTo[Category]
	Comments orm.MorphMany[Comment] `orm:"morph:commentable"`
	Tags     orm.MorphToMany[Tag]   `orm:"morph:taggable"`
}

// GlobalScopes hides archived posts from every query, like an Eloquent global scope. Switch it off
// with Query().WithoutGlobalScope("not_archived").
func (Post) GlobalScopes() []orm.NamedScope {
	return []orm.NamedScope{{
		Name: "not_archived",
		Fn:   func(g *gorm.DB) *gorm.DB { return g.Where("archived = ?", false) },
	}}
}
