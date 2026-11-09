package seed

import (
	"context"
	"reflect"

	"github.com/LouisRiverstone/migro"
)

// Call runs seeders in the order given, stopping at the first error (Laravel's $this->call([...])).
// Use it from a DatabaseSeeder so dependent tables are filled in foreign-key order; the calls share
// the caller's transaction and context, so a failure rolls the whole DatabaseSeeder back.
func Call(ctx context.Context, db *migro.DB, classes ...Class) error {
	for _, c := range classes {
		if err := c.Run(ctx, db); err != nil {
			t := reflect.TypeOf(c)
			for t.Kind() == reflect.Pointer {
				t = t.Elem()
			}
			return migro.Wrap(migro.SeedFailed, err, "seeder %s", t.Name())
		}
	}
	return nil
}
