package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateOrdersTable struct{}

func init() { migrate.Register(&CreateOrdersTable{}) }

func (CreateOrdersTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "orders", func(t *schema.Table) {
		t.Increments("id")
		t.Integer("user_id").NotNull().Constrained("users").OnDelete("RESTRICT")
		t.String("status", 20).NotNull().Default("pending")
		t.Decimal("total", 10, 2).NotNull()
		t.Timestamp("placed_at").Nullable()
		t.Timestamps()
	})
}

func (CreateOrdersTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "orders")
}
