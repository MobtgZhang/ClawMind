package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// HookEvents are the events the dispatcher fires. Only command hooks run; other hook types are
// recorded but never executed, so a malformed hook is rejected at write time instead of silently
// doing nothing.
var HookEvents = map[string]struct{}{
	"SessionStart":     {},
	"UserPromptSubmit": {},
	"PreToolUse":       {},
	"PostToolUse":      {},
	"PostCompact":      {},
	"Stop":             {},
	"SubagentStop":     {},
}

// TriggerLine renders one skill trigger line. A skill that is never named in always-on text is
// never opened, so the trigger is part of the component, not a nicety.
func TriggerLine(spec SurfaceSpec, name, desc string) string {
	return strings.NewReplacer("{name}", name, "{desc}", desc).Replace(spec.TriggerLine)
}

// ReservedScriptName returns the reserved runtime name a script path collides with, or "".
func ReservedScriptName(rel string) string {
	rel, err := NormalizeRel(rel)
	if err != nil {
		return ""
	}
	spec := DefaultSpec()
	if !strings.HasPrefix(rel, spec.ScriptDir+"/") {
		return ""
	}
	name := strings.TrimSuffix(strings.TrimPrefix(rel, spec.ScriptDir+"/"), ".py")
	for _, reserved := range spec.ReservedScripts {
		if name == strings.TrimSuffix(reserved, ".py") {
			return reserved
		}
	}
	return ""
}

// PythonCheckEnabled can be turned off by tests or by a host without python3.
var PythonCheckEnabled = true

// pythonCompiles reports a syntax error in a proposed script. A script that cannot be parsed is
// never executed, so the check runs where the proposal is written, not when the agent calls it.
func pythonCompiles(rel, src string) string {
	if !PythonCheckEnabled || strings.TrimSpace(src) == "" {
		return ""
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-c", "import sys; compile(sys.stdin.read(), sys.argv[1], 'exec')", rel)
	cmd.Stdin = strings.NewReader(src)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return "does not compile"
	}
	return msg
}

// pluginManifestProblem checks the shape the plugin registry actually consumes.
func pluginManifestProblem(src string) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return "empty manifest"
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(src), &doc); err != nil {
		return "manifest is not valid JSON: " + err.Error()
	}
	id, _ := doc["id"].(string)
	if strings.TrimSpace(id) == "" {
		return "manifest needs an id"
	}
	seams, ok := doc["seams"].([]any)
	if !ok || len(seams) == 0 {
		return "manifest needs at least one seam"
	}
	for _, s := range seams {
		name, _ := s.(string)
		if _, ok := pluginSeams[name]; !ok {
			return "unknown seam " + name
		}
	}
	return ""
}

var pluginSeams = map[string]struct{}{
	"llm": {}, "tools": {}, "memory": {}, "session": {}, "sandbox": {}, "prompt": {}, "hooks": {},
}

// hookProblems validates the hooks file against the schema the dispatcher dispatches.
func hookProblems(src string) []string {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}
	var errs []string
	var doc map[string]any
	if err := json.Unmarshal([]byte(src), &doc); err != nil {
		return []string{"hooks.json is not valid JSON: " + err.Error()}
	}
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok || len(hooks) == 0 {
		return []string{`hooks.json needs a top-level {"hooks": {"<Event>": [...]}} object`}
	}
	for event, raw := range hooks {
		if _, ok := HookEvents[event]; !ok {
			errs = append(errs, "hooks.json: unknown event "+event)
			continue
		}
		entries, ok := raw.([]any)
		if !ok {
			errs = append(errs, "hooks.json: "+event+" must map to a list of matcher entries")
			continue
		}
		if len(entries) == 0 {
			errs = append(errs, "hooks.json: "+event+" maps to an empty list")
			continue
		}
		for i, entry := range entries {
			obj, ok := entry.(map[string]any)
			if !ok {
				errs = append(errs, fmt.Sprintf("hooks.json: %s entry %d is not an object", event, i))
				continue
			}
			list, ok := obj["hooks"].([]any)
			if !ok || len(list) == 0 {
				errs = append(errs, "hooks.json: "+event+" entry needs a hooks list")
				continue
			}
			for j, h := range list {
				hook, ok := h.(map[string]any)
				if !ok {
					errs = append(errs, fmt.Sprintf("hooks.json: %s entry %d hook %d is not an object", event, i, j))
					continue
				}
				if hook["type"] != "command" {
					errs = append(errs, fmt.Sprintf("hooks.json: %s entry %d hook %d type must be \"command\"", event, i, j))
				} else if cmd, _ := hook["command"].(string); strings.TrimSpace(cmd) == "" {
					errs = append(errs, fmt.Sprintf("hooks.json: %s entry %d hook %d has an empty command", event, i, j))
				}
			}
		}
	}
	return errs
}
