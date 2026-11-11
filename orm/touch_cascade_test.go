package orm_test

import (
	"context"
	"testing"
	"time"

	"github.com/LouisRiverstone/migro/internal/testdb"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/schema"
)

type tBlog struct{ orm.BaseModel }

func (tBlog) TableName() string { return "t_blogs" }

type tPost struct {
	orm.BaseModel
	TBlogID uint                 `gorm:"column:t_blog_id"`
	Blog    orm.BelongsTo[tBlog] `orm:"foreignKey:t_blog_id"`
}

func (tPost) TableName() string { return "t_posts" }
func (tPost) Touches() []string { return []string{"Blog"} }

type tComment struct {
	orm.BaseModel
	TPostID uint                 `gorm:"column:t_post_id"`
	Post    orm.BelongsTo[tPost] `orm:"foreignKey:t_post_id"`
}

func (tComment) TableName() string { return "t_comments" }
func (tComment) Touches() []string { return []string{"Post"} }

func TestTouchesCascade(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	s := schema.New(db)
	ok(t, s.CreateTable(ctx, "t_blogs", func(t *schema.Table) { t.Increments("id"); t.Timestamps() }))
	ok(t, s.CreateTable(ctx, "t_posts", func(t *schema.Table) { t.Increments("id"); t.Integer("t_blog_id"); t.Timestamps() }))
	ok(t, s.CreateTable(ctx, "t_comments", func(t *schema.Table) { t.Increments("id"); t.Integer("t_post_id"); t.Timestamps() }))

	blog := &tBlog{}
	ok(t, orm.New[tBlog](db).Create(ctx, blog))
	post := &tPost{TBlogID: blog.ID}
	ok(t, orm.New[tPost](db).Create(ctx, post))

	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	db.Gorm.Exec("UPDATE t_blogs SET updated_at = ?", old)
	db.Gorm.Exec("UPDATE t_posts SET updated_at = ?", old)

	ok(t, orm.New[tComment](db).Create(ctx, &tComment{TPostID: post.ID}))
	for _, tbl := range []string{"t_posts", "t_blogs"} {
		var at time.Time
		ok(t, db.Gorm.Raw("SELECT updated_at FROM "+tbl).Row().Scan(&at))
		if at.Year() == 2001 {
			t.Fatalf("%s was not touched by the cascade", tbl)
		}
	}
}
