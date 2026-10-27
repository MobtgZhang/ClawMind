package graphloop

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mobtgzhang/clawmind/backend/internal/llm"
)

// decisionWord is the control signal a node's prose emits. Without it a node can describe a plan
// it cannot execute and the graph has no way to choose its outgoing edge.
var reDecision = regexp.MustCompile(`(?i)^\s*DECISION:\s*([A-Za-z_]+)`)

// decision reads the node's decision from its output, falling back when the model omitted it.
func decision(text, fallback string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if m := reDecision.FindStringSubmatch(lines[i]); m != nil {
			return strings.ToLower(strings.TrimSpace(m[1]))
		}
	}
	return fallback
}

// statusOf classifies one tool result. The graph branches on this, so "the command ran" is not
// enough: a non-zero exit and an error reply have to be distinguishable from a successful call.
func statusOf(result string) string {
	s := strings.TrimSpace(result)
	if s == "" {
		return "ok"
	}
	if strings.HasPrefix(s, "错误:") || strings.Contains(s, "exit non-zero") {
		return "fail"
	}
	return "ok"
}

// reTarget matches a path-like token in the plan: a plan that names files is executable, a plan
// that only describes intent is not.
var reTarget = regexp.MustCompile(`(?m)[\w./\-]+\.(go|py|ts|tsx|js|jsx|rs|java|c|cc|cpp|h|hpp|rb|php|cs|kt|swift|vue|svelte|sql|sh|json|toml|yaml|yml|md)`)

// parseTargets extracts the file and symbol targets the plan committed to.
func parseTargets(text string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, m := range reTarget.FindAllString(text, -1) {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
		if len(out) >= 12 {
			break
		}
	}
	return out
}

// matchFiles pulls file paths out of a code_search reply (`path:line: text`), so the next node can
// read the located files instead of re-searching for them.
func matchFiles(result string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, line := range strings.Split(result, "\n") {
		i := strings.Index(line, ":")
		if i <= 0 {
			continue
		}
		file := strings.TrimSpace(line[:i])
		rest := line[i+1:]
		j := strings.Index(rest, ":")
		if j <= 0 {
			continue
		}
		// Only accept lines where the second segment is a line number: a plain path containing a
		// colon would otherwise be mistaken for a match.
		numeric := true
		for _, r := range rest[:j] {
			if r < '0' || r > '9' {
				numeric = false
				break
			}
		}
		if !numeric {
			continue
		}
		if _, ok := seen[file]; ok {
			continue
		}
		seen[file] = struct{}{}
		out = append(out, file)
		if len(out) >= 40 {
			break
		}
	}
	return out
}

func argPath(argsJSON string) string {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return ""
	}
	return filepath.ToSlash(strings.TrimSpace(a.Path))
}

// contextBlock is what the plan node is allowed to know: the bounded profile, the graph summary,
// the conversation so far, and the request. A node that plans from the request alone re-derives
// facts the runtime already paid for.
func contextBlock(d *Deps, s *State) string {
	var b strings.Builder
	if d.Profile != nil {
		if block := d.Profile.PromptBlock(d.Budget.PromptChars); block != "" {
			b.WriteString(block + "\n\n")
		}
	}
	if d.Graph != nil && d.Budget.Graph {
		if block := d.Graph.PromptBlock(d.Budget.PromptChars); block != "" {
			b.WriteString(block + "\n\n")
		}
	}
	if h := historyBlock(d.History, d.Budget.PromptChars); h != "" {
		b.WriteString(h + "\n\n")
	}
	b.WriteString("## 用户请求\n\n" + d.LastUser)
	return b.String()
}

