package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateRolesTable struct{}

func init() { migrate.Register(&CreateRolesTable{}) }

func (CreateRolesTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "roles", func(t *schema.Table) {
		t.Increments("id")
		t.String("name", 50).NotNull().Unique()
		t.Timestamps()
	})
}

func (CreateRolesTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "roles")
}
