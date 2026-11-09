package models

import "github.com/LouisRiverstone/migro/orm"

// The aliases stored in *_type columns (Laravel's Relation::morphMap).
func init() {
	orm.MorphMap(map[string]any{"post": Post{}, "video": Video{}})
}
