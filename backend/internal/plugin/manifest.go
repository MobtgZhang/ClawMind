package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/tools"
)

// Seam is a swappable capability with three roles: a definition, a provider, and a consumer.
// Every part of the product is a plugin, including the model adapter, the tool registry, the
// session log, the memory store, and the agent loop, so each is replaceable from config.
type Seam string

const (
	SeamLLM     Seam = "llm"
	SeamTools   Seam = "tools"
	SeamMemory  Seam = "memory"
	SeamSession Seam = "session"
	SeamSandbox Seam = "sandbox"
	SeamPrompt  Seam = "prompt"
	SeamHooks   Seam = "hooks"
)

// Seams lists the known seams in a stable order.
func Seams() []Seam {
	return []Seam{SeamLLM, SeamTools, SeamMemory, SeamSession, SeamSandbox, SeamPrompt, SeamHooks}
}

func ValidSeam(s Seam) bool {
	for _, known := range Seams() {
		if s == known {
			return true
		}
	}
	return false
}

// HookEvents is the event set the dispatcher fires; the harness surface owns the definition so
// the validator and the dispatcher cannot drift.
var HookEvents = harness.HookEvents

// Hook is one command hook on one event.
type Hook struct {
	Event   string `json:"event"`
	Matcher string `json:"matcher,omitempty"`
	Type    string `json:"type"`
	Command string `json:"command"`
}

// PatchRow is one config row. A patch targets a row by id and replaces its whole config,
// or inserts new rows; layers apply to an empty entry list in order.
type PatchRow struct {
	ID     string         `json:"id"`
	Seam   Seam           `json:"seam"`
	Config map[string]any `json:"config"`
}

var rePluginID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Manifest is one plugin's declaration, stored as plugin.json inside its directory.
type Manifest struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description"`
	Seams       []Seam             `json:"seams"`
	Tools       []tools.Definition `json:"tools,omitempty"`
	Skills      []string           `json:"skills,omitempty"`
	Subagents   []string           `json:"subagents,omitempty"`
	Hooks       map[string][]Hook  `json:"hooks,omitempty"`
	Patch       []PatchRow         `json:"patch,omitempty"`
	Enabled     bool               `json:"enabled"`
	Dir         string             `json:"-"`
}

// LoadManifest reads plugin.json from dir. A missing manifest returns os.ErrNotExist.
func LoadManifest(dir string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("plugin.json: %w", err)
	}
	m.Dir = dir
	if m.Name == "" {
		m.Name = filepath.Base(dir)
	}
	return &m, nil
}

// SaveManifest writes plugin.json inside dir.
func (m *Manifest) SaveManifest() error {
	if m.Dir == "" {
		return fmt.Errorf("plugin: no directory")
	}
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.Dir, "plugin.json"), b, 0o600)
}

// Validate enforces the shape the dispatcher actually consumes: a plugin that fails validation
// is not mounted, rather than mounted and silently doing nothing.
func (m *Manifest) Validate() []string {
	var problems []string
	if !rePluginID.MatchString(strings.TrimSpace(m.ID)) {
		problems = append(problems, "plugin id must be a lowercase slug")
	}
	if len(m.Seams) == 0 {
		problems = append(problems, "plugin needs at least one seam")
	}
	for _, s := range m.Seams {
		if !ValidSeam(s) {
			problems = append(problems, fmt.Sprintf("unknown seam %q; known seams: %s", s, strings.Join(seamNames(m.Seams), ", ")))
		}
	}
	seen := map[string]struct{}{}
	for _, t := range m.Tools {
		name := strings.TrimSpace(t.Function.Name)
		if name == "" {
			problems = append(problems, "tool entry with empty name")
			continue
		}
		if _, ok := seen[name]; ok {
			problems = append(problems, "duplicate tool "+name)
			continue
		}
		seen[name] = struct{}{}
		if t.Type != "function" {
			problems = append(problems, "tool "+name+": type must be function")
		}
		if strings.TrimSpace(t.Function.Description) == "" {
			problems = append(problems, "tool "+name+": missing description")
		}
		if len(t.Function.Parameters) == 0 {
			problems = append(problems, "tool "+name+": missing parameters schema")
		}
	}
	for event, entries := range m.Hooks {
		if _, ok := HookEvents[event]; !ok {
			problems = append(problems, fmt.Sprintf("hooks: unknown event %q (the dispatcher fires %s)", event, strings.Join(sortedHookEvents(), ", ")))
			continue
		}
		if len(entries) == 0 {
			problems = append(problems, "hooks: "+event+" maps to an empty list")
			continue
		}
		for i, h := range entries {
			if h.Type != "command" {
				problems = append(problems, fmt.Sprintf("hooks: %s[%d] type must be \"command\"", event, i))
			} else if strings.TrimSpace(h.Command) == "" {
				problems = append(problems, fmt.Sprintf("hooks: %s[%d] has an empty command", event, i))
			}
		}
	}
	for _, rel := range append(append([]string{}, m.Skills...), m.Subagents...) {
		if err := checkRel(rel); err != nil {
			problems = append(problems, err.Error())
		}
	}
	for _, row := range m.Patch {
		if strings.TrimSpace(row.ID) == "" {
			problems = append(problems, "patch row with empty id")
			continue
		}
		if !ValidSeam(row.Seam) {
			problems = append(problems, fmt.Sprintf("patch row %s: unknown seam %q", row.ID, row.Seam))
			continue
		}
		// A patch key the host never reads is a plugin that declares behaviour it does not have, so
		// it is reported at validation time instead of being quietly ignored at boot.
		known := ConsumedKeys(row.Seam)
		var keys []string
		for k := range row.Config {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !containsString(known, k) {
				problems = append(problems, fmt.Sprintf("patch row %s on seam %s: key %q is never read by the runtime (read keys: %s)", row.ID, row.Seam, k, strings.Join(known, ", ")))
				continue
			}
			accepted := NestedKeys(k)
			if len(accepted) == 0 {
				continue
			}
			nested, ok := row.Config[k].(map[string]any)
			if !ok {
				problems = append(problems, fmt.Sprintf("patch row %s on seam %s: key %q must be an object (read keys: %s)", row.ID, row.Seam, k, strings.Join(accepted, ", ")))
				continue
			}
			var inner []string
			for nk := range nested {
				inner = append(inner, nk)
			}
			sort.Strings(inner)
			for _, nk := range inner {
				if !containsString(accepted, strings.ToLower(nk)) {
					problems = append(problems, fmt.Sprintf("patch row %s on seam %s: nested key %q under %q is never read by the runtime (read keys: %s)", row.ID, row.Seam, nk, k, strings.Join(accepted, ", ")))
				}
			}
		}
	}
	return problems
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func checkRel(rel string) error {
	r := strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	if r == "" {
		return fmt.Errorf("empty path")
	}
	if strings.HasPrefix(r, "/") || strings.Contains(r, "..") {
		return fmt.Errorf("path escapes the plugin: %q", rel)
	}
	return nil
}

func seamNames(list []Seam) []string {
	var out []string
	for _, s := range list {
		if ValidSeam(s) {
			out = append(out, string(s))
		}
	}
	return out
}

func sortedHookEvents() []string {
	var out []string
	for e := range HookEvents {
		out = append(out, e)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
