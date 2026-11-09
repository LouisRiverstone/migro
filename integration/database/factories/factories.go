// Package factories holds model factories (Laravel's database/factories, Lucid's factories).
package factories

import (
	"context"
	"sync/atomic"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/integration/models"
	"github.com/LouisRiverstone/migro/orm"
	"github.com/LouisRiverstone/migro/seed"
)

var countrySeq atomic.Int64

// CountryFactory builds countries. The unique two-letter code comes from a counter: random codes
// would collide in a batch.
func CountryFactory() *seed.Factory[models.Country] {
	return seed.Define(func(f *seed.Faker) models.Country {
		n := countrySeq.Add(1) + 200 // stay clear of the codes DatabaseSeeder inserts
		return models.Country{Code: string(rune('A'+n/26%26)) + string(rune('A'+n%26)), Name: f.Word()}
	})
}

// UserFactory builds users that belong to country 1 unless told otherwise. States and relations
// mirror Lucid: Apply("...") / With("posts", 2), plus For("country") to give each user a new country.
func UserFactory() *seed.Factory[models.User] {
	return seed.Define(func(f *seed.Faker) models.User {
		return models.User{CountryID: 1, Email: f.UUID() + "@example.com", Name: f.Name()}
	}).
		Parent("country", func(ctx context.Context, db *migro.DB, u *models.User, f *seed.Faker) error {
			c, err := CountryFactory().Create(ctx, db)
			if err != nil {
				return err
			}
			u.CountryID = c.ID
			return nil
		}).
		Relation("posts", func(ctx context.Context, db *migro.DB, u *models.User, n int, f *seed.Faker) error {
			orm.Bind(db, u) // the factory hands over a plain struct; binding unlocks u.Posts.Create
			for range n {
				if _, err := u.Posts.Create(ctx, map[string]any{"slug": f.UUID(), "title": "Post " + f.UUID()}); err != nil {
					return err
				}
			}
			return nil
		})
}
