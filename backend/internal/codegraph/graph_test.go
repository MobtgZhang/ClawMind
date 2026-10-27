package codegraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestIndexFindsDefinitionsAndCallers(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "store/store.go", `package store

import "fmt"

func Helper() int { return 1 }

func Load() int {
	return Helper()
}
`)
	write(t, dir, "app/app.go", `package app

import "demo/internal/store"

func Run() {
	fmt.Println(store.Load())
}
`)

	ix := New(dir, DefaultLimits())
	parsed, _, _ := ix.Refresh(dir)
	if parsed != 2 {
		t.Fatalf("parsed = %d, want 2", parsed)
	}

	shape := ix.Shape()
	if shape.Symbols < 3 {
		t.Fatalf("symbols = %d, want >= 3", shape.Symbols)
	}
	if shape.Modules < 2 {
		t.Fatalf("modules = %d, want >= 2", shape.Modules)
	}

	hits := ix.Callers("Load", 2, 20)
	if len(hits) == 0 {
		t.Fatal("Load should have an inbound reference from app.Run")
	}
	if hits[0].Symbol.File != "app/app.go" {
		t.Fatalf("caller = %s, want app/app.go", hits[0].Symbol.File)
	}

	if out := ix.Render("stats", "", 2, 20); !strings.Contains(out, "symbols") {
		t.Fatalf("stats render = %q", out)
	}
	if out := ix.PromptBlock(1000); !strings.Contains(out, "代码图") {
		t.Fatalf("prompt block = %q", out)
	}
}

// An unqualified call carries no package, so the resolver has to look at the file it was written in
// rather than search for an import that is not there. Same-file references are the most common edges
// in a package, and losing them makes the graph report a symbol nobody uses.
func TestIndexResolvesSameFileReferences(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "store/store.go", `package store

func Helper() int { return 1 }

func Load() int {
	return Helper()
}
`)
	ix := New(dir, DefaultLimits())
	ix.Refresh(dir)
	hits := ix.Callers("Helper", 2, 20)
	if len(hits) == 0 {
		t.Fatal("same-file reference lost: an unqualified call must resolve inside its own file")
	}
	if hits[0].Symbol.Name != "Load" {
		t.Fatalf("caller = %s, want Load", hits[0].Symbol.Name)
	}
}

func TestIndexReusesUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.go", "package a\n\nfunc A() { B() }\n\nfunc B() {}\n")
	ix := New(dir, DefaultLimits())
	parsed, reused, _ := ix.Refresh(dir)
	if parsed != 1 || reused != 0 {
		t.Fatalf("first refresh parsed=%d reused=%d", parsed, reused)
	}
	parsed, reused, _ = ix.Refresh(dir)
	if parsed != 0 || reused != 1 {
		t.Fatalf("second refresh parsed=%d reused=%d, want 0/1", parsed, reused)
	}

	write(t, dir, "a.go", "package a\n\nfunc A() { B() }\n\nfunc B() {}\n\nfunc C() {}\n")
	parsed, reused, _ = ix.Refresh(dir)
	if parsed != 1 || reused != 0 {
		t.Fatalf("changed file parsed=%d reused=%d, want 1/0", parsed, reused)
	}
	if len(ix.Lookup("C", 10)) == 0 {
		t.Fatal("C should be found after the change")
	}
}

func TestIndexDetectsImportCycle(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "x/x.go", "package x\n\nimport \"demo/y\"\n\nfunc X() { y.Y() }\n")
	write(t, dir, "y/y.go", "package y\n\nimport \"demo/x\"\n\nfunc Y() { x.X() }\n")
	ix := New(dir, DefaultLimits())
	ix.Refresh(dir)
	cycles := ix.PackageCycles(10)
	if len(cycles) == 0 {
		t.Fatal("x → y → x should be reported as a cycle")
	}
	if ix.Shape().Cycles == 0 {
		t.Fatal("shape should carry the cycle count")
	}
}

func TestIndexSkipsDependencyDirs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "node_modules/pkg/junk.go", "package pkg\n\nfunc Junk() {}\n")
	write(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	ix := New(dir, DefaultLimits())
	ix.Refresh(dir)
	if len(ix.Lookup("Junk", 10)) != 0 {
		t.Fatal("dependency dirs must be skipped")
	}
	if len(ix.Lookup("main", 10)) == 0 {
		t.Fatal("main should be indexed")
	}
}
