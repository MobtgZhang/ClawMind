package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mobtgzhang/clawmind/backend/internal/harness"
)

// Component is one SKILL.md or AGENT.md entry parsed from a plugin directory.
type Component struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mode        string `json:"mode,omitempty"`
	Path        string `json:"path"`
	Body        string `json:"body"`
}

var reComponentName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ParseComponent parses the frontmatter contract the runtime actually reads: name, description,
// and for a subagent the mode. A component without a description is never retrieved, so it is
// rejected at mount time rather than silently ignored later.
func ParseComponent(content, path string) (Component, error) {
	fm, body := harness.ParseFrontmatter(content)
	name := harness.FrontmatterValue(fm, "name")
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if !reComponentName.MatchString(name) {
		return Component{}, fmt.Errorf("component %s: name %q must be a lowercase slug", path, name)
	}
	desc := harness.FrontmatterValue(fm, "description")
	if desc == "" {
		return Component{}, fmt.Errorf("component %s: missing frontmatter description", path)
	}
	mode := harness.FrontmatterValue(fm, "mode")
	if strings.HasSuffix(path, "AGENT.md") && mode != "subagent" {
		return Component{}, fmt.Errorf("component %s: frontmatter needs mode: subagent", path)
	}
	return Component{Name: name, Description: desc, Mode: mode, Path: path, Body: strings.TrimSpace(body)}, nil
}

func readComponentFile(full string) (Component, error) {
	b, err := os.ReadFile(full)
	if err != nil {
		return Component{}, err
	}
	return ParseComponent(string(b), filepath.Base(full))
}

// ImportSkillDir reads a directory of SKILL.md files (Claude/OpenClaw layout) into a harness
// tree. Names, descriptions and referenced scripts are mapped onto the editable surface; the
// import never promises to cover every community variant.
func ImportSkillDir(dir string) (harness.Tree, []string, error) {
	t := harness.New()
	var notes []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !harness.OnSurface(rel) {
			notes = append(notes, "skipped off-surface "+rel)
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		if strings.HasSuffix(rel, "SKILL.md") {
			comp, perr := ParseComponent(string(b), rel)
			if perr != nil {
				notes = append(notes, perr.Error())
				return nil
			}
			t[rel] = "---\nname: " + comp.Name + "\ndescription: " + comp.Description + "\n---\n" + comp.Body + "\n"
			return nil
		}
		t[rel] = string(b)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return t, notes, nil
}