// historyBlock renders prior turns as plain text. A node receives the conversation as evidence,
// not as a chat transcript it has to re-answer.
func historyBlock(history []llm.ChatMessage, maxChars int) string {
	if len(history) == 0 {
		return ""
	}
	if maxChars <= 0 {
		maxChars = 2000
	}
	var lines []string
	for _, m := range history {
		role := strings.TrimSpace(m.Role)
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		if len([]rune(text)) > 400 {
			text = string([]rune(text)[:400]) + "…"
		}
		lines = append(lines, role+": "+text)
	}
	if len(lines) == 0 {
		return ""
	}
	out := "## 对话上下文\n\n" + strings.Join(lines, "\n")
	if len(out) > maxChars {
		out = out[:maxChars] + "…"
	}
	return out
}

// actBlock is the edit node's input: the plan it has to carry out plus what locate and analyze
// actually found. Passing the plan alone would let the node edit files the plan invented.
func actBlock(d *Deps, s *State) string {
	var b strings.Builder
	if s.Plan != "" {
		b.WriteString("## 计划\n\n" + s.Plan + "\n\n")
	}
	if len(s.Candidates) > 0 {
		b.WriteString("## 已定位文件\n\n")
		for _, f := range s.Candidates {
			b.WriteString("- " + f + "\n")
		}
		b.WriteString("\n")
	}
	if d.Graph != nil && len(s.Symbols) > 0 {
		b.WriteString("## 影响面（必须同步的调用点）\n\n")
		for _, name := range s.Symbols {
			hits := d.Graph.Callers(name, 1, 15)
			if len(hits) == 0 {
				continue
			}
			b.WriteString("### " + name + "\n")
			for _, h := range hits {
				b.WriteString(fmt.Sprintf("- %s %s · %s:%d\n", h.Symbol.Kind, h.Symbol.Name, h.Symbol.File, h.Symbol.Line))
			}
			b.WriteString("\n")
		}
	}
	if len(s.Findings) > 0 {
		b.WriteString("## 评审要求（返工）\n\n" + strings.Join(s.Findings, "\n\n") + "\n\n")
	}
	b.WriteString("## 请求\n\n" + d.LastUser)
	return b.String()
}

func reviewBlock(d *Deps, s *State) string {
	var b strings.Builder
	if s.Plan != "" {
		b.WriteString("## 计划\n\n" + s.Plan + "\n\n")
	}
	if len(s.Edited) > 0 {
		b.WriteString("## 实际改动\n\n")
		for _, f := range s.Edited {
			b.WriteString("- " + f + "\n")
		}
		b.WriteString("\n")
	}
	if len(s.Evidence) > 0 {
		b.WriteString("## 验证证据\n\n")
		for _, e := range s.Evidence {
			b.WriteString("- " + e + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("## 请求\n\n" + d.LastUser)
	return b.String()
}

// answerBlock is the deliverable node's input. The evidence list is included so the answer can
// separate what was proven from what was inferred, which is the claim the linear loop cannot make.
func answerBlock(d *Deps, s *State) string {
	var b strings.Builder
	if s.Plan != "" {
		b.WriteString("## 计划\n\n" + s.Plan + "\n\n")
	}
	if len(s.Edited) > 0 {
		b.WriteString("## 改动文件\n\n")
		for _, f := range s.Edited {
			b.WriteString("- " + f + "\n")
		}
		b.WriteString("\n")
	}
	if len(s.Evidence) > 0 {
		b.WriteString("## 证据（已执行的命令与结果）\n\n")
		for _, e := range s.Evidence {
			b.WriteString("- " + e + "\n")
		}
		b.WriteString("\n")
	} else {
		b.WriteString("## 证据\n\n无：没有执行任何验证命令，不得声称改动已通过。\n\n")
	}
	if len(s.Findings) > 0 {
		b.WriteString("## 评审结论\n\n" + strings.Join(s.Findings, "\n\n") + "\n\n")
	}
	if trace := s.TraceBlock(); trace != "" {
		b.WriteString(trace + "\n\n")
	}
	b.WriteString("## 请求\n\n" + d.LastUser)
	return b.String()
}
