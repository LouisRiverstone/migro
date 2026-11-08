package console

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/LouisRiverstone/migro"
)

// LoadDotEnv reads KEY=VALUE lines from path into the process environment.
// Variables that are already set win. A missing file is not an error.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		if _, set := os.LookupEnv(k); !set {
			_ = os.Setenv(k, v)
		}
	}
	return sc.Err()
}

var namedConn = regexp.MustCompile(`^MIGRO_([A-Z0-9_]+)_DIALECT$`)

// ConnectionsFromEnv builds the connection table from the environment:
//
//	MIGRO_DIALECT / MIGRO_DSN                  the default connection
//	MIGRO_<NAME>_DIALECT / MIGRO_<NAME>_DSN    a named one, used as --database=<name> (lower-cased)
func ConnectionsFromEnv() map[string]Connection {
	conns := map[string]Connection{}
	add := func(name, dialectVar, dsnVar string) {
		d, dsn := os.Getenv(dialectVar), os.Getenv(dsnVar)
		conns[name] = func(ctx context.Context) (*migro.DB, error) {
			if d == "" || dsn == "" {
				return nil, migro.Newf(migro.InvalidConfig, "set %s and %s (in the environment or .env)", dialectVar, dsnVar)
			}
			return migro.OpenContext(ctx, migro.Config{Dialect: d, DSN: dsn})
		}
	}
	add("default", "MIGRO_DIALECT", "MIGRO_DSN")
	var names []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if m := namedConn.FindStringSubmatch(k); m != nil {
			names = append(names, m[1])
		}
	}
	sort.Strings(names)
	for _, n := range names {
		add(strings.ToLower(n), "MIGRO_"+n+"_DIALECT", "MIGRO_"+n+"_DSN")
	}
	return conns
}

// FromEnv builds an App for root from .env and the environment. Directories can be overridden with
// MIGRO_MIGRATIONS_DIR, MIGRO_SEEDERS_DIR, MIGRO_FACTORIES_DIR and MIGRO_MODELS_DIR.
func FromEnv(root string) (*App, error) {
	if err := LoadDotEnv(filepath.Join(root, ".env")); err != nil {
		return nil, migro.Wrap(migro.InvalidConfig, err, "read .env")
	}
	interactive := stdinIsTerminal()
	return &App{
		Root:          root,
		Connections:   ConnectionsFromEnv(),
		MigrationsDir: os.Getenv("MIGRO_MIGRATIONS_DIR"),
		SeedersDir:    os.Getenv("MIGRO_SEEDERS_DIR"),
		FactoriesDir:  os.Getenv("MIGRO_FACTORIES_DIR"),
		ModelsDir:     os.Getenv("MIGRO_MODELS_DIR"),
		Interactive:   interactive,
	}, nil
}

// stdinIsTerminal is true for a real terminal; /dev/null is a character device too but cannot answer a prompt.
func stdinIsTerminal() bool {
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(st, null)
}
