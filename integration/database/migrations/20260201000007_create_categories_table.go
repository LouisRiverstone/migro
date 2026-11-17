package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type CreateCategoriesTable struct{}

func init() { migrate.Register(&CreateCategoriesTable{}) }

func (CreateCategoriesTable) Up(ctx context.Context, s *schema.Builder) error {
	return s.CreateTable(ctx, "categories", func(t *schema.Table) {
		// self-referencing tree
		t.Increments("id")
		t.Integer("parent_id").Unsigned().Nullable()
		t.String("name").NotNull()
		onDelete := "CASCADE"
		if s.DB().Dialect.Name() == "sqlserver" {
			onDelete = "NO ACTION" // SQL Server cannot cascade through a self-referencing key
		}
		t.Foreign("parent_id").References("categories", "id").OnDelete(onDelete).Named("categories_parent_fk")
		t.Timestamps()
	})
}

func (CreateCategoriesTable) Down(ctx context.Context, s *schema.Builder) error {
	return s.DropTableIfExists(ctx, "categories")
}
