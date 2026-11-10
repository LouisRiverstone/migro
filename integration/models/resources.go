package models

import "github.com/LouisRiverstone/migro/orm"

// CommentResource is the API shape of a comment.
var CommentResource = orm.NewResource(func(c *Comment, r *orm.R) {
	r.Set("id", c.ID)
	r.Set("body", c.Body)
})

// PostResource is the API shape of a post; comments appear only when they were eager loaded.
var PostResource = orm.NewResource(func(p *Post, r *orm.R) {
	r.Set("id", p.ID)
	r.Set("slug", p.Slug)
	r.Set("title", p.Title)
	r.Set("published", p.Published)
	r.WhenLoaded("Comments", func() (any, error) { return CommentResource.List(p.Comments.Items()) })
})

// UserResource is the API shape of a user: no password, no token, the role only for admins.
var UserResource = orm.NewResource(func(u *User, r *orm.R) {
	r.Set("id", u.ID)
	r.Set("name", u.Name)
	r.Set("email", u.Email)
	r.When(u.Role == "admin", "role", u.Role)
	r.WhenLoaded("Posts", func() (any, error) { return PostResource.List(u.Posts.Items()) })
})
