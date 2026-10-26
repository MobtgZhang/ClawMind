package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Editable harness surface. Only these paths are on the surface; anything else is dropped.
const (
	LessonsFile = "lessons.md"
)

// Subagent is one delegated component parsed from sub_agents/<name>/AGENT.md. It runs in its
// own context and returns text, so its cost is not paid on every run.
type Subagent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Body        string `json:"body"`
}

// Tree is one harness: relative path -> file content.
type Tree map[string]string

// Skill is one on-demand component parsed from skills/<name>/SKILL.md.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Body        string `json:"body"`
}

// New returns an empty tree.
func New() Tree { return Tree{} }

// Read loads a harness tree from disk (missing dir returns an empty tree).
func Read(dir string) (Tree, error) {
	t := New()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		t[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Write persists the tree under dir, deleting files no longer present.
func (t Tree) Write(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	want := map[string]struct{}{}
	for rel := range t {
		want[rel] = struct{}{}
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(t[rel]), 0o644); err != nil {
			return err
		}
	}
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if _, ok := want[rel]; !ok {
			_ = os.Remove(path)
		}
		return nil
	})
}

// Paths returns sorted relative paths.
func (t Tree) Paths() []string {
	out := make([]string, 0, len(t))
	for rel := range t {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// AlwaysOnPaths are the components injected into every turn.
func (t Tree) AlwaysOnPaths() []string {
	var out []string
	for _, p := range t.Paths() {
		if ComponentKind(p) != "always_on" {
			continue
		}
		// lessons.md is the legacy name for the memory component; memory.md wins when both exist.
		if p == LessonsFile && strings.TrimSpace(t[MemoryFile]) != "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// AlwaysOnChars counts the always-on cost: every character here is paid on every run.
func (t Tree) AlwaysOnChars() int {
	n := 0
	for _, p := range t.AlwaysOnPaths() {
		n += len(strings.TrimSpace(t[p]))
	}
	return n
}

// Skills parses skills/<name>/SKILL.md entries.
func (t Tree) Skills() []Skill {
	var out []Skill
	prefix := SkillDir + "/"
	for _, rel := range t.Paths() {
		if !strings.HasPrefix(rel, prefix) || !strings.HasSuffix(rel, "/SKILL.md") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(rel, prefix), "/SKILL.md")
		content := t[rel]
		fm, body := ParseFrontmatter(content)
		desc := FrontmatterValue(fm, "description")
		fname := FrontmatterValue(fm, "name")
		if fname != "" && fname != name {
			name = fname
		}
		out = append(out, Skill{Name: name, Description: desc, Path: rel, Body: body})
	}
	return out
}

// Subagents parses sub_agents/<name>/AGENT.md entries.
func (t Tree) Subagents() []Subagent {
	var out []Subagent
	prefix := SubagentDir + "/"
	for _, rel := range t.Paths() {
		if !strings.HasPrefix(rel, prefix) || !strings.HasSuffix(rel, "/AGENT.md") {
			continue
		}
		fm, body := ParseFrontmatter(t[rel])
		if fm["mode"] != "subagent" {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(rel, prefix), "/AGENT.md")
		if got := FrontmatterValue(fm, "name"); got != "" {
			name = got
		}
		out = append(out, Subagent{
			Name: name, Description: FrontmatterValue(fm, "description"),
			Path: rel, Body: strings.TrimSpace(body),
		})
	}
	return out
}

// Plugins lists plugins/<name>.json entries; the plugin registry parses them at boot.
func (t Tree) Plugins() []string {
	var out []string
	prefix := PluginDir + "/"
	for _, rel := range t.Paths() {
		if strings.HasPrefix(rel, prefix) && rel != prefix {
			out = append(out, rel)
		}
	}
	return out
}

// WithContributions returns a copy of the tree with plugin-contributed components materialised.
// The agent looks components up through one surface, so a skill or subagent mounted by a plugin
// is reachable through load_skill and delegate without a second lookup path. The copy is never
// written back: plugin contributions live in the plugin, not in the harness tree.
func (t Tree) WithContributions(skills []Skill, subs []Subagent) Tree {
	out := Tree{}
	for rel, content := range t {
		out[rel] = content
	}
	for _, s := range skills {
		out[SkillDir+"/"+s.Name+"/SKILL.md"] = renderComponent(s.Name, s.Description, s.Body, "")
	}
	for _, a := range subs {
		out[SubagentDir+"/"+a.Name+"/AGENT.md"] = renderComponent(a.Name, a.Description, a.Body, "subagent")
	}
	return out
}

func renderComponent(name, description, body string, mode string) string {
	var b strings.Builder
	b.WriteString("---\nname: " + name + "\n")
	if description != "" {
		b.WriteString("description: " + description + "\n")
	}
	if mode != "" {
		b.WriteString("mode: " + mode + "\n")
	}
	b.WriteString("---\n" + strings.TrimSpace(body) + "\n")
	return b.String()
}

// Scripts lists scripts/<name> entries.
func (t Tree) Scripts() []string {
	var out []string
	prefix := ScriptDir + "/"
	for _, rel := range t.Paths() {
		if strings.HasPrefix(rel, prefix) && rel != prefix {
			out = append(out, rel)
		}
	}
	return out
}

// DiffResult lists changed paths between two trees.
type DiffResult struct {
	Added    []string
	Removed  []string
	Modified []string
}

func (d DiffResult) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Modified) == 0
}

func (d DiffResult) Components() []string {
	seen := map[string]struct{}{}
	for _, p := range append(append(d.Added, d.Removed...), d.Modified...) {
		seen[p] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Contains reports whether a path changed in this diff.
func (d DiffResult) Contains(path string) bool {
	for _, p := range d.Components() {
		if p == path {
			return true
		}
	}
	return false
}

// Diff returns changed paths between base and next.
func Diff(base, next Tree) DiffResult {
	var d DiffResult
	for rel, content := range next {
		old, ok := base[rel]
		if !ok {
			d.Added = append(d.Added, rel)
			continue
		}
		if old != content {
			d.Modified = append(d.Modified, rel)
		}
	}
	for rel := range base {
		if _, ok := next[rel]; !ok {
			d.Removed = append(d.Removed, rel)
		}
	}
	sort.Strings(d.Added)
	sort.Strings(d.Removed)
	sort.Strings(d.Modified)
	return d
}

var reTemplateBraces = regexp.MustCompile(`\{\{|\{%`)
var reSnake = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9]+)?$`)

// Validate enforces the rules that make a proposal unusable at runtime: paths must stay on the
// surface, every skill needs a trigger line in always-on text, every named script must exist and
// compile, subagents need mode: subagent, plugins and hooks must parse, always-on text must not
// contain template braces, and the always-on block must fit its cap. A proposal that fails
// validation is discarded in full, never partially applied.
func (t Tree) Validate() []string {
	var problems []string
	alwaysOn := strings.Join(t.alwaysOnText(), "\n")
	spec := DefaultSpec()
	for rel := range t {
		if ComponentKind(rel) != "" {
			continue
		}
		if strings.HasPrefix(rel, ScriptDir+"/") && strings.HasSuffix(rel, ".py") {
			problems = append(problems, "script "+rel+": name must be snake_case")
			continue
		}
		problems = append(problems, "off-surface path "+rel)
	}
	for _, s := range t.Skills() {
		if s.Description == "" {
			problems = append(problems, "skill "+s.Path+": missing frontmatter description")
		}
		if !strings.Contains(alwaysOn, s.Name) {
			problems = append(problems, "skill "+s.Path+": name "+s.Name+" is not named in always-on text (never opened)")
		}
		for _, ref := range referencedScripts(s.Body) {
			if _, ok := t[ref]; !ok {
				problems = append(problems, "skill "+s.Path+" references missing script "+ref)
			}
		}
	}
	for _, a := range t.Subagents() {
		if a.Description == "" {
			problems = append(problems, "subagent "+a.Path+": missing frontmatter description")
		}
		if !strings.Contains(alwaysOn, a.Name) {
			problems = append(problems, "subagent "+a.Path+": name "+a.Name+" is not named in always-on text (never delegated to)")
		}
	}
	for _, rel := range t.Scripts() {
		if !reSnake.MatchString(strings.TrimPrefix(rel, ScriptDir+"/")) {
			problems = append(problems, "script "+rel+": name must be snake_case")
		}
		if reserved := ReservedScriptName(rel); reserved != "" {
			problems = append(problems, "script "+rel+": "+reserved+" is reserved by the runtime")
		}
		if msg := pythonCompiles(rel, t[rel]); msg != "" {
			problems = append(problems, "script "+rel+": "+msg)
		}
	}
	for _, rel := range t.Plugins() {
		if msg := pluginManifestProblem(t[rel]); msg != "" {
			problems = append(problems, "plugin "+rel+": "+msg)
		}
	}
	for _, msg := range hookProblems(t[HooksFile]) {
		problems = append(problems, msg)
	}
	for _, p := range t.AlwaysOnPaths() {
		if reTemplateBraces.MatchString(t[p]) {
			problems = append(problems, "always-on text "+p+": template braces are not allowed")
		}
	}
	if n := t.AlwaysOnChars(); n > spec.AlwaysOnMax {
		problems = append(problems, fmt.Sprintf("always-on text %d chars exceeds cap %d", n, spec.AlwaysOnMax))
	}
	return problems
}

// AutoTrigger appends a trigger line for each skill missing one, returning the names fixed.
func (t Tree) AutoTrigger() []string {
	fixed := []string{}
	alwaysOn := t[AgentsFile]
	for _, s := range t.Skills() {
		if strings.Contains(alwaysOn, s.Name) {
			continue
		}
		if strings.TrimSpace(alwaysOn) == "" {
			alwaysOn = "# 角色与流程\n"
		}
		line := "\n" + TriggerLine(DefaultSpec(), s.Name, firstClause(s.Description))
		// A line that would push the always-on block over its cap is skipped, not added.
		if len(strings.TrimSpace(alwaysOn))+len(line) > DefaultSpec().AlwaysOnMax {
			continue
		}
		alwaysOn += line
		fixed = append(fixed, s.Name)
	}
	if len(fixed) > 0 {
		t[AgentsFile] = alwaysOn
	}
	return fixed
}

func (t Tree) alwaysOnText() []string {
	var out []string
	for _, p := range t.AlwaysOnPaths() {
		if strings.TrimSpace(t[p]) != "" {
			out = append(out, t[p])
		}
	}
	return out
}

func referencedScripts(body string) []string {
	var out []string
	for _, m := range reScriptRef.FindAllString(body, -1) {
		out = append(out, m)
	}
	return out
}

var reScriptRef = regexp.MustCompile(`scripts/[a-z0-9_]+\.[a-z0-9]+`)

// ParseFrontmatter splits a leading --- block from the body. Skills, subagents and plugins all
// use the same contract, so the parser exists once.
func ParseFrontmatter(content string) (map[string]string, string) {
	fm := map[string]string{}
	lines := strings.Split(content, "\n")
	i := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i = 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				break
			}
			if strings.Contains(lines[i], ":") {
				k, v, _ := strings.Cut(strings.TrimSpace(lines[i]), ":")
				fm[k] = strings.TrimSpace(v)
			}
		}
		i++
	}
	return fm, strings.Join(lines[i:], "\n")
}

func FrontmatterValue(fm map[string]string, key string) string {
	return strings.TrimSpace(fm[key])
}

func firstClause(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "该技能描述的情形"
	}
	for _, sep := range []string{"；", ";", "，", ",", "。", "."} {
		if i := strings.Index(s, sep); i > 0 {
			return s[:i]
		}
	}
	if len([]rune(s)) > 40 {
		return string([]rune(s)[:40])
	}
	return s
}
