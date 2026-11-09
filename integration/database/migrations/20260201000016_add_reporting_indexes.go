package migrations

import (
	"context"

	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/schema"
)

type AddReportingIndexes struct{}

func init() { migrate.Register(&AddReportingIndexes{}) }

func (AddReportingIndexes) Up(ctx context.Context, s *schema.Builder) error {
	if err := s.AlterTable(ctx, "comments", func(t *schema.Table) { t.Index("created_at") }); err != nil {
		return err
	}
	return s.AlterTable(ctx, "orders", func(t *schema.Table) { t.Index("status", "placed_at") })
}

func (AddReportingIndexes) Down(ctx context.Context, s *schema.Builder) error {
	if err := s.AlterTable(ctx, "orders", func(t *schema.Table) { t.DropIndex("orders_status_placed_at_index") }); err != nil {
		return err
	}
	return s.AlterTable(ctx, "comments", func(t *schema.Table) { t.DropIndex("comments_created_at_index") })
}
