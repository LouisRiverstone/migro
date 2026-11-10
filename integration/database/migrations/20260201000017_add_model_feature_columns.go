package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

// AddModelFeatureColumns adds the columns the casting, scope and JSON-column tests rely on.
type AddModelFeatureColumns struct{}

func init() { migrate.Register(&AddModelFeatureColumns{}) }

func (AddModelFeatureColumns) Up(ctx context.Context, s *schema.Builder) error {
	if err := s.AlterTable(ctx, "users", func(t *schema.Table) {
		t.String("password", 100).Nullable()
		t.String("role", 20).NotNull().Default("member")
		t.Text("api_token").Nullable()
		t.Date("birthday").Nullable()
		t.JSON("settings").Nullable()
	}); err != nil {
		return err
	}
	return s.AlterTable(ctx, "posts", func(t *schema.Table) {
		t.Boolean("archived").NotNull().Default(false)
	})
}

func (AddModelFeatureColumns) Down(ctx context.Context, s *schema.Builder) error {
	if err := s.AlterTable(ctx, "posts", func(t *schema.Table) { t.DropColumn("archived") }); err != nil {
		return err
	}
	return s.AlterTable(ctx, "users", func(t *schema.Table) {
		t.DropColumn("settings")
		t.DropColumn("birthday")
		t.DropColumn("api_token")
		t.DropColumn("role")
		t.DropColumn("password")
	})
}
