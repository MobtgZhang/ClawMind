package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobtgzhang/clawmind/backend/internal/clawmindcfg"
	"github.com/mobtgzhang/clawmind/backend/internal/codectx"
	"github.com/mobtgzhang/clawmind/backend/internal/codegraph"
)

func newCodeServer(t *testing.T) *Server {
	return &Server{
		Cfg:       clawmindcfg.NewManager(t.TempDir()),
		CodeCache: codectx.NewCache(0),
		CodeGraph: codegraph.NewManager(codegraph.DefaultLimits(), 0),
	}
}

// A large workspace that scores complex must still honour an operator override: the override is the
// operator's decision, and a classifier that re-decides it makes the flag a suggestion.
func TestProjectClassOverrideWinsOverTheScore(t *testing.T) {
	t.Setenv("CLAWMIND_PROJECT_CLASS", "simple")
	dir := largeWorkspace(t)
	b, _, _ := newCodeServer(t).classifyWorkspace(dir, "重构依赖边界")
	if b.Class != codectx.ClassSimple {
		t.Fatalf("class = %s, want the override to win over the score", b.Class)
	}
	if len(b.Reasons) == 0 || !strings.Contains(b.Reasons[0], "强制") {
		t.Fatalf("reasons = %v, want the override to be reported as the reason", b.Reasons)
	}
}

// Without the override the same workspace has to reach the graph, otherwise the classifier is only a
// label and the loop shape never changes.
func TestClassifierScoresALargeWorkspaceComplex(t *testing.T) {
	dir := largeWorkspace(t)
	b, profile, shape := newCodeServer(t).classifyWorkspace(dir, "重构依赖边界")
	if b.Class != codectx.ClassComplex || b.Loop != "graph" {
		t.Fatalf("class=%s loop=%s score=%d reasons=%v, want a large workspace to run the graph", b.Class, b.Loop, b.Score, b.Reasons)
	}
	if profile == nil || shape.Symbols == 0 {
		t.Fatalf("profile=%v shape=%v, want both views to feed the score", profile, shape)
	}
}

// The endpoint may narrow the workspace root but never widen it, the same rule the sandbox layer
// applies to the agent: a root that can be widened is not a root.
func TestCodeWorkspaceCannotWidenTheRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_WORKSPACE", dir)
	s := &Server{}
	if _, ok := s.codeWorkspace(filepath.Join(dir, "..", "outside")); ok {
		t.Fatal("a query path must not reach outside the workspace root")
	}
	got, ok := s.codeWorkspace("sub/pkg")
	if !ok || got != filepath.Join(dir, "sub", "pkg") {
		t.Fatalf("codeWorkspace = %q ok=%v, want the root plus the requested subdirectory", got, ok)
	}
}

func largeWorkspace(t *testing.T) string {
	dir := t.TempDir()
	for p := 0; p < 5; p++ {
		pkg := fmt.Sprintf("pkg%d", p)
		if err := os.MkdirAll(filepath.Join(dir, pkg), 0o755); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < 26; f++ {
			body := fmt.Sprintf("package %s\n\nimport \"demo/internal/pkg0\"\n\nfunc A%d() int { return pkg0.B0() }\n\nfunc B%d() int { return A%d() }\n", pkg, f, f, f)
			if err := os.WriteFile(filepath.Join(dir, pkg, fmt.Sprintf("f%d.go", f)), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}
