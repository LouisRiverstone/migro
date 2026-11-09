package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreatePostsTable struct{}

func init() { migrate.Register(&CreatePostsTable{}) }

func (CreatePostsTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "posts", func(t *schema.Table) {
		t.Increments("id")
		t.Integer("user_id").NotNull().Constrained("users").OnDelete("CASCADE")
		t.Integer("category_id").Nullable().Constrained("categories").OnDelete("SET NULL")
		t.String("slug").NotNull().Unique()
		t.String("title").NotNull()
		t.Boolean("published").NotNull().Default(false)
		t.Unique("user_id", "title").Named("posts_user_title_unique")
		t.Index("user_id", "published").Named("posts_user_published_idx")
		t.Timestamps()
	})
}

func (CreatePostsTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "posts")
}
