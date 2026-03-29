package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/mobtgzhang/clawmind/backend/internal/llm"
)

// ToolRunner executes model-selected tools under workspace constraints.
type ToolRunner struct {
	Workspace string
	Client    *llm.Client
}

func (r *ToolRunner) safePath(rel string) (string, error) {
	rel = filepath.Clean(strings.TrimSpace(rel))
	if rel == "." || rel == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes workspace")
	}
	base, err := filepath.Abs(filepath.Clean(r.Workspace))
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(filepath.Join(base, rel))
	if err != nil {
		return "", err
	}
	sep := string(os.PathSeparator)
	if full == base || strings.HasPrefix(full, base+sep) {
		return full, nil
	}
	return "", fmt.Errorf("path escapes workspace")
}

// Run executes one tool by name and JSON arguments.
func (r *ToolRunner) Run(ctx context.Context, name, argsJSON string, cfg RunConfig) (string, error) {
	switch name {
	case "file_read":
		var a struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", err
		}
		full, err := r.safePath(a.Path)
		if err != nil {
			return "", err
		}
		b, err := os.ReadFile(full)
		if err != nil {
			return "", err
		}
		if len(b) > 256*1024 {
			return string(b[:256*1024]) + "\n…(truncated)", nil
		}
		return string(b), nil
	case "file_write":
		var a struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", err
		}
		full, err := r.safePath(a.Path)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(full, []byte(a.Content), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path), nil
	case "shell_exec":
		var a struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", err
		}
		cmd := a.Command
		if cmd == "" {
			return "", fmt.Errorf("empty command")
		}
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		var c *exec.Cmd
		if runtime.GOOS == "windows" {
			c = exec.CommandContext(cctx, "cmd", "/C", cmd)
		} else {
			c = exec.CommandContext(cctx, "sh", "-c", cmd)
		}
		c.Dir = r.Workspace
		out, err := c.CombinedOutput()
		s := string(out)
		if len(s) > 32*1024 {
			s = s[:32*1024] + "\n…(truncated)"
		}
		if err != nil {
			return s + "\n[error] " + err.Error(), nil
		}
		return s, nil
	case "web_fetch":
		var a struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", err
		}
		u := strings.TrimSpace(a.URL)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return "", fmt.Errorf("only http/https URLs")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", "ClawMind-Agent/1.0")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		if err != nil {
			return "", err
		}
		text := stripHTMLish(string(b))
		if len(text) > 24*1024 {
			text = text[:24*1024] + "\n…(truncated)"
		}
		return fmt.Sprintf("status %d\n%s", resp.StatusCode, text), nil
	case "task_plan":
		var a struct {
			Goal string `json:"goal"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", err
		}
		return r.llmText(ctx, cfg,
			"你只输出 Markdown 任务列表：使用 - [ ] 条目，3～8 行，不要解释。",
			"目标：\n"+a.Goal)
	case "task_summary":
		var a struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
			return "", err
		}
		body := a.Text
		if len(body) > 12000 {
			body = body[:12000] + "…"
		}
		return r.llmText(ctx, cfg,
			"将用户给出的文本总结为 3～6 条中文要点（Markdown 列表），不要开场白。",
			body)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

func (r *ToolRunner) llmText(ctx context.Context, cfg RunConfig, system, user string) (string, error) {
	if r.Client == nil {
		return "", fmt.Errorf("llm client not configured")
	}
	msgs := []llm.ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
	res, err := r.Client.Complete(ctx, llm.CompleteParams{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		Messages:    msgs,
		Temperature: minFloat(cfg.Temperature, 0.4),
		TopP:        cfg.TopP,
		TopK:        cfg.TopK,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Content), nil
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

var reTags = regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<[^>]+>`)

func stripHTMLish(s string) string {
	s = reTags.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}
