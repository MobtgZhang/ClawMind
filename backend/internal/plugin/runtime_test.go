package plugin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mobtgzhang/clawmind/backend/internal/security"
)

func TestResolveAppliesPatchLayersInMountOrder(t *testing.T) {
	home := t.TempDir()
	base := &Manifest{ID: "base", Enabled: true, Seams: []Seam{SeamLLM}, Patch: []PatchRow{
		{ID: "model", Seam: SeamLLM, Config: map[string]any{"model": "base-model", "maxAgentRounds": 4}},
	}}
	over := &Manifest{ID: "override", Enabled: true, Seams: []Seam{SeamLLM}, Patch: []PatchRow{
		{ID: "model", Seam: SeamLLM, Config: map[string]any{"model": "layer-model"}},
	}}
	writePlugin(t, home, base)
	writePlugin(t, home, over)
	reg := NewRegistry(home)
	if _, err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	rt := Resolve(reg)
	if rt.LLM.Model != "layer-model" {
		t.Fatalf("model = %q, want the top layer to win", rt.LLM.Model)
	}
	// A row that replaces a whole config drops the keys the earlier row carried, which is the
	// documented patch semantics; the surviving key comes from the row that is still effective.
	if rt.LLM.MaxAgentRounds != 0 {
		t.Fatalf("maxAgentRounds = %d, want 0 because the winning row replaced the whole config", rt.LLM.MaxAgentRounds)
	}

	only := &Manifest{ID: "only", Enabled: true, Seams: []Seam{SeamSession}, Patch: []PatchRow{
		{ID: "ctx", Seam: SeamSession, Config: map[string]any{"maxContextTokens": 8000, "tokenBudget": 1500}},
	}}
	writePlugin(t, home, only)
	reg2 := NewRegistry(home)
	if _, err := reg2.Load(); err != nil {
		t.Fatal(err)
	}
	if rt2 := Resolve(reg2); rt2.Session.MaxContextTokens != 8000 || rt2.Session.TokenBudget != 1500 {
		t.Fatalf("session layer = %+v", rt2.Session)
	}
}

func TestValidateRejectsPatchKeysTheRuntimeNeverReads(t *testing.T) {
	m := &Manifest{ID: "demo", Enabled: true, Seams: []Seam{SeamMemory}, Patch: []PatchRow{
		{ID: "mem", Seam: SeamMemory, Config: map[string]any{"maxItems": 12, "vectorStore": "pinecone"}},
	}}
	problems := m.Validate()
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want exactly the unconsumed key", problems)
	}
	if !strings.Contains(problems[0], `key "vectorStore" is never read`) {
		t.Fatalf("problems = %v", problems)
	}
}

func TestSandboxLayerCanOnlyAddPatternsAndLowerLimits(t *testing.T) {
	layer := SandboxRuntime{
		Denied:        []string{"shutdown"},
		HighRisk:      []string{"git push"},
		ShellTimeout:  10 * time.Second,
		AllowApproval: true,
	}
	policy := layer.Policy()
	if !policy.CommandDenied("shutdown now") {
		t.Fatal("added denied pattern did not take effect")
	}
	if !policy.CommandHighRisk("git push origin main") {
		t.Fatal("added high-risk pattern did not take effect")
	}
	// The built-in floor survives: a layer cannot remove a pattern it did not add.
	if !policy.CommandDenied("rm -rf /") {
		t.Fatal("a patch layer must not be able to remove a built-in denied pattern")
	}
	if policy.ShellTimeout > security.MaxShellTimeout {
		t.Fatalf("shell timeout = %v, a layer may only lower the ceiling", policy.ShellTimeout)
	}
	if security.ResolvePolicy(SandboxRuntime{}.Policy()).ShellTimeout != security.MaxShellTimeout {
		t.Fatal("an empty layer must keep the built-in timeout")
	}
}

func TestDispatcherRecordsHooksAsSkippedWhenShellIsDisabled(t *testing.T) {
	home := t.TempDir()
	m := &Manifest{ID: "quiet", Enabled: true, Seams: []Seam{SeamHooks}, Hooks: map[string][]Hook{
		"PreToolUse": {{Event: "PreToolUse", Type: "command", Command: "echo hi"}},
	}}
	writePlugin(t, home, m)
	reg := NewRegistry(home)
	if _, err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(reg)
	d.SetAllowShell(false)
	results, err := d.Fire(context.Background(), "PreToolUse", HookPayload{Tool: "shell_exec"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Skipped {
		t.Fatalf("results = %+v, want a skipped hook rather than a silent absence", results)
	}
	if !strings.Contains(results[0].Err, "disabled by the hook layer") {
		t.Fatalf("result = %+v", results[0])
	}
}

func TestProvenanceNamesTheLayerThatWinsEachKey(t *testing.T) {
	home := t.TempDir()
	base := &Manifest{ID: "base", Enabled: true, Seams: []Seam{SeamLLM}, Patch: []PatchRow{
		{ID: "model", Seam: SeamLLM, Config: map[string]any{"model": "base-model", "temperature": 0.3}},
	}}
	over := &Manifest{ID: "override", Enabled: true, Seams: []Seam{SeamLLM}, Patch: []PatchRow{
		{ID: "model", Seam: SeamLLM, Config: map[string]any{"model": "layer-model"}},
	}}
	writePlugin(t, home, base)
	writePlugin(t, home, over)
	reg := NewRegistry(home)
	if _, err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	source := reg.Provenance(SeamLLM)
	if source["model"] != "override" {
		t.Fatalf("source = %+v, want the winning layer to be named", source)
	}
	if source["temperature"] != "base" {
		t.Fatalf("source = %+v, want the surviving key to name its own author", source)
	}
}

func TestValidateRejectsNestedKeysTheRuntimeNeverReads(t *testing.T) {
	m := &Manifest{ID: "demo", Enabled: true, Seams: []Seam{SeamMemory}, Patch: []PatchRow{
		{ID: "mem", Seam: SeamMemory, Config: map[string]any{"caps": map[string]any{"L1": 40, "l9": 5}}},
	}}
	problems := m.Validate()
	if len(problems) != 1 {
		t.Fatalf("problems = %v, want only the unread nested key", problems)
	}
	if !strings.Contains(problems[0], `nested key "l9"`) {
		t.Fatalf("problems = %v", problems)
	}
	if len(NestedKeys("caps")) != 4 {
		t.Fatalf("NestedKeys(caps) = %v", NestedKeys("caps"))
	}
}
