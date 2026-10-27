package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/mobtgzhang/clawmind/backend/internal/codectx"
)

// MaxSearchResults bounds one code_search reply so it does not crowd the context.
const MaxSearchResults = 60

// MaxSearchScan is the ceiling on files one search may walk. It is a ceiling, not a target: the
// project class picks the working value, and a large repository needs a larger one than a three-file
// script without ever becoming unbounded.
const MaxSearchScan = 20000

// searchArgs is the code_search argument shape.
type searchArgs struct {
	Query  string `json:"query"`
	Path   string `json:"path"`
	Glob   string `json:"glob"`
	Regex  bool   `json:"regex"`
	Max    int    `json:"max_results"`
	Ignore bool   `json:"case_insensitive"`
}

// codeSearch walks the workspace and returns bounded file:line matches.
func (r *ToolRunner) codeSearch(ctx context.Context, argsJSON string, cfg RunConfig) (string, error) {
	var a searchArgs
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return "", err
	}
	query := strings.TrimSpace(a.Query)
	if query == "" {
		return "", fmt.Errorf("code_search: query required")
	}
	re, err := buildMatcher(query, a.Regex, a.Ignore)
	if err != nil {
		return "", err
	}
	glob := strings.TrimSpace(a.Glob)
	limit := a.Max
	if limit <= 0 {
		limit = cfg.Budget.SearchResults
	}
	if limit <= 0 {
		limit = MaxSearchResults
	}
	if limit > 200 {
		limit = 200
	}
	scanCeiling := cfg.Budget.SearchFiles
	if scanCeiling <= 0 {
		scanCeiling = 3000
	}
	if scanCeiling > MaxSearchScan {
		scanCeiling = MaxSearchScan
	}
	root := r.Workspace
	if p := strings.TrimSpace(a.Path); p != "" {
		full, err := r.safePath(p)
		if err != nil {
			return "", err
		}
		root = full
	}
	var out []string
	var scanned int
	var truncated bool
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if ctx.Err() != nil {
			return context.Canceled
		}
		if d.IsDir() {
			if codectx.IsIgnored(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if scanned >= scanCeiling {
			truncated = true
			return filepath.SkipDir
		}
		rel, rerr := filepath.Rel(r.Workspace, path)
		if rerr != nil {
			rel = d.Name()
		}
		if glob != "" && !matchGlob(glob, rel) {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() > 512*1024 {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 {
			return nil
		}
		if looksBinary(b) {
			return nil
		}
		scanned++
		for i, line := range strings.Split(string(b), "\n") {
			if re.MatchString(line) {
				out = append(out, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(rel), i+1, trimLine(line)))
				if len(out) >= limit {
					return filepath.SkipDir
				}
			}
		}
		return nil
	})
	if len(out) == 0 {
		return fmt.Sprintf("无匹配（已扫描 %d 个文件）", scanned), nil
	}
	if truncated {
		return fmt.Sprintf("%d 个匹配（扫描 %d 个文件，已达扫描上限 %d：缩小 path 或 glob 后重试）\n%s", len(out), scanned, scanCeiling, strings.Join(out, "\n")), nil
	}
	return fmt.Sprintf("%d 个匹配（扫描 %d 个文件）\n%s", len(out), scanned, strings.Join(out, "\n")), nil
}

// repoTree lists the workspace structure with bounded depth and entry count.
func (r *ToolRunner) repoTree(argsJSON string) (string, error) {
	var a struct {
		Path  string `json:"path"`
		Depth int    `json:"depth"`
		Max   int    `json:"max_entries"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return "", err
	}
	depth := a.Depth
	if depth <= 0 || depth > 6 {
		depth = 3
	}
	limit := a.Max
	if limit <= 0 || limit > 400 {
		limit = 240
	}
	root := r.Workspace
	if p := strings.TrimSpace(a.Path); p != "" {
		full, err := r.safePath(p)
		if err != nil {
			return "", err
		}
		root = full
	}
	var out []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		rel, rerr := filepath.Rel(r.Workspace, path)
		if rerr != nil || rel == "." {
			return nil
		}
		if info.IsDir() {
			if codectx.IsIgnored(info.Name()) {
				return filepath.SkipDir
			}
			if strings.Count(rel, string(os.PathSeparator)) >= depth {
				return filepath.SkipDir
			}
			if len(out) >= limit {
				return filepath.SkipDir
			}
			out = append(out, filepath.ToSlash(rel)+"/")
			return nil
		}
		if len(out) >= limit {
			return filepath.SkipDir
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if len(out) == 0 {
		return "（空目录）", nil
	}
	return strings.Join(out, "\n"), nil
}

// fileEdit performs an exact string replacement, which is safer than rewriting a whole file.
func (r *ToolRunner) fileEdit(argsJSON string) (string, error) {
	var a struct {
		Path       string `json:"path"`
		Old        string `json:"old"`
		New        string `json:"new"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return "", err
	}
	if a.Old == "" {
		return "", fmt.Errorf("file_edit: old text required")
	}
	full, err := r.safePath(a.Path)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	s := string(b)
	n := strings.Count(s, a.Old)
	if n == 0 {
		return "", fmt.Errorf("file_edit: old text not found in %s (re-read the file; do not guess)", filepath.ToSlash(a.Path))
	}
	if n > 1 && !a.ReplaceAll {
		return "", fmt.Errorf("file_edit: old text appears %d times in %s; add surrounding context or set replace_all", n, filepath.ToSlash(a.Path))
	}
	updated := strings.Replace(s, a.Old, a.New, -1)
	if err := os.WriteFile(full, []byte(updated), 0o644); err != nil {
		return "", err
	}
	// The edit invalidates the index entry for this file. Marking it is enough: the next graph
	// query re-parses the marked files rather than rebuilding the whole workspace, so the answer a
	// dependency question gives after an edit describes the edited code.
	r.Graph.MarkDirty(filepath.ToSlash(a.Path))
	return fmt.Sprintf("edited %s (%d occurrence%s)", filepath.ToSlash(a.Path), n, plural(n)), nil
}

