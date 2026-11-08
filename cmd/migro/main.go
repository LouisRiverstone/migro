// Command migro manages SQL file migrations.
//
//	migro [-dialect d] [-dsn s] [-dir migrations] make|up|rollback|reset|status|unlock [name]
//
// MIGRO_DIALECT and MIGRO_DSN are used when the flags are omitted.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LouisRiverstone/migro"
	_ "github.com/LouisRiverstone/migro/dialect/mysql"
	_ "github.com/LouisRiverstone/migro/dialect/oracle"
	_ "github.com/LouisRiverstone/migro/dialect/postgres"
	_ "github.com/LouisRiverstone/migro/dialect/sqlite"
	_ "github.com/LouisRiverstone/migro/dialect/sqlserver"
	"github.com/LouisRiverstone/migro/migrate"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("migro", flag.ContinueOnError)
	dia := fs.String("dialect", os.Getenv("MIGRO_DIALECT"), "database dialect")
	dsn := fs.String("dsn", os.Getenv("MIGRO_DSN"), "connection string")
	dir := fs.String("dir", "migrations", "migrations directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("command required: make, up, rollback, reset, status, unlock")
	}
	cmd := fs.Arg(0)
	if cmd == "make" {
		return makeFiles(*dir, fs.Arg(1))
	}

	db, err := migro.Open(migro.Config{Dialect: *dia, DSN: *dsn})
	if err != nil {
		return err
	}
	defer db.Close()
	m := migrate.New(db)
	ms, err := migrate.FromFS(os.DirFS(*dir), ".")
	if err != nil {
		return err
	}
	if err := m.Add(ms...); err != nil {
		return err
	}
	ctx := context.Background()
	switch cmd {
	case "up":
		res, err := m.Latest(ctx)
		return report("applied", res, err)
	case "rollback":
		res, err := m.Rollback(ctx)
		return report("reverted", res, err)
	case "reset":
		res, err := m.Reset(ctx)
		return report("reverted", res, err)
	case "unlock":
		return m.Unlock(ctx)
	case "status":
		st, err := m.Status(ctx)
		if err != nil {
			return err
		}
		for _, s := range st {
			mark := "pending"
			if s.Applied {
				mark = fmt.Sprintf("batch %d", s.Batch)
			}
			fmt.Printf("%-50s %s\n", s.Name, mark)
		}
		return nil
	}
	return fmt.Errorf("unknown command %q", cmd)
}

func report(verb string, res migrate.Result, err error) error {
	if err != nil {
		return err
	}
	if len(res.Names) == 0 {
		fmt.Println("nothing to do")
		return nil
	}
	for _, n := range res.Names {
		fmt.Printf("%s %s\n", verb, n)
	}
	return nil
}

func makeFiles(dir, name string) error {
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return '_'
	}, name)
	if name == "" {
		return fmt.Errorf("usage: migro make <name>")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	base := time.Now().UTC().Format("20060102150405") + "_" + name
	for _, suffix := range []string{".up.sql", ".down.sql"} {
		p := filepath.Join(dir, base+suffix)
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			return err
		}
		fmt.Println("created", p)
	}
	return nil
}
