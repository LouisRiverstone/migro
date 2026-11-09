package seeders

import (
	"context"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/integration/database/factories"
	"github.com/LouisRiverstone/migro/integration/models"
	"github.com/LouisRiverstone/migro/seed"
)

// FactorySeeder adds generated users on top of DatabaseSeeder's data; it is not part of it.
type FactorySeeder struct{}

func init() { seed.Register(&FactorySeeder{}) }

func (FactorySeeder) Run(ctx context.Context, db *migro.DB) error {
	_, err := factories.UserFactory().Seed(42).
		Sequence(func(i int, u *models.User) { u.CountryID = uint(i%3 + 1) }).
		With("posts", 2).
		CreateMany(ctx, db, 3)
	return err
}
