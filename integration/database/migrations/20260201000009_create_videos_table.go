package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateVideosTable struct{}

func init() { migrate.Register(&CreateVideosTable{}) }

func (CreateVideosTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "videos", func(t *schema.Table) {
		t.Increments("id")
		t.Integer("user_id").Unsigned().NotNull().Constrained("users").OnDelete("CASCADE")
		t.String("title").NotNull()
		t.Integer("seconds").NotNull().Default(0)
		t.Timestamps()
	})
}

func (CreateVideosTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "videos")
}
