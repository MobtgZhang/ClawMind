package evolution

import (
	"regexp"
	"strings"

	"github.com/mobtgzhang/clawmind/backend/internal/harness"
)

var (
	reProposal = regexp.MustCompile(`(?s)<proposal>(.*?)</proposal>`)
	reDelete   = regexp.MustCompile(`(?s)<delete>(.*?)</delete>`)
	reNotes    = regexp.MustCompile(`(?s)<notes>(.*?)</notes>`)
	reFence    = regexp.MustCompile("(?s)```[a-zA-Z]*\\s*\\n(.+?)\\n```")
)

// Proposal is one editor's output applied on top of a base tree.
type Proposal struct {
	Base    harness.Tree
	Tree    harness.Tree
	Notes   string
	Deleted []string
}

// ParseProposal turns an editor reply into a tree. Files not mentioned are inherited unchanged;
// a reply with no proposal blocks is KEEP_BASE.
func ParseProposal(text string, base harness.Tree) *Proposal {
	p := &Proposal{Tree: harness.New()}
	p.Base = base
	for rel, content := range base {
		p.Tree[rel] = content
	}
	if m := reProposal.FindStringSubmatch(text); len(m) > 1 {
		for _, block := range reFence.FindAllStringSubmatch(m[1], -1) {
			lines := strings.Split(strings.TrimSpace(block[1]), "\n")
			if len(lines) == 0 {
				continue
			}
			rel := strings.TrimSpace(lines[0])
			if !OnSurface(rel) {
				continue
			}
			p.Tree[rel] = strings.Join(lines[1:], "\n")
		}
	}
	if m := reDelete.FindStringSubmatch(text); len(m) > 1 {
		for _, line := range strings.Split(strings.TrimSpace(m[1]), "\n") {
			rel := strings.TrimSpace(line)
			if rel == "" {
				continue
			}
			if OnSurface(rel) {
				delete(p.Tree, rel)
				p.Deleted = append(p.Deleted, rel)
			}
		}
	}
	if m := reNotes.FindStringSubmatch(text); len(m) > 1 {
		p.Notes = strings.TrimSpace(m[1])
	}
	return p
}

// OnSurface restricts proposals and manual edits to the editable component face.
func OnSurface(rel string) bool {
	rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	if rel == harness.AgentsFile || rel == harness.GuidanceFile || rel == harness.LessonsFile {
		return true
	}
	if strings.HasPrefix(rel, harness.SkillDir+"/") || strings.HasPrefix(rel, harness.ScriptDir+"/") {
		return true
	}
	return false
}

// KEEP_BASE reports whether a proposal changed nothing.
// KeptBase reports whether a proposal changed nothing: adaptation the evidence does not
// support costs always-on context and buys nothing.
func (p *Proposal) KeptBase() bool { return harness.Diff(p.Base, p.Tree).Empty() }

// Probe is one candidate's predicted effect on the recorded trajectory. It is always a
// prediction: the candidate was not executed, so the observation is behavioural, not measured.
type Probe struct {
	Label      string `json:"label"`
	Score      int    `json:"score"`
	Observed   string `json:"observed,omitempty"`
	Confounded bool   `json:"confounded"`
}

var (
	reProbe      = regexp.MustCompile(`(?s)<probe>(.*?)</probe>`)
	reChoice     = regexp.MustCompile("(?s)```choice\\s*\\n?\\s*([A-Za-z0-9_-]+)\\s*\\n?```")
	reWhy        = regexp.MustCompile(`(?s)<why>(.*?)</why>`)
	reExperience = regexp.MustCompile(`(?s)<experience>(.*?)</experience>`)
	reEdits      = regexp.MustCompile(`(?s)<edits>(.*?)</edits>`)
)

func parseProbe(text, label string) Probe {
	out := Probe{Label: label, Observed: "no observable difference"}
	block := text
	if m := reProbe.FindStringSubmatch(text); len(m) > 1 {
		block = m[1]
	}
	for _, line := range strings.Split(block, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "score":
			n := 0
			for _, r := range val {
				if r >= '0' && r <= '9' {
					n = n*10 + int(r-'0')
				} else {
					break
				}
			}
			out.Score = n
		case "observed":
			if val != "" {
				out.Observed = val
			}
		case "confounded":
			out.Confounded = strings.EqualFold(val, "true")
		}
	}
	return out
}

func parseChoice(text string) string {
	if m := reChoice.FindStringSubmatch(text); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func parseExperience(text string) map[string]string {
	block := text
	if m := reExperience.FindStringSubmatch(text); len(m) > 1 {
		block = m[1]
	}
	out := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		out[key] = strings.TrimSpace(val)
	}
	if strings.TrimSpace(block) == "NO_ENTRY" {
		return nil
	}
	if out["cue"] == "" || out["lesson"] == "" {
		return nil
	}
	return out
}

type editRow struct {
	Component  string
	Candidates string
	Change     string
	Observed   string
	Verdict    string
}

func parseEditRows(text string) []editRow {
	block := text
	if m := reEdits.FindStringSubmatch(text); len(m) > 1 {
		block = m[1]
	}
	var rows []editRow
	var cur editRow
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "component":
			if cur.Component != "" {
				rows = append(rows, cur)
			}
			cur = editRow{Component: val}
		case "candidates":
			cur.Candidates = val
		case "change":
			cur.Change = val
		case "observed":
			cur.Observed = val
		case "verdict":
			cur.Verdict = val
		}
	}
	if cur.Component != "" {
		rows = append(rows, cur)
	}
	return rows
}
