package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateCommentsTable struct{}

func init() { migrate.Register(&CreateCommentsTable{}) }

func (CreateCommentsTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "comments", func(t *schema.Table) {
		// polymorphic one-to-many: (commentable_type, commentable_id) is deliberately not a foreign key
		t.Increments("id")
		t.Integer("user_id").Unsigned().NotNull().Constrained("users").OnDelete("CASCADE")
		t.Morphs("commentable")
		t.Text("body").NotNull()
		t.Timestamps()
	})
}

func (CreateCommentsTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "comments")
}
