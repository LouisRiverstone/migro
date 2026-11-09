package seeders

import (
	"context"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/integration/models"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/seed"
)

type ContentSeeder struct{}

func init() { seed.Register(&ContentSeeder{}) }

func (ContentSeeder) Run(ctx context.Context, db *migro.DB) error {
	find := func(id uint) (*models.User, error) { return orm.New[models.User](db).Find(ctx, id) }
	ana, err := find(1)
	if err != nil {
		return err
	}
	bia, err := find(2)
	if err != nil {
		return err
	}
	caio, err := find(3)
	if err != nil {
		return err
	}

	// categories: a three-level tree built through the Children relation
	tech := &models.Category{Name: "Tech"}
	if err := orm.New[models.Category](db).Create(ctx, tech); err != nil {
		return err
	}
	databases, err := tech.Children.Create(ctx, map[string]any{"name": "Databases"})
	if err != nil {
		return err
	}
	migrations, err := databases.Children.Create(ctx, map[string]any{"name": "Migrations"})
	if err != nil {
		return err
	}

	post1, err := ana.Posts.Create(ctx, map[string]any{"category_id": databases.ID, "slug": "hello-db", "title": "Hello DB", "published": true})
	if err != nil {
		return err
	}
	post2, err := ana.Posts.Create(ctx, map[string]any{"category_id": migrations.ID, "slug": "rolling-back", "title": "Rolling back", "published": true})
	if err != nil {
		return err
	}
	draft, err := bia.Posts.Create(ctx, map[string]any{"category_id": tech.ID, "slug": "draft", "title": "A draft"})
	if err != nil {
		return err
	}
	video, err := bia.Videos.Create(ctx, map[string]any{"title": "Intro to indexes", "seconds": 600})
	if err != nil {
		return err
	}

	// polymorphic comments: the relation fills commentable_type / commentable_id
	for _, c := range []struct {
		on interface {
			Create(context.Context, map[string]any) (*models.Comment, error)
		}
		author uint
		body   string
	}{
		{&post1.Comments, bia.ID, "nice"},
		{&post1.Comments, caio.ID, "thanks"},
		{&video.Comments, caio.ID, "great video"},
		{&draft.Comments, ana.ID, "ghost"},
	} {
		if _, err := c.on.Create(ctx, map[string]any{"user_id": c.author, "body": c.body}); err != nil {
			return err
		}
	}

	// polymorphic many-to-many: the same tags on posts and a video
	tags, err := orm.New[models.Tag](db).FindMany(ctx, 1, 2)
	if err != nil {
		return err
	}
	goTag, sqlTag := &tags[0], &tags[1]
	if err := post1.Tags.Attach(ctx, goTag, sqlTag); err != nil {
		return err
	}
	if err := post2.Tags.Attach(ctx, goTag); err != nil {
		return err
	}
	return video.Tags.Attach(ctx, sqlTag)
}
