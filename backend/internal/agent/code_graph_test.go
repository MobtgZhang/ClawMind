package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobtgzhang/clawmind/backend/internal/codegraph"
)

// A missing index has to be reported as missing. Falling back to a text search would answer a
// different question with the same confidence, and the model cannot tell the two apart.
func TestCodeGraphReportsAMissingIndex(t *testing.T) {
	r := &ToolRunner{Workspace: t.TempDir()}
	args, _ := json.Marshal(map[string]any{"mode": "callers", "query": "Helper"})
	if _, err := r.Run(context.Background(), "code_graph", string(args), RunConfig{}); err == nil {
		t.Fatal("a missing index must be reported instead of guessed at")
	}
}

// The graph is only worth having if it answers after a change: an index that still describes the
// pre-edit bytes reports a dependency the code no longer has.
func TestCodeGraphReflectsAnEdit(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "store", "store.go"), []byte("package store\n\nfunc Helper() int { return 1 }\n\nfunc Load() int {\n\treturn Helper()\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ix := codegraph.New(dir, codegraph.DefaultLimits())
	ix.Refresh(dir)
	r := &ToolRunner{Workspace: dir, Graph: ix}

	out, err := r.Run(context.Background(), "code_graph", `{"mode":"callers","query":"Helper"}`, RunConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "store/store.go") {
		t.Fatalf("callers = %q, want the inbound reference", out)
	}

	if _, err := r.Run(context.Background(), "file_edit", `{"path":"store/store.go","old":"return Helper()","new":"return 0"}`, RunConfig{}); err != nil {
		t.Fatal(err)
	}
	if ix.Dirty() == 0 {
		t.Fatal("an edit must mark the index entry for the file it changed")
	}

	out, err = r.Run(context.Background(), "code_graph", `{"mode":"callers","query":"Helper"}`, RunConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Load") {
		t.Fatalf("graph answered from the pre-edit bytes: %q", out)
	}
}

// A pattern without a segment wildcard is a filename pattern, not a directory pattern: the two are
// different questions and a search that conflates them reports matches the caller did not ask for.
func TestMatchGlobSupportsSegmentWildcards(t *testing.T) {
	if !matchGlob("**/internal/**/*.go", "backend/internal/api/code.go") {
		t.Fatal("** must match across path segments")
	}
	if !matchGlob("internal/?.go", "internal/a.go") {
		t.Fatal("? must match exactly one character")
	}
	if matchGlob("internal/*.go", "internal/api/sub.go") {
		t.Fatal("a single-segment * must not reach across a directory boundary")
	}
	if !matchGlob("*.go", "deep/nested/file.go") {
		t.Fatal("a bare filename pattern must match the base name anywhere")
	}
	if matchGlob("src/*.go", "lib/file.go") {
		t.Fatal("a rooted pattern must not match outside its root")
	}
}
