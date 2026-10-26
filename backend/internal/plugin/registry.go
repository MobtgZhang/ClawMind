package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/tools"
)

// Registry is the plugin tree composed at boot from ordered layers. There is no privileged
// core to patch: behaviour is added by mounting a plugin beside the others, and every
// registration is an effect that unwinds when its plugin unloads.
type Registry struct {
	Home   string
	layers []*Manifest
	// stacks records contributions per (kind, key) in mount order. The effective entry is the
	// top of the stack, so a later layer replaces an earlier one and unmounting restores it.
	stacks map[string][]contribution
}

type contribution struct {
	pluginID string
	kind     string // tool | skill | subagent | hook | row
	key      string
	tool     tools.Definition
	skill    harness.Skill
	hook     Hook
	row      PatchRow
}

func NewRegistry(home string) *Registry {
	return &Registry{Home: home, stacks: map[string][]contribution{}}
}

// Load reads every plugin directory under home in order and mounts the enabled ones.
// A plugin that fails validation is skipped and reported, never mounted.
func (r *Registry) Load() ([]string, error) {
	if r.Home == "" {
		return nil, fmt.Errorf("plugin: no home directory")
	}
	entries, err := os.ReadDir(r.Home)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var skipped []string
	for _, name := range names {
		m, err := LoadManifest(filepath.Join(r.Home, name))
		if err != nil {
			skipped = append(skipped, name+": "+err.Error())
			continue
		}
		if problems := m.Validate(); len(problems) > 0 {
			skipped = append(skipped, name+": "+strings.Join(problems, "; "))
			continue
		}
		if !m.Enabled {
			continue
		}
		if err := r.Mount(m); err != nil {
			skipped = append(skipped, name+": "+err.Error())
		}
	}
	return skipped, nil
}

// Mount registers one plugin's contributions. Registration is reversible: Unmount removes
// exactly what this plugin added and restores anything a later layer had overridden.
func (r *Registry) Mount(m *Manifest) error {
	if problems := m.Validate(); len(problems) > 0 {
		return fmt.Errorf("plugin %s: %s", m.ID, strings.Join(problems, "; "))
	}
	for _, existing := range r.layers {
		if existing.ID == m.ID {
			return fmt.Errorf("plugin %s is already mounted", m.ID)
		}
	}
	r.layers = append(r.layers, m)
	for _, def := range m.Tools {
		r.push(contribution{pluginID: m.ID, kind: "tool", key: def.Function.Name, tool: def})
	}
	for _, rel := range m.Skills {
		comp, err := readComponentFile(filepath.Join(m.Dir, rel))
		if err != nil {
			return fmt.Errorf("plugin %s: %w", m.ID, err)
		}
		r.push(contribution{pluginID: m.ID, kind: "skill", key: comp.Name, skill: harness.Skill{
			Name: comp.Name, Description: comp.Description, Path: rel, Body: comp.Body,
		}})
	}
	for _, rel := range m.Subagents {
		agent, err := readComponentFile(filepath.Join(m.Dir, rel))
		if err != nil {
			return fmt.Errorf("plugin %s: %w", m.ID, err)
		}
		r.push(contribution{pluginID: m.ID, kind: "subagent", key: agent.Name, skill: harness.Skill{
			Name: agent.Name, Description: agent.Description, Path: rel, Body: agent.Body,
		}})
	}
	for event, hooks := range m.Hooks {
		for _, h := range hooks {
			r.push(contribution{pluginID: m.ID, kind: "hook", key: event, hook: h})
		}
	}
	for _, row := range m.Patch {
		r.push(contribution{pluginID: m.ID, kind: "row", key: row.ID, row: row})
	}
	return nil
}

// Unmount rewinds exactly the contributions this plugin registered.
func (r *Registry) Unmount(pluginID string) error {
	found := false
	for key, stack := range r.stacks {
		var kept []contribution
		for _, c := range stack {
			if c.pluginID == pluginID {
				found = true
				continue
			}
			kept = append(kept, c)
		}
		if len(kept) == 0 {
			delete(r.stacks, key)
			continue
		}
		r.stacks[key] = kept
	}
	var kept []*Manifest
	for _, m := range r.layers {
		if m.ID != pluginID {
			kept = append(kept, m)
		}
	}
	if !found {
		return fmt.Errorf("plugin %s is not mounted", pluginID)
	}
	r.layers = kept
	return nil
}

