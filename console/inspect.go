package console

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/migrate"
	"github.com/LouisRiverstone/migro/orm"
)

// ---- schema:dump --------------------------------------------------------------------------

func (c *call) dumpPath(db *migro.DB) string {
	return filepath.Join(c.dir("path", c.app.SchemaDir), db.Dialect.Name()+"-schema.sql")
}

// loadDump returns the schema dump for db's dialect when one exists on disk.
func (c *call) loadDump(db *migro.DB) (*migrate.Dump, error) {
	b, err := os.ReadFile(c.dumpPath(db))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, migro.Wrap(migro.InvalidConfig, err, "read schema dump")
	}
	d, err := migrate.ParseDump(string(b))
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func dumpSchema(c *call) error {
	return c.withMigrator(true, func(m *migrate.Migrator, db *migro.DB) error {
		d, err := m.Dump(c.ctx)
		if err != nil {
			return err
		}
		path := c.dumpPath(db)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return migro.Wrap(migro.InvalidConfig, err, "create %s", filepath.Dir(path))
		}
		if err := os.WriteFile(path, []byte(d.String()), 0o644); err != nil {
			return migro.Wrap(migro.InvalidConfig, err, "write %s", path)
		}
		c.printf("Schema dumped: %s (%d migrations, %d statements)\n", relTo(c.app.Root, path), len(d.Migrations), len(d.Statements))
		return nil
	})
}

// ---- db:* ---------------------------------------------------------------------------------

func wipeDatabase(c *call) error {
	if err := c.guard(); err != nil {
		return err
	}
	db, _, err := c.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := migrate.DropAllTables(c.ctx, db); err != nil {
		return err
	}
	c.printf("Dropped all tables.\n")
	return nil
}

func showDatabase(c *call) error {
	db, conn, err := c.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	tables, err := db.Gorm.Migrator().GetTables()
	if err != nil {
		return migro.Wrap(migro.QueryFailed, err, "list tables")
	}
	sort.Strings(tables)
	c.printf("Connection: %s\nDialect:    %s\nTables:     %d\n", conn, db.Dialect.Name(), len(tables))
	for _, t := range tables {
		if !c.flags.isTrue("counts") {
			c.printf("  %s\n", t)
			continue
		}
		var n int64
		if err := db.Gorm.Table(t).Count(&n).Error; err != nil {
			return migro.Wrap(migro.QueryFailed, err, "count %s", t)
		}
		c.printf("  %-40s %d rows\n", t, n)
	}
	return nil
}

func showTable(c *call) error {
	if len(c.args) != 1 {
		return usagef("exactly one table name is required")
	}
	db, _, err := c.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	table := c.args[0]
	mg := db.Gorm.Migrator()
	if !mg.HasTable(table) {
		return migro.Newf(migro.NotFound, "table %q does not exist", table)
	}
	cols, err := mg.ColumnTypes(table)
	if err != nil {
		return migro.Wrap(migro.QueryFailed, err, "read columns of %s", table)
	}
	c.printf("Table: %s\n\n%-30s %-20s %s\n", table, "Column", "Type", "Flags")
	for _, col := range cols {
		var flags []string
		if pk, ok := col.PrimaryKey(); ok && pk {
			flags = append(flags, "primary")
		}
		if null, ok := col.Nullable(); ok && !null {
			flags = append(flags, "not null")
		}
		if u, ok := col.Unique(); ok && u {
			flags = append(flags, "unique")
		}
		c.printf("%-30s %-20s %s\n", col.Name(), col.DatabaseTypeName(), strings.Join(flags, ", "))
	}
	idx, err := mg.GetIndexes(table)
	if err != nil || len(idx) == 0 {
		return nil // index listing is not available on every driver
	}
	c.printf("\nIndexes:\n")
	for _, i := range idx {
		kind := "index"
		if u, ok := i.Unique(); ok && u {
			kind = "unique"
		}
		c.printf("  %-40s %-7s (%s)\n", i.Name(), kind, strings.Join(i.Columns(), ", "))
	}
	return nil
}

// ---- model:* ------------------------------------------------------------------------------

func showModel(c *call) error {
	if len(c.args) != 1 {
		return usagef("exactly one model name is required")
	}
	var model any
	var known []string
	for _, m := range c.app.Models {
		t := reflect.TypeOf(m)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		known = append(known, t.Name())
		if strings.EqualFold(t.Name(), c.args[0]) {
			model = m
		}
	}
	if model == nil {
		return migro.Newf(migro.NotFound, "no model named %q (have: %s); set App.Models or run through the launcher", c.args[0], strings.Join(known, ", "))
	}
	db, _, err := c.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	info, err := orm.Describe(db, model)
	if err != nil {
		return err
	}
	c.printf("Model: %s\nTable: %s\nSoft deletes: %v\n", info.Name, info.Table, info.SoftDeletes)
	if len(info.Traits) > 0 {
		c.printf("Behaviours: %s\n", strings.Join(info.Traits, ", "))
	}
	c.printf("\n%-24s %-22s %s\n", "Column", "Go type", "Flags")
	for _, col := range info.Columns {
		var flags []string
		for _, f := range []struct {
			on   bool
			name string
		}{{col.PrimaryKey, "primary"}, {col.AutoIncrement, "auto"}, {col.NotNull, "not null"}, {col.Unique, "unique"}, {col.HasDefault, "default"}} {
			if f.on {
				flags = append(flags, f.name)
			}
		}
		c.printf("%-24s %-22s %s\n", col.Name, col.GoType, strings.Join(flags, ", "))
	}
	if len(info.Relations) > 0 {
		c.printf("\n%-20s %-18s %-16s %s\n", "Relation", "Kind", "Related", "Keys")
		for _, r := range info.Relations {
			c.printf("%-20s %-18s %-16s %s\n", r.Name, r.Kind, r.Related, r.Keys)
		}
	}
	return nil
}

func pruneModels(c *call) error {
	if err := c.guard(); err != nil {
		return err
	}
	db, _, err := c.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	counts, err := orm.PruneAll(c.ctx, db)
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c.printf("%-30s %d pruned\n", n, counts[n])
	}
	if err == nil && len(names) == 0 {
		c.printf("No prunable models registered. Call orm.RegisterPrunable in init().\n")
	}
	return err
}
