package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobtgzhang/clawmind/backend/internal/codectx"
	"github.com/mobtgzhang/clawmind/backend/internal/harness"
)

func TestFileEdit_rejectsAmbiguousMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("x\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &ToolRunner{Workspace: dir}
	args, _ := json.Marshal(map[string]any{"path": "a.go", "old": "x", "new": "y"})
	if _, err := r.Run(context.Background(), "file_edit", string(args), RunConfig{}); err == nil {
		t.Fatal("ambiguous match should error instead of guessing")
	}
	args, _ = json.Marshal(map[string]any{"path": "a.go", "old": "x", "new": "y", "replace_all": true})
	if _, err := r.Run(context.Background(), "file_edit", string(args), RunConfig{}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "a.go"))
	if !strings.Contains(string(b), "y\ny") {
		t.Fatalf("file = %q", string(b))
	}
}

func TestCodeSearchSkipsDependencyDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "junk.go"), []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("needle here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &ToolRunner{Workspace: dir}
	out, err := r.Run(context.Background(), "code_search", `{"query":"needle"}`, RunConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "main.go:1") {
		t.Fatalf("out = %q", out)
	}
	if strings.Contains(out, "node_modules") {
		t.Fatalf("dependency dirs should be skipped: %q", out)
	}
}

func TestFileEditRejectsPathsOutsideWorkspace(t *testing.T) {
	dir := t.TempDir()
	r := &ToolRunner{Workspace: dir}
	if _, err := r.Run(context.Background(), "file_edit", `{"path":"../outside.go","old":"x","new":"y"}`, RunConfig{}); err == nil {
		t.Fatal("path escaping the workspace should error")
	}
}

func TestLoadSkillReturnsBody(t *testing.T) {
	r := &ToolRunner{Workspace: t.TempDir(), Harness: harness.Tree{
		"skills/grep_calls/SKILL.md": "---\nname: grep_calls\ndescription: 改动公共 API 前\n---\n先搜索调用点。\n",
	}}
	out, err := r.Run(context.Background(), "load_skill", `{"name":"grep_calls"}`, RunConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "先搜索调用点") {
		t.Fatalf("out = %q", out)
	}
	if _, err := r.Run(context.Background(), "load_skill", `{"name":"missing"}`, RunConfig{}); err == nil {
		t.Fatal("unknown skill should error")
	}
}

func TestRunTestsNeedsACommand(t *testing.T) {
	r := &ToolRunner{Workspace: t.TempDir()}
	if _, err := r.Run(context.Background(), "run_tests", "{}", RunConfig{}); err == nil {
		t.Fatal("expected an error when no command is given and none is inferred")
	}
	out, err := r.Run(context.Background(), "run_tests", "{}", RunConfig{Repo: &codectx.Profile{Commands: codectx.Commands{Test: "false"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "exit non-zero") {
		t.Fatalf("a failing verification command should be reported, not hidden: %q", out)
	}
}
