package graphloop

import (
	"context"
	"strings"
	"testing"

	"github.com/mobtgzhang/clawmind/backend/internal/codectx"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
)

// fakeProvider answers node prose and, for the edit node, a scripted tool round.
type fakeProvider struct {
	prose     string
	toolCalls []llm.ToolCallResult
	calls     int
}

func (f *fakeProvider) Complete(ctx context.Context, p llm.CompleteParams) (*llm.CompleteResult, error) {
	f.calls++
	if f.calls <= len(f.toolCalls) {
		return &llm.CompleteResult{ToolCalls: []llm.ToolCallResult{f.toolCalls[f.calls-1]}, TotalTokens: 10}, nil
	}
	return &llm.CompleteResult{Content: "", TotalTokens: 10}, nil
}

func (f *fakeProvider) StreamChat(ctx context.Context, p llm.StreamParams, onChunk func(string) error) error {
	return onChunk(f.prose)
}

type fakeEmitter struct {
	parts []domain.PartType
	tools []string
}

func (e *fakeEmitter) Start(ctx context.Context, pt domain.PartType) error {
	e.parts = append(e.parts, pt)
	return nil
}

func (e *fakeEmitter) Delta(ctx context.Context, pt domain.PartType, text string) error { return nil }

func (e *fakeEmitter) End(ctx context.Context, pt domain.PartType) error { return nil }

func (e *fakeEmitter) Tool(ctx context.Context, name, args, result string) error {
	e.tools = append(e.tools, name)
	return nil
}

type fakeTools struct {
	ran    []string
	output map[string]string
}

func (f *fakeTools) Run(ctx context.Context, name, argsJSON string) (string, error) {
	f.ran = append(f.ran, name)
	if out, ok := f.output[name]; ok {
		return out, nil
	}
	return "", nil
}

func testDeps(p *fakeProvider, t *fakeTools, e *fakeEmitter, b codectx.Budget) *Deps {
	return &Deps{
		Client:   p,
		Tools:    t,
		Emitter:  e,
		Budget:   b,
		Profile:  &codectx.Profile{Commands: codectx.Commands{Test: "go test ./..."}},
		LastUser: "重构 store.Load 的调用点",
		Prompts:  DefaultPrompts(),
	}
}

func TestShapeForSimpleProjectsStaysLinear(t *testing.T) {
	g := Shape(codectx.Budget{Class: codectx.ClassSimple, MaxNodes: 5, Reentry: 1})
	for _, e := range g.Edges {
		if e.To == "locate" || e.To == "analyze" || e.To == "review" {
			t.Fatalf("simple shape must not add graph nodes, got edge %s -> %s", e.From, e.To)
		}
	}
	if g.next("plan", "ok", newState()) != "act" {
		t.Fatal("simple shape should go straight from plan to act")
	}
}

func TestShapeForComplexProjectsAddsBackEdges(t *testing.T) {
	g := Shape(codectx.Budget{Class: codectx.ClassComplex, MaxNodes: 12, Reentry: 3})
	if g.next("plan", "ok", newState()) != "locate" {
		t.Fatal("complex shape must locate before it edits")
	}
	if g.next("verify", "fail", newState()) != "act" {
		t.Fatal("a failed gate must send the run back to the edit node")
	}
	if g.next("review", "issues", newState()) != "act" {
		t.Fatal("review findings must be able to reopen the edit node")
	}
}

// A back edge that has spent its bound must fall through to the honest exit rather than retry.
func TestBoundedBackEdgeFallsBackInsteadOfRetrying(t *testing.T) {
	g := Shape(codectx.Budget{Class: codectx.ClassComplex, MaxNodes: 12, Reentry: 1})
	s := newState()
	if got := g.next("verify", "fail", s); got != "act" {
		t.Fatalf("first failure should re-enter act, got %q", got)
	}
	if got := g.next("verify", "fail", s); got != "answer" {
		t.Fatalf("second failure must fall back to answer, got %q", got)
	}
}

