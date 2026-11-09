package seeders

import (
	"context"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/integration/models"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/seed"
)

type UserSeeder struct{}

func init() { seed.Register(&UserSeeder{}) }

// Run is idempotent (Lucid's updateOrCreateMany): running it twice leaves three users.
func (UserSeeder) Run(ctx context.Context, db *migro.DB) error {
	users, err := orm.New[models.User](db).UpdateOrCreateMany(ctx, []string{"email"}, []models.User{
		{CountryID: 1, Email: "ana@example.com", Name: "Ana"},
		{CountryID: 1, Email: "bia@example.com", Name: "Bia"},
		{CountryID: 2, Email: "caio@example.com", Name: "Caio"},
	})
	if err != nil {
		return err
	}
	ana, bia, caio := &users[0], &users[1], &users[2]

	if _, err := ana.Profile.Create(ctx, map[string]any{"bio": "Writes; a lot"}); err != nil {
		return err
	}
	if _, err := bia.Profile.Create(ctx, map[string]any{}); err != nil {
		return err
	}
	for user, roles := range map[*models.User][]any{ana: {1, 2}, bia: {2}, caio: {3}} {
		if err := user.Roles.Attach(ctx, roles...); err != nil {
			return err
		}
	}
	return nil
}
