package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/mobtgzhang/clawmind/backend/internal/codectx"
)

// MaxSearchResults bounds one code_search reply so it does not crowd the context.
const MaxSearchResults = 60

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
func (r *ToolRunner) codeSearch(ctx context.Context, argsJSON string) (string, error) {
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
	if limit <= 0 || limit > MaxSearchResults {
		limit = MaxSearchResults
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
		if scanned >= 3000 {
			return filepath.SkipDir
		}
		if glob != "" && !matchGlob(glob, d.Name()) {
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
		rel, rerr := filepath.Rel(r.Workspace, path)
		if rerr != nil {
			rel = path
		}
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

func matchGlob(glob, name string) bool {
	glob = strings.ToLower(strings.TrimSpace(glob))
	name = strings.ToLower(name)
	if glob == "*" || glob == "" {
		return true
	}
	if strings.HasPrefix(glob, "*") {
		return strings.HasSuffix(name, strings.TrimPrefix(glob, "*"))
	}
	return glob == name
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
