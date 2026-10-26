package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobtgzhang/clawmind/backend/internal/tools"
)

func writePlugin(t *testing.T, home string, manifest *Manifest) string {
	t.Helper()
	dir := filepath.Join(home, manifest.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest.Dir = dir
	if err := manifest.SaveManifest(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func toolDef(name, desc string) tools.Definition {
	return tools.Definition{Type: "function", Function: tools.FunctionSpec{
		Name: name, Description: desc, Parameters: map[string]any{"type": "object"},
	}}
}

func TestValidateRejectsShapeTheDispatcherCannotConsume(t *testing.T) {
	m := &Manifest{ID: "demo", Enabled: true, Seams: []Seam{SeamTools}, Tools: []tools.Definition{toolDef("ok", "desc")}}
	if problems := m.Validate(); len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}

	bad := &Manifest{ID: "Bad ID", Seams: []Seam{"nope"}, Hooks: map[string][]Hook{
		"UnknownEvent": {{Type: "command", Command: "echo hi"}},
		"PreToolUse":   {{Type: "prompt", Command: ""}},
	}, Skills: []string{"../escape/SKILL.md"}}
	problems := bad.Validate()
	if len(problems) < 4 {
		t.Fatalf("problems = %v", problems)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"lowercase slug", "unknown seam", "UnknownEvent", "command", "escapes the plugin"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("problems %q missing %q", joined, want)
		}
	}
}

func TestMountUnmountIsReversibleAndRestoresOverriddenRows(t *testing.T) {
	home := t.TempDir()
	base := &Manifest{ID: "base", Enabled: true, Seams: []Seam{SeamTools}, Tools: []tools.Definition{toolDef("shell_exec", "base")}, Patch: []PatchRow{{ID: "model", Seam: SeamLLM, Config: map[string]any{"model": "base-model"}}}}
	writePlugin(t, home, base)
	over := &Manifest{ID: "override", Enabled: true, Seams: []Seam{SeamTools}, Tools: []tools.Definition{toolDef("shell_exec", "override")}, Patch: []PatchRow{{ID: "model", Seam: SeamLLM, Config: map[string]any{"model": "override-model"}}}}
	writePlugin(t, home, over)

	reg := NewRegistry(home)
	skipped, err := reg.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v", skipped)
	}
	if got := reg.Tools(); len(got) != 1 || got[0].PluginID != "override" || !got[0].Overridden {
		t.Fatalf("effective tool = %+v", got)
	}
	if cfg := reg.Config()[SeamLLM]; len(cfg) != 1 || cfg[0].Config["model"] != "override-model" {
		t.Fatalf("patch = %+v", cfg)
	}

	if err := reg.Unmount("override"); err != nil {
		t.Fatal(err)
	}
	got := reg.Tools()
	if len(got) != 1 || got[0].PluginID != "base" || got[0].Definition.Function.Description != "base" {
		t.Fatalf("after unmount = %+v", got)
	}
	if cfg := reg.Config()[SeamLLM]; len(cfg) != 1 || cfg[0].Config["model"] != "base-model" {
		t.Fatalf("after unmount patch = %+v", cfg)
	}
	if err := reg.Unmount("override"); err == nil {
		t.Fatal("unmounting an unmounted plugin should error")
	}
}

func TestLoadSkipsInvalidPluginsInsteadOfMountingThem(t *testing.T) {
	home := t.TempDir()
	good := &Manifest{ID: "good", Enabled: true, Seams: []Seam{SeamPrompt}}
	writePlugin(t, home, good)
	bad := &Manifest{ID: "bad", Seams: nil}
	writePlugin(t, home, bad)
	disabled := &Manifest{ID: "disabled", Seams: []Seam{SeamTools}}
	writePlugin(t, home, disabled)

	reg := NewRegistry(home)
	skipped, err := reg.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "bad") {
		t.Fatalf("skipped = %v", skipped)
	}
	if len(reg.Layers()) != 1 || reg.Layers()[0].ID != "good" {
		t.Fatalf("layers = %v", reg.Layers())
	}
}

func TestApplyPatchReplacesWholeConfigForAnExistingRow(t *testing.T) {
	base := []PatchRow{{ID: "model", Seam: SeamLLM, Config: map[string]any{"model": "a", "temperature": 0.2}}}
	patch := []PatchRow{{ID: "model", Seam: SeamLLM, Config: map[string]any{"model": "b"}}, {ID: "memory", Seam: SeamMemory, Config: map[string]any{"backend": "sqlite"}}}
	out := ApplyPatch(base, patch)
	if len(out) != 2 {
		t.Fatalf("out = %+v", out)
	}
	if out[0].Config["model"] != "b" {
		t.Fatalf("patch should replace the whole row, got %+v", out[0].Config)
	}
	if _, ok := out[0].Config["temperature"]; ok {
		t.Fatalf("partial merge is not the contract: %+v", out[0].Config)
	}
}

func TestImportSkillDirMapsComponentsOntoTheSurface(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "skills", "tdd"), 0o755); err != nil {
		t.Fatal(err)
	}
	skill := "---\nname: tdd\ndescription: 先写失败测试，再实现功能\n---\n步骤：先失败，再最小实现。\n"
	if err := os.WriteFile(filepath.Join(dir, "skills", "tdd", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub_agents", "reviewer"), 0o755); err != nil {
		t.Fatal(err)
	}
	agent := "---\nname: reviewer\ndescription: 审查正确性与可维护性\nmode: subagent\n---\n只读，不改代码。\n"
	if err := os.WriteFile(filepath.Join(dir, "sub_agents", "reviewer", "AGENT.md"), []byte(agent), 0o644); err != nil {
		t.Fatal(err)
	}
	// Off-surface content is reported, not imported.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	tree, notes, err := ImportSkillDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 2 {
		t.Fatalf("tree = %v", tree.Paths())
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "notes.txt") {
		t.Fatalf("notes = %v", notes)
	}
	if got := tree.Skills(); len(got) != 1 || got[0].Name != "tdd" {
		t.Fatalf("skills = %+v", got)
	}
	if got := tree.Subagents(); len(got) != 1 || got[0].Name != "reviewer" {
		t.Fatalf("subagents = %+v", got)
	}
}

