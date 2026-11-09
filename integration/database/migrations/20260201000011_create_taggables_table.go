package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateTaggablesTable struct{}

func init() { migrate.Register(&CreateTaggablesTable{}) }

func (CreateTaggablesTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "taggables", func(t *schema.Table) {
		// polymorphic many-to-many pivot
		t.Integer("tag_id").NotNull().Constrained("tags").OnDelete("CASCADE")
		t.Morphs("taggable")
		t.Primary("tag_id", "taggable_id", "taggable_type")
	})
}

func (CreateTaggablesTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "taggables")
}