// runTests executes the inferred (or explicit) verification command for the workspace.
func (r *ToolRunner) runTests(ctx context.Context, argsJSON string, profile *codectx.Profile) (string, error) {
	var a struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return "", err
	}
	cmd := strings.TrimSpace(a.Command)
	if cmd == "" {
		if profile != nil && profile.Commands.Test != "" {
			cmd = profile.Commands.Test
		} else {
			return "", fmt.Errorf("run_tests: no command given and none inferred from the workspace")
		}
	}
	timeout := time.Duration(a.Timeout) * time.Second
	if timeout <= 0 || timeout > 300*time.Second {
		timeout = 120 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(cctx, "cmd", "/C", cmd)
	} else {
		c = exec.CommandContext(cctx, "sh", "-c", cmd)
	}
	c.Dir = r.Workspace
	out, err := c.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if len(s) > 16*1024 {
		s = s[:16*1024] + "\n…(truncated)"
	}
	if err != nil {
		return fmt.Sprintf("exit non-zero: %v\n%s", err, s), nil
	}
	return "exit 0\n" + s, nil
}

// loadSkill returns one on-demand skill body from the harness.
func (r *ToolRunner) loadSkill(name string) (string, error) {
	if r.Harness == nil {
		return "", fmt.Errorf("load_skill: no harness loaded")
	}
	name = strings.TrimSpace(name)
	for _, s := range r.Harness.Skills() {
		if s.Name == name {
			return fmt.Sprintf("skill %s\n%s", s.Name, strings.TrimSpace(s.Body)), nil
		}
	}
	return "", fmt.Errorf("load_skill: unknown skill %q", name)
}

func buildMatcher(query string, regex, ignoreCase bool) (*regexp.Regexp, error) {
	pattern := query
	if !regex {
		pattern = regexp.QuoteMeta(query)
	}
	if ignoreCase {
		pattern = "(?i)" + pattern
	}
	return regexp.Compile(pattern)
}

// exact case where a bounded search matters most.
func matchGlob(glob, rel string) bool {
	glob = strings.ToLower(strings.TrimSpace(glob))
	rel = strings.ToLower(filepath.ToSlash(rel))
	if glob == "" || glob == "*" {
		return true
	}
	if !strings.Contains(glob, "/") {
		return globSegment(glob, path.Base(rel))
	}
	return globSegments(strings.Split(glob, "/"), strings.Split(rel, "/"))
}

func globSegments(pattern, parts []string) bool {
	if len(pattern) == 0 {
		return len(parts) == 0
	}
	if pattern[0] == "**" {
		if len(pattern) == 1 {
			return true
		}
		for i := 0; i <= len(parts); i++ {
			if globSegments(pattern[1:], parts[i:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	if !globSegment(pattern[0], parts[0]) {
		return false
	}
	return globSegments(pattern[1:], parts[1:])
}

func globSegment(pattern, name string) bool {
	if pattern == "" {
		return name == ""
	}
	if pattern[0] == '*' {
		for i := 0; i <= len(name); i++ {
			if globSegment(pattern[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if name == "" {
		return false
	}
	if pattern[0] != '?' && pattern[0] != name[0] {
		return false
	}
	return globSegment(pattern[1:], name[1:])
}

// codeGraph answers the question code_search cannot: who depends on a symbol. The index is
// heuristic (parsed, not type-checked), so the tool reports that rather than presenting an edge as a
// verified fact.
func (r *ToolRunner) codeGraph(argsJSON string) (string, error) {
	if r.Graph == nil {
		return "", fmt.Errorf("code_graph: no symbol index for this workspace")
	}
	// A query that follows an edit has to see the edit. Only the marked files are re-parsed, so this
	// is the cost of the change the model made, not the cost of the repository.
	if r.Graph.Dirty() > 0 {
		r.Graph.Refresh(r.Workspace)
	}
	var a struct {
		Mode  string `json:"mode"`
		Query string `json:"query"`
		Depth int    `json:"depth"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return "", err
	}
	mode := strings.ToLower(strings.TrimSpace(a.Mode))
	if mode == "" {
		mode = "definition"
	}
	depth := a.Depth
	if depth <= 0 || depth > 4 {
		depth = 2
	}
	limit := a.Limit
	if limit <= 0 || limit > 100 {
		limit = 40
	}
	query := strings.TrimSpace(a.Query)
	if query == "" && mode != "stats" && mode != "cycles" {
		return "", fmt.Errorf("code_graph: query required for mode %q", mode)
	}
	return r.Graph.Render(mode, query, depth, limit), nil
}

func looksBinary(b []byte) bool {
	limit := len(b)
	if limit > 1024 {
		limit = 1024
	}
	for i := 0; i < limit; i++ {
		if b[i] == 0 {
			return true
		}
	}
	return false
}

func trimLine(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
