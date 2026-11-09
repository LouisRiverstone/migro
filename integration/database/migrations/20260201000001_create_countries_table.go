package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateCountriesTable struct{}

func init() { migrate.Register(&CreateCountriesTable{}) }

func (CreateCountriesTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "countries", func(t *schema.Table) {
		t.Increments("id")
		t.String("code", 2).NotNull().Unique()
		t.String("name", 100).NotNull()
		t.Timestamps()
	})
}

func (CreateCountriesTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "countries")
}
