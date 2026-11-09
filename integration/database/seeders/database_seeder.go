package seeders

import (
	"context"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/seed"
)

// DatabaseSeeder is the root seeder (php artisan db:seed). Registered seeders run in name order,
// which ignores foreign keys, so this one calls the others in the order the schema needs.
type DatabaseSeeder struct{}

func init() { seed.Register(&DatabaseSeeder{}) }

func (DatabaseSeeder) Run(ctx context.Context, db *migro.DB) error {
	return seed.Call(ctx, db, &ReferenceDataSeeder{}, &UserSeeder{}, &ContentSeeder{}, &OrderSeeder{})
}
