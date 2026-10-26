package codectx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetect_infersGoCommands(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module x\n")
	write(t, dir, "main.go", "package main\n")
	write(t, dir, "node_modules/pkg/junk.go", "x")
	p := Detect(dir)
	if p.Commands.Test != "go test ./..." {
		t.Fatalf("test command = %q", p.Commands.Test)
	}
	if len(p.Languages) == 0 || p.Languages[0].Name != "Go" {
		t.Fatalf("languages = %+v", p.Languages)
	}
	for _, e := range p.Tree {
		if strings.Contains(e, "node_modules") {
			t.Fatalf("ignored dir leaked into tree: %s", e)
		}
	}
	if p.PromptBlock(2000) == "" {
		t.Fatal("prompt block empty")
	}
}

func TestDetect_infersNpmScripts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"test":"vitest run","lint":"eslint ."}}`)
	p := Detect(dir)
	if p.Commands.Test != "vitest run" || p.Commands.Lint != "eslint ." {
		t.Fatalf("commands = %+v", p.Commands)
	}
}

func TestDetect_makeTargets(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Makefile", ".PHONY: test\n\ntest:\n\tgo test ./...\n")
	p := Detect(dir)
	if p.Commands.Test != "make test" {
		t.Fatalf("test command = %q", p.Commands.Test)
	}
}

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
