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
	"runtime/debug"
	"slices"
	"strings"

	"github.com/LouisRiverstone/migro/console"
)

// version is set at release time (-ldflags "-X main.version=v1.2.3"); `go install ...@vX` fills it from build info.
var version = ""

func main() { os.Exit(run(os.Args[1:])) }

// resolveVersion prefers the release-time value, then the module version Go embeds, then "dev".
func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func run(args []string) int {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-v" || args[0] == "version") {
		fmt.Println("migro " + resolveVersion())
		return console.ExitOK
	}
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
	if len(args) == 0 || !needsRunner(args) {
		return app.Run(context.Background(), args)
	}
	if rootErr != nil {
		fmt.Fprintln(os.Stderr, "ERROR  run migro inside a Go module (no go.mod found in this directory or its parents)")
		return console.ExitError
	}
	return runInModule(app, root, args)
}

// needsRunner reports whether a command must run inside the compiled project.
func needsRunner(args []string) bool {
	cmd := args[0]
	switch {
	case cmd == "make:model" && slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "--from-table") }):
		return true // reads the live schema
	case cmd == "help" || cmd == "list" || cmd == "--help" || cmd == "-h":
		return false
	case strings.HasPrefix(cmd, "make:"):
		return false
	}
	return true
}
