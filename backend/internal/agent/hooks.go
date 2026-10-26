package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/plugin"
)

// HookSink is the narrow surface the agent needs from the plugin dispatcher. A nil sink means no
// plugin mounted a hook, which is a normal state rather than an error.
type HookSink interface {
	Fire(ctx context.Context, event string, payload plugin.HookPayload) ([]plugin.HookResult, error)
}

// fireHooks fires one event and returns the guidance the hooks produced.
//
// Exit code 2 is the documented veto: the caller stops the tool call. A hook that merely failed is
// recorded but does not block, because a broken hook script should not take the agent down with it.
func (r *ToolRunner) fireHooks(ctx context.Context, event string, cfg RunConfig, tool string, args map[string]any, output string) (string, error) {
	if r.Hooks == nil {
		return "", nil
	}
	results, err := r.Hooks.Fire(ctx, event, plugin.HookPayload{
		SessionID: cfg.SessionID,
		ProjectID: cfg.ProjectID,
		Tool:      tool,
		Args:      args,
		Output:    output,
	})
	if err != nil {
		return "", err
	}
	var guidance []string
	for _, res := range results {
		if res.Skipped {
			continue
		}
		if res.ExitCode == 2 {
			return "", fmt.Errorf("blocked by hook %s on %s: %s", res.PluginID, event, firstNonEmpty(res.Output, res.Command))
		}
		if res.Output != "" {
			guidance = append(guidance, fmt.Sprintf("[%s] %s", res.PluginID, res.Output))
		}
	}
	return strings.Join(guidance, "\n"), nil
}

// Run executes one tool and fires PreToolUse before it and PostToolUse after it.
func (r *ToolRunner) Run(ctx context.Context, name, argsJSON string, cfg RunConfig) (string, error) {
	args := map[string]any{}
	if argsJSON != "" {
		_ = json.Unmarshal([]byte(argsJSON), &args)
	}
	guidance, err := r.fireHooks(ctx, "PreToolUse", cfg, name, args, "")
	if err != nil {
		return "", err
	}
	out, err := r.runTool(ctx, name, argsJSON, cfg)
	if guidance != "" {
		if out == "" {
			out = "hook guidance:\n" + guidance
		} else {
			out = "hook guidance:\n" + guidance + "\n\n" + out
		}
	}
	if r.Hooks != nil {
		if extra, herr := r.fireHooks(ctx, "PostToolUse", cfg, name, args, out); herr != nil {
			return out, err
		} else if extra != "" && out != "" {
			out = out + "\n\nhook guidance:\n" + extra
		}
	}
	return out, err
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// stopTurn records a hook veto as the turn's answer. A veto is a decision made by a plugin, so it
// belongs in the transcript rather than only in the error stream.
func stopTurn(
	ctx context.Context,
	st MessageUpdater,
	msg *domain.Message,
	parts *[]domain.Part,
	onEvent func(domain.StreamEvent) error,
	event string,
	veto error,
) error {
	warn := fmt.Sprintf("%s hook 否决：%s", event, veto.Error())
	_ = onEvent(domain.StreamEvent{Type: domain.EventError, MessageID: msg.ID, Error: warn})
	*parts = append(*parts, domain.Part{Type: domain.PartText, Text: warn})
	_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(*parts))
	_ = onEvent(domain.StreamEvent{Type: domain.EventDone, MessageID: msg.ID})
	_ = st.TouchSession(ctx, msg.SessionID)
	return nil
}

// lastAnswerText returns the last text part produced so far: that is what a Stop hook inspects.
func lastAnswerText(parts []domain.Part) string {
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i].Type == domain.PartText && strings.TrimSpace(parts[i].Text) != "" {
			return parts[i].Text
		}
	}
	return ""
}
