// Package migrate runs versioned schema migrations in batches, Knex-style.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/LouisRiverstone/migro"
	"github.com/LouisRiverstone/migro/schema"
)

// Func is one direction of a migration. It receives a schema builder bound to
// the migration's transaction (when the dialect supports transactional DDL).
type Func func(ctx context.Context, s *schema.Builder) error

// Migration is a named, ordered unit of change. Names sort lexicographically
// (use a timestamp prefix such as 20240101120000_create_users).
type Migration struct {
	Name string
	Up   Func
	Down Func
	// DisableTransaction runs the migration without a wrapping transaction.
	DisableTransaction bool
	// Checksum is optional; when set, edits to an applied migration are detected.
	Checksum string
	// Connection pins the migration to a named connection; "" means the default one.
	Connection string
}

// FromFS loads "<name>.up.sql" / "<name>.down.sql" pairs from dir in fsys.
// Statements are split on ";" (outside quotes and -- comments); stored procedures
// with inner semicolons need a Go migration instead.
func FromFS(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, migro.Wrap(migro.MigrationFailed, err, "read migrations dir %q", dir)
	}
	type pair struct{ up, down string }
	pairs := map[string]*pair{}
	for _, e := range entries {
		n := e.Name()
		var base string
		var isUp bool
		switch {
		case strings.HasSuffix(n, ".up.sql"):
			base, isUp = strings.TrimSuffix(n, ".up.sql"), true
		case strings.HasSuffix(n, ".down.sql"):
			base = strings.TrimSuffix(n, ".down.sql")
		default:
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, n))
		if err != nil {
			return nil, migro.Wrap(migro.MigrationFailed, err, "read %s", n)
		}
		p := pairs[base]
		if p == nil {
			p = &pair{}
			pairs[base] = p
		}
		if isUp {
			p.up = string(b)
		} else {
			p.down = string(b)
		}
	}
	names := make([]string, 0, len(pairs))
	for n := range pairs {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Migration, 0, len(names))
	for _, n := range names {
		p := pairs[n]
		if strings.TrimSpace(p.up) == "" {
			return nil, migro.Newf(migro.MigrationFailed, "migration %q has no up.sql", n)
		}
		sum := sha256.Sum256([]byte(p.up))
		m := Migration{Name: n, Up: sqlFunc(p.up), Checksum: hex.EncodeToString(sum[:])}
		if strings.TrimSpace(p.down) != "" {
			m.Down = sqlFunc(p.down)
		}
		out = append(out, m)
	}
	return out, nil
}

func sqlFunc(src string) Func {
	stmts := SplitStatements(src)
	return func(ctx context.Context, s *schema.Builder) error {
		for _, st := range stmts {
			if err := s.Raw(ctx, st); err != nil {
				return err
			}
		}
		return nil
	}
}

// SplitStatements splits SQL on semicolons, ignoring those in quotes and -- comments.
func SplitStatements(src string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	rs := []rune(src)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote != 0:
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"' || r == '`':
			quote = r
			cur.WriteRune(r)
		case r == '-' && i+1 < len(rs) && rs[i+1] == '-':
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
			cur.WriteRune('\n')
		case r == ';':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func validate(ms []Migration) error {
	seen := map[string]bool{}
	for _, m := range ms {
		if m.Name == "" || m.Up == nil {
			return migro.Newf(migro.MigrationFailed, "migration %q needs a name and an Up func", m.Name)
		}
		if seen[m.Name] {
			return migro.Newf(migro.MigrationFailed, "duplicate migration name %q", m.Name)
		}
		seen[m.Name] = true
	}
	return nil
}

func (m Migration) String() string { return fmt.Sprintf("migration(%s)", m.Name) }
