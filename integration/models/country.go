// Package models holds the integration database's models, written the way an application would:
// BaseModel for the instance API, relations declared as typed fields.
package models

import "github.com/LouisRiverstone/migro/orm"

type Country struct {
	orm.BaseModel
	Code string
	Name string

	Users orm.HasMany[User]
	Posts orm.HasManyThrough[Post, User]
}
