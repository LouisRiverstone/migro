package orm

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/LouisRiverstone/migro"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gschema "gorm.io/gorm/schema"
)

// Upsert inserts rows, and for a row whose uniqueBy columns already exist updates the update columns
// (all columns except the keys and created_at when update is empty), in one statement
// (Eloquent's upsert). uniqueBy needs a unique index. MySQL ignores uniqueBy and uses whichever
// unique key conflicts. SQL Server and Oracle run a MERGE; Redshift has no upsert and is refused.
func (m *Model[T]) Upsert(ctx context.Context, rows []T, uniqueBy []string, update []string) error {
	if len(rows) == 0 {
		return nil
	}
	if len(uniqueBy) == 0 {
		return migro.Newf(migro.InvalidConfig, "upsert needs at least one uniqueBy column")
	}
	if len(update) == 0 {
		sch, err := parseModel(m.db, new(T))
		if err != nil {
			return err
		}
		for _, f := range sch.Fields {
			if f.DBName != "" && f.Creatable && !f.PrimaryKey && f.AutoCreateTime == 0 && !slices.Contains(uniqueBy, f.DBName) {
				update = append(update, f.DBName)
			}
		}
	}
	switch m.db.Dialect.Name() {
	case "sqlserver", "oracle":
		return m.merge(ctx, rows, uniqueBy, update)
	case "redshift":
		return migro.Newf(migro.UnsupportedFeature, "Upsert is not supported on redshift")
	}
	cols := make([]clause.Column, len(uniqueBy))
	for i, c := range uniqueBy {
		cols[i] = clause.Column{Name: c}
	}
	oc := clause.OnConflict{Columns: cols, DoUpdates: clause.AssignmentColumns(update)}
	if len(update) == 0 {
		oc = clause.OnConflict{Columns: cols, DoNothing: true}
	}
	if err := wrap(m.db.Gorm.WithContext(ctx).Clauses(oc).Create(&rows).Error, "upsert"); err != nil {
		return err
	}
	return nil
}

// InsertOrIgnore inserts rows and silently skips the ones that violate a unique key.
func (m *Model[T]) InsertOrIgnore(ctx context.Context, rows []T) error {
	if len(rows) == 0 {
		return nil
	}
	switch m.db.Dialect.Name() {
	case "sqlserver", "oracle", "redshift":
		// GORM would drop the clause and run a plain INSERT, which fails on the first duplicate.
		return migro.Newf(migro.UnsupportedFeature, "InsertOrIgnore is not supported on %s; use Upsert with the unique columns", m.db.Dialect.Name())
	}
	return wrap(m.db.Gorm.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error, "insert or ignore")
}

// FirstOrNew finds a row matching the non-zero fields of where, or returns an unsaved one built from
// where and attrs. Returns whether the row exists.
func (m *Model[T]) FirstOrNew(ctx context.Context, where T, attrs T) (*T, bool, error) {
	out := where
	res := m.db.Gorm.WithContext(ctx).Where(&where).Attrs(&attrs).FirstOrInit(&out)
	if res.Error != nil {
		return nil, false, wrap(res.Error, "first or new")
	}
	exists := res.RowsAffected > 0
	bind(m.db, &out, exists)
	return &out, exists, nil
}

// merge is Upsert for databases without ON CONFLICT: one MERGE per batch of rows.
func (m *Model[T]) merge(ctx context.Context, rows []T, uniqueBy, update []string) error {
	sch, err := parseModel(m.db, new(T))
	if err != nil {
		return err
	}
	for _, c := range uniqueBy {
		if _, ok := sch.FieldsByDBName[c]; !ok {
			return migro.Newf(migro.InvalidSchema, "model %s has no column %q", sch.Name, c)
		}
	}
	for i := range rows { // BeforeCreate hooks fill keys such as a UUID, as Create would
		if h, ok := any(&rows[i]).(interface{ BeforeCreate(*gorm.DB) error }); ok {
			if err := h.BeforeCreate(m.db.Gorm.WithContext(ctx)); err != nil {
				return wrap(err, "upsert")
			}
		}
	}
	now := time.Now()
	var fields []*gschema.Field
	for _, f := range sch.Fields {
		if f.DBName == "" || !f.Creatable {
			continue
		}
		omit := true // an auto-increment key or a column left to its database default, in every row
		for i := range rows {
			_, zero := f.ValueOf(ctx, reflect.ValueOf(&rows[i]).Elem())
			if f.AutoCreateTime > 0 || f.AutoUpdateTime > 0 || !zero || !f.AutoIncrement && !f.HasDefaultValue {
				omit = false
			}
		}
		if !omit {
			fields = append(fields, f)
		}
	}
	q := m.db.Dialect.Quote
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = q(f.DBName)
	}
	inSource := func(c string) bool {
		return slices.ContainsFunc(fields, func(f *gschema.Field) bool { return f.DBName == c })
	}
	on := make([]string, len(uniqueBy))
	for i, c := range uniqueBy {
		if !inSource(c) {
			return migro.Newf(migro.InvalidConfig, "upsert: uniqueBy column %q has no value in any row", c)
		}
		on[i] = "t." + q(c) + " = s." + q(c)
	}
	set := make([]string, 0, len(update))
	for _, c := range update {
		if !inSource(c) { // left to its database default, so there is nothing to copy
			continue
		}
		set = append(set, "t."+q(c)+" = s."+q(c))
	}
	ins := make([]string, len(names))
	for i, n := range names {
		ins[i] = "s." + n
	}
	oracle := m.db.Dialect.Name() == "oracle"
	batch := max(1, 2000/max(1, len(fields))) // SQL Server allows 2100 parameters per statement
	for start := 0; start < len(rows); start += batch {
		end := min(start+batch, len(rows))
		var args []any
		var src []string
		for i := start; i < end; i++ {
			rv := reflect.ValueOf(&rows[i]).Elem()
			marks := make([]string, len(fields))
			for j, f := range fields {
				v, zero := f.ValueOf(ctx, rv)
				if zero && f.AutoCreateTime > 0 || zero && f.AutoUpdateTime > 0 {
					v = now
				}
				args = append(args, v)
				marks[j] = "?"
				if oracle {
					marks[j] = "? AS " + names[j]
				}
			}
			if oracle {
				src = append(src, "SELECT "+strings.Join(marks, ", ")+" FROM dual")
			} else {
				src = append(src, "("+strings.Join(marks, ", ")+")")
			}
		}
		var sb strings.Builder
		sb.WriteString("MERGE INTO " + q(sch.Table))
		if oracle {
			sb.WriteString(" t USING (" + strings.Join(src, " UNION ALL ") + ") s ON (" + strings.Join(on, " AND ") + ")")
		} else {
			sb.WriteString(" AS t USING (VALUES " + strings.Join(src, ", ") + ") AS s (" + strings.Join(names, ", ") + ") ON " + strings.Join(on, " AND "))
		}
		if len(set) > 0 {
			sb.WriteString(" WHEN MATCHED THEN UPDATE SET " + strings.Join(set, ", "))
		}
		sb.WriteString(" WHEN NOT MATCHED THEN INSERT (" + strings.Join(names, ", ") + ") VALUES (" + strings.Join(ins, ", ") + ")")
		if !oracle {
			sb.WriteString(";") // SQL Server requires the terminator
		}
		if err := wrap(m.db.Gorm.WithContext(ctx).Exec(sb.String(), args...).Error, "upsert"); err != nil {
			return err
		}
	}
	return nil
}
