package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateTagsTable struct{}

func init() { migrate.Register(&CreateTagsTable{}) }

func (CreateTagsTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "tags", func(t *schema.Table) {
		t.Increments("id")
		t.String("name", 50).NotNull().Unique()
		t.Timestamps()
	})
}

func (CreateTagsTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "tags")
}
