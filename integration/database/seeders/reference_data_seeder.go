package seeders

import (
	"context"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/integration/models"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/seed"
)

type ReferenceDataSeeder struct{}

func init() { seed.Register(&ReferenceDataSeeder{}) }

// Rows get sequential ids (1, 2, 3...) from the auto-increment key, in this order.
func (ReferenceDataSeeder) Run(ctx context.Context, db *migro.DB) error {
	if err := orm.New[models.Country](db).CreateMany(ctx, []models.Country{
		{Code: "BR", Name: "Brazil"}, {Code: "PT", Name: "Portugal"}, {Code: "NZ", Name: "New Zealand"},
	}); err != nil {
		return err
	}
	if err := orm.New[models.Role](db).CreateMany(ctx, []models.Role{
		{Name: "admin"}, {Name: "editor"}, {Name: "reader"},
	}); err != nil {
		return err
	}
	return orm.New[models.Tag](db).CreateMany(ctx, []models.Tag{
		{Name: "go"}, {Name: "sql"}, {Name: "orm"}, {Name: "unused"},
	})
}