func (r *Registry) push(c contribution) {
	r.stacks[c.kind+":"+c.key] = append(r.stacks[c.kind+":"+c.key], c)
}

// Layers returns mounted plugins in composition order.
func (r *Registry) Layers() []*Manifest { return r.layers }

// effective returns the top contribution for each key of one kind.
func (r *Registry) effective(kind string) []contribution {
	var out []contribution
	for key, stack := range r.stacks {
		if !strings.HasPrefix(key, kind+":") || len(stack) == 0 {
			continue
		}
		out = append(out, stack[len(stack)-1])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// all returns every contribution of one kind in mount order. Hooks accumulate rather than
// replace: several plugins can react to the same event, and unmounting one leaves the others.
func (r *Registry) all(kind string) []contribution {
	var keys []string
	for key := range r.stacks {
		if strings.HasPrefix(key, kind+":") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var out []contribution
	for _, key := range keys {
		out = append(out, r.stacks[key]...)
	}
	return out
}

// ToolSource is one effective tool definition plus the plugin that provided it.
type ToolSource struct {
	PluginID   string           `json:"pluginId"`
	Layer      int              `json:"layer"`
	Overridden bool             `json:"overridden"`
	Definition tools.Definition `json:"definition"`
}

// Tools returns the effective tool set: a later layer replaces the whole definition of an
// earlier one, which is what makes a tool registry replaceable from config.
func (r *Registry) Tools() []ToolSource {
	var out []ToolSource
	for _, c := range r.effective("tool") {
		stack := r.stacks["tool:"+c.key]
		out = append(out, ToolSource{
			PluginID:   c.pluginID,
			Layer:      r.layerIndex(c.pluginID),
			Overridden: len(stack) > 1,
			Definition: c.tool,
		})
	}
	return out
}

// Skills returns the effective skill set contributed by plugins.
func (r *Registry) Skills() []harness.Skill {
	var out []harness.Skill
	for _, c := range r.effective("skill") {
		out = append(out, c.skill)
	}
	return out
}

// Subagents returns the effective subagent set contributed by plugins. Subagents are stored as
// components because the frontmatter contract is the same; the runtime distinguishes them by kind.
func (r *Registry) Subagents() []harness.Subagent {
	var out []harness.Subagent
	for _, c := range r.effective("subagent") {
		out = append(out, harness.Subagent{Name: c.skill.Name, Description: c.skill.Description, Path: c.skill.Path, Body: c.skill.Body})
	}
	return out
}

// Hooks returns the effective hooks grouped by event.
func (r *Registry) Hooks() map[string][]Hook {
	out := map[string][]Hook{}
	for _, c := range r.all("hook") {
		out[c.key] = append(out[c.key], c.hook)
	}
	for event := range HookEvents {
		if _, ok := out[event]; !ok {
			out[event] = nil
		}
	}
	return out
}

// Config resolves the patch layers against an empty entry list, which is the same order the
// runtime uses: profile layers, then the home patch, then overlays.
func (r *Registry) Config() map[Seam][]PatchRow {
	out := map[Seam][]PatchRow{}
	for _, c := range r.effective("row") {
		out[c.row.Seam] = append(out[c.row.Seam], c.row)
	}
	for _, seam := range Seams() {
		rows := out[seam]
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
		out[seam] = rows
	}
	return out
}

// ApplyPatch applies one patch layer to an entry list: a row targeting an existing id replaces
// its whole config, otherwise it inserts.
func ApplyPatch(base []PatchRow, patch []PatchRow) []PatchRow {
	out := append([]PatchRow{}, base...)
	for _, p := range patch {
		replaced := false
		for i, row := range out {
			if row.ID == p.ID && row.Seam == p.Seam {
				out[i] = p
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, p)
		}
	}
	return out
}

func (r *Registry) layerIndex(pluginID string) int {
	for i, m := range r.layers {
		if m.ID == pluginID {
			return i + 1
		}
	}
	return 0
}

// LayerIndex reports the mount position of a plugin, or 0 when it is not mounted. The layer order
// is what decides which contribution wins for a key, so it has to be readable from outside.
func (r *Registry) LayerIndex(pluginID string) int { return r.layerIndex(pluginID) }
