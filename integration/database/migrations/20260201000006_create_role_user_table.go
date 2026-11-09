package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateRoleUserTable struct{}

func init() { migrate.Register(&CreateRoleUserTable{}) }

func (CreateRoleUserTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "role_user", func(t *schema.Table) {
		// many-to-many pivot with a composite primary key
		t.Integer("user_id").NotNull().Constrained("users").OnDelete("CASCADE")
		t.Integer("role_id").NotNull().Index().Constrained("roles").OnDelete("CASCADE")
		t.Primary("user_id", "role_id")
	})
}

func (CreateRoleUserTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "role_user")
}
