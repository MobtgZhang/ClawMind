package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
)

// MaxDelegateChars bounds what a subagent can return. A delegation is only cheap if its answer is
// small: an unbounded subagent just moves the cost from the tool call to the transcript.
const MaxDelegateChars = 4000

// delegate runs one harness subagent in its own context and returns only its text. The subagent's
// body is not part of the main conversation, so a large investigation is paid once, not on every turn.
func (r *ToolRunner) delegate(ctx context.Context, name, task string, cfg RunConfig) (string, error) {
	if r.Harness == nil {
		return "", fmt.Errorf("delegate: no harness loaded")
	}
	name = strings.TrimSpace(name)
	var sub *harness.Subagent
	for _, a := range r.Harness.Subagents() {
		if a.Name == name {
			sub = &a
			break
		}
	}
	if sub == nil {
		return "", fmt.Errorf("delegate: unknown subagent %q", name)
	}
	if strings.TrimSpace(task) == "" {
		return "", fmt.Errorf("delegate: task is empty")
	}
	if r.Client == nil {
		return "", fmt.Errorf("delegate: no model client")
	}
	res, err := r.Client.Complete(ctx, llm.CompleteParams{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
		Messages: []llm.ChatMessage{
			{Role: "system", Content: strings.TrimSpace(sub.Body)},
			{Role: "user", Content: strings.TrimSpace(task)},
		},
		Temperature: cfg.Temperature,
		TopP:        cfg.TopP,
	})
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(res.Content)
	if len(text) > MaxDelegateChars {
		text = text[:MaxDelegateChars] + "\n…(subagent answer truncated)"
	}
	if r.Hooks != nil {
		if _, herr := r.fireHooks(ctx, "SubagentStop", cfg, "delegate", map[string]any{"agent": name, "task": task}, text); herr != nil {
			return text, herr
		}
	}
	return fmt.Sprintf("subagent %s (%d tokens)\n%s", name, res.TotalTokens, text), nil
}
