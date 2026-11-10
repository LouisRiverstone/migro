package models

import "github.com/LouisRiverstone/migro/orm"

// Local query scopes: plain functions applied with Query.Apply.

// PublishedPosts keeps published posts.
func PublishedPosts(q *orm.Query[Post]) *orm.Query[Post] { return q.Where("published = ?", true) }

// PostsBy keeps one author's posts (a scope with an argument).
func PostsBy(userID uint) func(*orm.Query[Post]) *orm.Query[Post] {
	return func(q *orm.Query[Post]) *orm.Query[Post] { return q.Where("user_id = ?", userID) }
}

// TitleContains keeps posts whose title contains term.
func TitleContains(term string) func(*orm.Query[Post]) *orm.Query[Post] {
	return func(q *orm.Query[Post]) *orm.Query[Post] { return q.Where("title LIKE ?", "%"+term+"%") }
}