// The full run is what proves the shape terminates: a failing gate is retried exactly Reentry
// times and then delivered with the failure recorded, not repeated until the round cap.
func TestRunTerminatesAfterBoundedReentry(t *testing.T) {
	b := codectx.Budget{Class: codectx.ClassComplex, MaxNodes: 12, MaxRounds: 4, Reentry: 1, Fanout: 1, PromptChars: 1000, Verify: true, Review: false}
	provider := &fakeProvider{
		prose:     "目标：改 store/store.go\nDECISION: ok",
		toolCalls: []llm.ToolCallResult{{ID: "c1", Name: "file_edit", Arguments: `{"path":"store/store.go","old":"a","new":"b"}`}},
	}
	tools := &fakeTools{output: map[string]string{
		"code_search": "store/store.go:3: func Load()",
		"file_edit":   "edited store/store.go (1 occurrence)",
		"run_tests":   "exit non-zero: exit status 1\n--- FAIL: TestLoad",
	}}
	emitter := &fakeEmitter{}
	d := testDeps(provider, tools, emitter, b)

	s := newState()
	if err := Shape(b).Run(context.Background(), d, s); err != nil {
		t.Fatalf("run: %v", err)
	}

	gateVisits := 0
	for _, st := range s.Trace {
		if st.Kind == "decision" && st.Node == "verify" {
			gateVisits++
		}
	}
	if gateVisits != 2 {
		t.Fatalf("gate visited %d times, want 1 re-entry + the original visit", gateVisits)
	}
	if len(s.Edited) != 1 {
		t.Fatalf("edited = %v", s.Edited)
	}
	if len(s.Evidence) == 0 {
		t.Fatal("a failed gate must still be recorded as evidence")
	}
	if s.Trace[len(s.Trace)-1].Node != "answer" {
		t.Fatalf("run must end at the answer node, ended at %s", s.Trace[len(s.Trace)-1].Node)
	}
	if !strings.Contains(s.TraceBlock(), "执行图") {
		t.Fatal("trace must be renderable for the transcript")
	}
}

// The repeat guard refuses a duplicate call instead of ending the node: re-reading a file after an
// edit is legitimate, repeating a failing edit is not.
func TestRepeatGuardRefusesTheSameCall(t *testing.T) {
	b := codectx.Budget{Class: codectx.ClassSimple, MaxNodes: 5, MaxRounds: 3, Reentry: 1, Verify: true}
	call := llm.ToolCallResult{ID: "c1", Name: "file_edit", Arguments: `{"path":"a.go","old":"x","new":"y"}`}
	provider := &fakeProvider{toolCalls: []llm.ToolCallResult{call, call}}
	tools := &fakeTools{output: map[string]string{"file_edit": "错误: file_edit: old text not found in a.go"}}
	d := testDeps(provider, tools, &fakeEmitter{}, b)
	s := newState()

	decision, err := nodeAct(context.Background(), d, s)
	if err != nil {
		t.Fatalf("act: %v", err)
	}
	if decision != "no_edit" {
		t.Fatalf("decision = %q", decision)
	}
	if len(tools.ran) != 1 {
		t.Fatalf("the duplicate call should not reach the tool layer, ran %v", tools.ran)
	}
	if len(s.Trace) < 2 || s.Trace[1].Status != "fail" {
		t.Fatalf("the refused call must be recorded: %v", s.Trace)
	}
}

func TestDecisionAndStatusHelpers(t *testing.T) {
	if got := decision("写完了\nDECISION: issues", "ok"); got != "issues" {
		t.Fatalf("decision = %q", got)
	}
	if got := decision("没有决策行", "ok"); got != "ok" {
		t.Fatalf("fallback decision = %q", got)
	}
	if statusOf("exit non-zero: exit status 1") != "fail" {
		t.Fatal("non-zero exit must read as a failure")
	}
	if statusOf("错误: path escapes workspace") != "fail" {
		t.Fatal("tool errors must read as failures")
	}
	if statusOf("exit 0\nok") != "ok" {
		t.Fatal("a clean run must read as success")
	}
	targets := parseTargets("改动 store/store.go 与 `internal/api/handlers.go`")
	if len(targets) != 2 {
		t.Fatalf("targets = %v", targets)
	}
	files := matchFiles("3 个匹配（扫描 5 个文件）\nstore/store.go:3: func Load()\nstore/store.go:9: func Save()")
	if len(files) != 1 || files[0] != "store/store.go" {
		t.Fatalf("files = %v", files)
	}
	if got := argPath(`{"path":"a/b.go"}`); got != "a/b.go" {
		t.Fatalf("argPath = %q", got)
	}
}