func TestParseComponentRejectsSubagentWithoutMode(t *testing.T) {
	if _, err := ParseComponent("---\nname: r\ndescription: d\n---\nbody\n", "AGENT.md"); err == nil {
		t.Fatal("AGENT.md without mode: subagent should be rejected")
	}
	if _, err := ParseComponent("---\nname: r\n---\nbody\n", "SKILL.md"); err == nil {
		t.Fatal("component without a description is never retrieved and must be rejected")
	}
}

func TestDispatcherFiresEffectiveHooksOnlyForMatchingTools(t *testing.T) {
	home := t.TempDir()
	m := &Manifest{ID: "audit", Enabled: true, Seams: []Seam{SeamHooks}, Hooks: map[string][]Hook{
		"PreToolUse": {
			{Matcher: "shell_exec", Type: "command", Command: "echo ran"},
			{Matcher: "file_read", Type: "command", Command: "echo other"},
			{Type: "command", Command: "echo all"},
			{Type: "command", Command: "rm -rf /"},
		},
	}}
	writePlugin(t, home, m)
	reg := NewRegistry(home)
	if _, err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(reg)
	var seen []string
	d.SetRunner(func(ctx context.Context, event string, hook Hook, payload HookPayload) (string, error) {
		seen = append(seen, hook.Command)
		return "ok", nil
	})
	results, err := d.Fire(context.Background(), "PreToolUse", HookPayload{Tool: "shell_exec"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %+v", results)
	}
	if len(seen) != 2 || seen[0] != "echo ran" || seen[1] != "echo all" {
		t.Fatalf("runner saw %v", seen)
	}
	denied := 0
	for _, r := range results {
		if strings.Contains(r.Err, "denied") {
			denied++
		}
	}
	if denied != 1 {
		t.Fatalf("denied=%d results=%+v", denied, results)
	}
	if _, err := d.Fire(context.Background(), "NoSuchEvent", HookPayload{}); err == nil {
		t.Fatal("unknown event must error, not silently do nothing")
	}

	// A hook the dispatcher would never execute is rejected at mount time, not mounted and
	// silently doing nothing.
	bad := &Manifest{ID: "bad-hook", Enabled: true, Seams: []Seam{SeamHooks}, Hooks: map[string][]Hook{
		"PreToolUse": {{Type: "prompt", Command: "record only"}},
	}}
	if problems := bad.Validate(); len(problems) != 1 || !strings.Contains(problems[0], "command") {
		t.Fatalf("problems = %v", problems)
	}
	if err := reg.Mount(bad); err == nil {
		t.Fatal("mounting an invalid plugin must error")
	}
}

func TestDefaultRunnerRefusesHighRiskCommandsWithoutApproval(t *testing.T) {
	run := DefaultRunner(t.TempDir(), nil)
	if _, err := run(context.Background(), "PreToolUse", Hook{Type: "command", Command: "curl https://example.com"}, HookPayload{}); err == nil {
		t.Fatal("high-risk hook command needs approval")
	}
	approved := 0
	run = DefaultRunner(t.TempDir(), func(command string) bool {
		approved++
		return true
	})
	out, err := run(context.Background(), "PreToolUse", Hook{Type: "command", Command: "echo $CLAWMIND_HOOK_EVENT"}, HookPayload{Tool: "shell_exec"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "PreToolUse" || approved != 0 {
		t.Fatalf("out=%q approved=%d", out, approved)
	}
}

func TestSubagentContributionsAreVisibleAndUnmounted(t *testing.T) {
	home := t.TempDir()
	m := &Manifest{
		ID: "research", Enabled: true, Seams: []Seam{SeamTools},
		Subagents: []string{"sub_agents/researcher/AGENT.md"},
	}
	dir := writePlugin(t, home, m)
	if err := os.MkdirAll(filepath.Join(dir, "sub_agents", "researcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub_agents", "researcher", "AGENT.md"),
		[]byte("---\nname: researcher\ndescription: bounded investigation\nmode: subagent\n---\nRead only.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(home)
	if _, err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	subs := reg.Subagents()
	if len(subs) != 1 || subs[0].Name != "researcher" {
		t.Fatalf("subagents = %v", subs)
	}
	if err := reg.Unmount("research"); err != nil {
		t.Fatal(err)
	}
	if got := reg.Subagents(); len(got) != 0 {
		t.Fatalf("after unmount, subagents = %v", got)
	}
}

func TestVetoExitCodeSurvivesTheRunnerBoundary(t *testing.T) {
	home := t.TempDir()
	m := &Manifest{
		ID: "gate", Enabled: true, Seams: []Seam{SeamHooks},
		Hooks: map[string][]Hook{"PreToolUse": {{Event: "PreToolUse", Type: "command", Command: "exit 2"}}},
	}
	writePlugin(t, home, m)
	reg := NewRegistry(home)
	if _, err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(reg)
	d.SetRunner(DefaultRunner(home, nil))
	results, err := d.Fire(context.Background(), "PreToolUse", HookPayload{Tool: "file_edit"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %v", results)
	}
	if results[0].ExitCode != 2 {
		t.Fatalf("a veto must survive the runner boundary, got %v", results[0])
	}
}
