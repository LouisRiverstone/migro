package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateProfilesTable struct{}

func init() { migrate.Register(&CreateProfilesTable{}) }

func (CreateProfilesTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "profiles", func(t *schema.Table) {
		// one-to-one: the foreign key column is also unique
		t.Increments("id")
		t.Integer("user_id").NotNull().Unique().Constrained("users").OnDelete("CASCADE")
		t.String("bio", 500).Nullable()
		t.Timestamps()
	})
}

func (CreateProfilesTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "profiles")
}
