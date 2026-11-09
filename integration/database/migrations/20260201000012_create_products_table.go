package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateProductsTable struct{}

func init() { migrate.Register(&CreateProductsTable{}) }

func (CreateProductsTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "products", func(t *schema.Table) {
		t.Increments("id")
		t.String("sku", 40).NotNull().Unique()
		t.String("name").NotNull()
		t.Decimal("price", 10, 2).NotNull()
		t.Integer("stock").NotNull().Default(0)
		t.Timestamps()
	})
}

func (CreateProductsTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "products")
}
