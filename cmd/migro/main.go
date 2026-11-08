// Command migro is the artisan/ace-style CLI.
//
//	migro make:migration create_users_table
//	migro migration:run
//	migro migration:rollback --step=2
//	migro migration:status
//	migro db:seed
//
// File generators (make:*) run directly. Everything that touches the database compiles a tiny
// runner inside your module so your own migrations, seeders and models are linked in, then runs it
// with `go run`. Configure the database with MIGRO_DIALECT / MIGRO_DSN in the environment or .env.
// Run `migro help` for the full command list.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/LouisRiverstone/migro/console"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR ", err)
		return console.ExitError
	}
	root, rootErr := findRoot(cwd)
	if rootErr != nil {
		root = cwd // outside a module only help works
	}
	app, err := console.FromEnv(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR ", err)
		return console.ExitError
	}
	if len(args) == 0 || !needsRunner(args[0]) {
		return app.Run(context.Background(), args)
	}
	if rootErr != nil {
		fmt.Fprintln(os.Stderr, "ERROR  run migro inside a Go module (no go.mod found in this directory or its parents)")
		return console.ExitError
	}
	return runInModule(app, root, args)
}

// needsRunner reports whether a command must run inside the compiled project.
func needsRunner(cmd string) bool {
	switch {
	case cmd == "help" || cmd == "list" || cmd == "--help" || cmd == "-h":
		return false
	case strings.HasPrefix(cmd, "make:"):
		return false
	}
	return true
}
