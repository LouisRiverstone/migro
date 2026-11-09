package seeders

import (
	"context"
	"time"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/integration/models"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/seed"
)

type OrderSeeder struct{}

func init() { seed.Register(&OrderSeeder{}) }

func (OrderSeeder) Run(ctx context.Context, db *migro.DB) error {
	products := orm.New[models.Product](db)
	if err := products.CreateMany(ctx, []models.Product{
		{SKU: "SKU-1", Name: "Keyboard", Price: 49.90, Stock: 10},
		{SKU: "SKU-2", Name: "Mouse", Price: 19.50},
	}); err != nil {
		return err
	}
	ana, err := orm.New[models.User](db).Find(ctx, 1)
	if err != nil {
		return err
	}
	bia, err := orm.New[models.User](db).Find(ctx, 2)
	if err != nil {
		return err
	}
	first, second := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC), time.Date(2026, 2, 2, 11, 0, 0, 0, time.UTC)

	paid, err := ana.Orders.Create(ctx, map[string]any{"status": "paid", "total": 119.30, "placed_at": first})
	if err != nil {
		return err
	}
	if _, err := paid.Items.CreateMany(ctx, []map[string]any{
		{"line_no": 1, "product_id": 1, "quantity": 2, "unit_price": 49.90},
		{"line_no": 2, "product_id": 2, "quantity": 1, "unit_price": 19.50},
	}); err != nil {
		return err
	}
	pending, err := bia.Orders.Create(ctx, map[string]any{"status": "pending", "total": 19.50, "placed_at": second})
	if err != nil {
		return err
	}
	_, err = pending.Items.Create(ctx, map[string]any{"line_no": 1, "product_id": 2, "quantity": 1, "unit_price": 19.50})
	return err
}
