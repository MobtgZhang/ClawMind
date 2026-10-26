package harness

import (
	"fmt"
	"regexp"
	"strings"
)

// Component names on the editable surface. Eight components, grouped by runtime role:
// context construction (system prompt, guardrails, memory), the tool interface (tool guidance,
// scripts), delegation (subagents), and middleware (plugins).
const (
	AgentsFile     = "AGENTS.md"
	GuardrailsFile = "guardrails.md"
	MemoryFile     = "memory.md"
	GuidanceFile   = "tool_guidance.md"
	HooksFile      = "hooks.json"
	SkillDir       = "skills"
	SubagentDir    = "sub_agents"
	ScriptDir      = "scripts"
	PluginDir      = "plugins"
)

// SurfaceSpec is the editable surface: paths, caps and per-surface rules. Everything that
// differs between runtimes is data on this value, so the shared logic exists once.
type SurfaceSpec struct {
	AlwaysOn        []string
	AlwaysOnMax     int
	SkillDir        string
	SubagentDir     string
	ScriptDir       string
	PluginDir       string
	HooksFile       string
	TriggerLine     string
	ReservedScripts []string
}

// DefaultSpec is the ClawMind surface. The always-on cap exists because every character here is
// paid on every run: it competes with the model's own output budget.
// SetAlwaysOnMax overrides the always-on character cap at boot. It is a process-wide value because
// every component that validates a tree reads the same spec; a plugin layer that raised the cap would
// let always-on text grow past the budget it exists to protect, so the override may only lower it.
func SetAlwaysOnMax(n int) {
	if n <= 0 || n > DefaultSpec().AlwaysOnMax {
		return
	}
	alwaysOnMaxOverride = n
}

var alwaysOnMaxOverride int

func DefaultSpec() SurfaceSpec {
	max := 4000
	if alwaysOnMaxOverride > 0 && alwaysOnMaxOverride < max {
		max = alwaysOnMaxOverride
	}
	return SurfaceSpec{
		AlwaysOn:        []string{AgentsFile, GuardrailsFile, MemoryFile, GuidanceFile},
		AlwaysOnMax:     max,
		SkillDir:        SkillDir,
		SubagentDir:     SubagentDir,
		ScriptDir:       ScriptDir,
		PluginDir:       PluginDir,
		HooksFile:       HooksFile,
		TriggerLine:     "当遇到「{desc}」时，加载 `{name}` 技能。",
		ReservedScripts: []string{"read_doc.py", "pack.py", "unpack.py"},
	}
}

var reComponentName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// NormalizeRel rejects escapes before a path is ever joined to a directory.
func NormalizeRel(rel string) (string, error) {
	r := strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	if r == "" {
		return "", fmt.Errorf("empty path")
	}
	if r == "." || strings.HasPrefix(r, "/") || containsDotDot(r) {
		return "", fmt.Errorf("path escapes the harness: %q", rel)
	}
	for strings.HasPrefix(r, "./") {
		r = r[2:]
	}
	return strings.Trim(r, "/"), nil
}

func containsDotDot(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// OnSurface reports whether a path is on the editable surface. It is a predicate and never
// raises, so a stray path cannot panic the editor.
func OnSurface(rel string) bool {
	rel, err := NormalizeRel(rel)
	if err != nil {
		return false
	}
	spec := DefaultSpec()
	for _, a := range spec.AlwaysOn {
		if rel == a {
			return true
		}
	}
	if rel == spec.HooksFile {
		return true
	}
	return matchSkill(spec, rel) || matchSubagent(spec, rel) || matchScript(spec, rel) || matchPlugin(spec, rel)
}

func matchSkill(spec SurfaceSpec, rel string) bool {
	prefix := spec.SkillDir + "/"
	if !strings.HasPrefix(rel, prefix) || !strings.HasSuffix(rel, "/SKILL.md") {
		return false
	}
	return reComponentName.MatchString(strings.TrimSuffix(strings.TrimPrefix(rel, prefix), "/SKILL.md"))
}

func matchSubagent(spec SurfaceSpec, rel string) bool {
	prefix := spec.SubagentDir + "/"
	if !strings.HasPrefix(rel, prefix) || !strings.HasSuffix(rel, "/AGENT.md") {
		return false
	}
	return reComponentName.MatchString(strings.TrimSuffix(strings.TrimPrefix(rel, prefix), "/AGENT.md"))
}

func matchScript(spec SurfaceSpec, rel string) bool {
	prefix := spec.ScriptDir + "/"
	if !strings.HasPrefix(rel, prefix) || !strings.HasSuffix(rel, ".py") {
		return false
	}
	return reComponentName.MatchString(strings.TrimSuffix(strings.TrimPrefix(rel, prefix), ".py"))
}

func matchPlugin(spec SurfaceSpec, rel string) bool {
	prefix := spec.PluginDir + "/"
	if !strings.HasPrefix(rel, prefix) || !strings.HasSuffix(rel, ".json") {
		return false
	}
	return reComponentName.MatchString(strings.TrimSuffix(strings.TrimPrefix(rel, prefix), ".json"))
}

// ComponentKind returns the surface role a path belongs to: always_on, skill, subagent, script,
// plugin, hooks, or "".
func ComponentKind(rel string) string {
	rel, err := NormalizeRel(rel)
	if err != nil {
		return ""
	}
	spec := DefaultSpec()
	for _, a := range spec.AlwaysOn {
		if rel == a {
			return "always_on"
		}
	}
	if rel == spec.HooksFile {
		return "hooks"
	}
	// lessons.md is the legacy name for the memory component; it still counts as always-on.
	if rel == LessonsFile {
		return "always_on"
	}
	switch {
	case matchSkill(spec, rel):
		return "skill"
	case matchSubagent(spec, rel):
		return "subagent"
	case matchScript(spec, rel):
		return "script"
	case matchPlugin(spec, rel):
		return "plugin"
	}
	return ""
}
