package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type AddPhoneToUsersTable struct{}

func init() { migrate.Register(&AddPhoneToUsersTable{}) }

func (AddPhoneToUsersTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.AlterTable(ctx, "users", func(t *schema.Table) { t.String("phone", 30).Nullable() })
}

func (AddPhoneToUsersTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.AlterTable(ctx, "users", func(t *schema.Table) { t.DropColumn("phone") })
}
