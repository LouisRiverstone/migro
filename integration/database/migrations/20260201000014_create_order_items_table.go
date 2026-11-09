package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateOrderItemsTable struct{}

func init() { migrate.Register(&CreateOrderItemsTable{}) }

func (CreateOrderItemsTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "order_items", func(t *schema.Table) {
		// composite primary key whose first half is also a cascading foreign key
		t.Integer("order_id").Unsigned().NotNull().Constrained("orders").OnDelete("CASCADE")
		t.Integer("line_no").NotNull()
		t.Integer("product_id").Unsigned().NotNull().Constrained("products").OnDelete("RESTRICT")
		t.Integer("quantity").NotNull()
		t.Decimal("unit_price", 10, 2).NotNull()
		t.Primary("order_id", "line_no")
	})
}

func (CreateOrderItemsTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "order_items")
}
