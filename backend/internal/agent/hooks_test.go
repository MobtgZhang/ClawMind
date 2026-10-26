package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
	"github.com/mobtgzhang/clawmind/backend/internal/memory"
	"github.com/mobtgzhang/clawmind/backend/internal/plugin"
)

type fakeSink struct {
	event string
	guide string
	veto  bool
	fired []string
}

func (f *fakeSink) Fire(ctx context.Context, event string, payload plugin.HookPayload) ([]plugin.HookResult, error) {
	f.event = event
	f.fired = append(f.fired, event)
	if f.veto {
		return []plugin.HookResult{{PluginID: "p1", Event: event, ExitCode: 2, Command: "deny"}}, nil
	}
	if f.guide == "" {
		return nil, nil
	}
	return []plugin.HookResult{{PluginID: "p1", Event: event, Command: "echo", Output: f.guide}}, nil
}

type fakeProvider struct {
	answer string
	tokens int
}

func (f *fakeProvider) Complete(ctx context.Context, p llm.CompleteParams) (*llm.CompleteResult, error) {
	return &llm.CompleteResult{Content: f.answer, TotalTokens: f.tokens}, nil
}

func (f *fakeProvider) StreamChat(ctx context.Context, p llm.StreamParams, onChunk func(string) error) error {
	return nil
}

func TestPreToolUseVetoBlocksTheTool(t *testing.T) {
	sink := &fakeSink{veto: true}
	r := &ToolRunner{Workspace: t.TempDir(), Hooks: sink}
	args, _ := json.Marshal(map[string]any{"path": "a.go", "old": "x", "new": "y"})
	_, err := r.Run(context.Background(), "file_edit", string(args), RunConfig{})
	if err == nil {
		t.Fatal("a veto must stop the tool call")
	}
	if !strings.Contains(err.Error(), "blocked by hook") {
		t.Fatalf("err = %q", err)
	}
	if sink.event != "PreToolUse" {
		t.Fatalf("event = %q", sink.event)
	}
}

func TestHookGuidanceIsReturnedWithTheToolOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sink := &fakeSink{guide: "run the package tests"}
	r := &ToolRunner{Workspace: dir, Hooks: sink}
	args, _ := json.Marshal(map[string]any{"path": "a.go", "old": "x", "new": "y"})
	out, err := r.Run(context.Background(), "file_edit", string(args), RunConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "run the package tests") {
		t.Fatalf("out = %q", out)
	}
	if len(sink.fired) != 2 || sink.fired[0] != "PreToolUse" || sink.fired[1] != "PostToolUse" {
		t.Fatalf("fired = %v", sink.fired)
	}
}

func TestDelegateRunsSubagentAndCapsItsAnswer(t *testing.T) {
	tree := harness.Tree{
		"sub_agents/researcher/AGENT.md": "---\nname: researcher\ndescription: bounded investigation\nmode: subagent\n---\nRead only, answer briefly.\n",
	}
	long := strings.Repeat("s", MaxDelegateChars+500)
	r := &ToolRunner{Workspace: t.TempDir(), Harness: tree, Client: &fakeProvider{answer: long, tokens: 12}, Hooks: &fakeSink{}}
	out, err := r.delegate(context.Background(), "researcher", "find the callers", RunConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "subagent researcher") {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(out, "truncated") {
		t.Fatalf("answer was not capped: %d chars", len(out))
	}
	if _, err := r.delegate(context.Background(), "unknown", "x", RunConfig{}); err == nil {
		t.Fatal("unknown subagent must error")
	}
}

type fakeStore struct {
	msg     *domain.Message
	history []domain.Message
	session *domain.Session
}

func (f *fakeStore) GetMessage(ctx context.Context, id string) (*domain.Message, error) {
	return f.msg, nil
}

func (f *fakeStore) ListMessages(ctx context.Context, sessionID string) ([]domain.Message, error) {
	return f.history, nil
}

func (f *fakeStore) UpdateMessageParts(ctx context.Context, id string, parts []domain.Part) error {
	f.msg.Parts = parts
	return nil
}

func (f *fakeStore) TouchSession(ctx context.Context, id string) error { return nil }

func (f *fakeStore) GetSession(ctx context.Context, id string) (*domain.Session, error) {
	return f.session, nil
}

func TestRunStreamFiresSessionAndCompactionHooks(t *testing.T) {
	parent := "u2"
	sink := &fakeSink{guide: "check the tests first"}
	store := &fakeStore{
		msg: &domain.Message{ID: "a2", SessionID: "s1", Role: domain.RoleAssistant, ParentMessageID: &parent},
		history: []domain.Message{
			{ID: "u1", SessionID: "s1", Role: domain.RoleUser, Parts: []domain.Part{{Type: domain.PartText, Text: strings.Repeat("old context ", 200)}}},
			{ID: "a1", SessionID: "s1", Role: domain.RoleAssistant, Parts: []domain.Part{{Type: domain.PartText, Text: strings.Repeat("answer ", 200)}}},
			{ID: "u2", SessionID: "s1", Role: domain.RoleUser, Parts: []domain.Part{{Type: domain.PartText, Text: "fix the bug"}}},
		},
		session: &domain.Session{ID: "s1", Model: "m"},
	}
	cfg := RunConfig{
		AssistantMessageID: "a2", SessionID: "s1", Model: "m",
		MaxContextTokens: 60, Hooks: sink,
	}
	if err := RunStream(context.Background(), store, memory.NoopStore{}, &fakeProvider{answer: "done"}, cfg, func(domain.StreamEvent) error { return nil }); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(sink.fired, ",")
	for _, want := range []string{"SessionStart", "UserPromptSubmit", "PostCompact", "Stop"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("events fired = %q, missing %s", joined, want)
		}
	}
}
