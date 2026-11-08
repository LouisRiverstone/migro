package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderMain(t *testing.T) {
	src := renderMain(runnerSpec{
		Dialects: []string{"github.com/LouisRiverstone/migro/dialect/postgres"},
		Blank:    []string{"example.com/shop/database/migrations"},
	})
	for _, want := range []string{`_ "github.com/LouisRiverstone/migro/dialect/postgres"`, `_ "example.com/shop/database/migrations"`, "console.FromEnv", "app.Main(os.Args[1:])"} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q:\n%s", want, src)
		}
	}
	if strings.Contains(src, "app.Models") {
		t.Error("models must only be wired for migration:diff")
	}
	withModels := renderMain(runnerSpec{ModelsPkg: "example.com/shop/app/models", ModelsName: "models", Models: []string{"User", "Post"}})
	if !strings.Contains(withModels, "&models.User{}") || !strings.Contains(withModels, `models "example.com/shop/app/models"`) {
		t.Errorf("models wiring:\n%s", withModels)
	}
}

func TestDialectImports(t *testing.T) {
	got, err := dialectImports([]string{"MIGRO_DIALECT=pg", "MIGRO_ANALYTICS_DIALECT=mariadb", "MIGRO_DSN=x", "MIGRO_OTHER_DIALECT="})
	if err != nil || len(got) != 2 || !strings.HasSuffix(got[0], "/mysql") || !strings.HasSuffix(got[1], "/postgres") {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := dialectImports([]string{"MIGRO_DIALECT=oracle9000"}); err == nil {
		t.Error("unknown dialect should fail")
	}
	if _, err := dialectImports([]string{"HOME=/x"}); err == nil || !strings.Contains(err.Error(), "MIGRO_DIALECT") {
		t.Errorf("no dialect should explain what to set: %v", err)
	}
}

func TestDiscoverModels(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package models\n\ntype User struct{ ID uint }\ntype Post struct{ ID uint }\ntype helper struct{ X int }\ntype Empty struct{}\ntype Alias = User\n")
	write("b.go", "package models\n\n// Cache is not a table.\n// migro:ignore\ntype Cache struct{ K string }\n")
	write("a_test.go", "package models\n\ntype FromTest struct{ ID uint }\n")
	pkg, names, err := discoverModels(dir)
	if err != nil || pkg != "models" || strings.Join(names, ",") != "Post,User" {
		t.Fatalf("pkg=%s names=%v err=%v", pkg, names, err)
	}
}

func TestFindRootAndNeedsRunner(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/x\n"), 0o644)
	sub := filepath.Join(root, "a", "b")
	_ = os.MkdirAll(sub, 0o755)
	if got, err := findRoot(sub); err != nil || got != root {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := findRoot(t.TempDir()); err == nil {
		t.Error("no go.mod should fail")
	}
	for cmd, want := range map[string]bool{"make:migration": false, "help": false, "migration:run": true, "db:seed": true, "migrate": true} {
		if needsRunner(cmd) != want {
			t.Errorf("needsRunner(%q) != %v", cmd, want)
		}
	}
}
