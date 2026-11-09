package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateUsersTable struct{}

func init() { migrate.Register(&CreateUsersTable{}) }

func (CreateUsersTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "users", func(t *schema.Table) {
		t.Increments("id")
		t.Integer("country_id").NotNull().Index().Constrained("countries").OnDelete("RESTRICT")
		t.String("email", 150).NotNull().Unique()
		t.String("name", 100).NotNull()
		t.SoftDeletes()
		t.Timestamps()
	})
}

func (CreateUsersTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "users")
}
