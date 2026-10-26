package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
)

func toolArgPath(argsJSON string) string {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return ""
	}
	return filepath.ToSlash(strings.TrimSpace(a.Path))
}

// verifyGate runs the inferred verification command after a round that changed files: a claimed
// fix is checked against the project instead of asserted, and the result is fed back into the run.
func verifyGate(ctx context.Context, cfg RunConfig, runner *ToolRunner, edited []string, msgID string, onEvent func(domain.StreamEvent) error) string {
	if !cfg.AutoVerify || len(edited) == 0 || cfg.Repo == nil || cfg.Repo.Commands.Test == "" {
		return ""
	}
	args, err := json.Marshal(map[string]any{"command": cfg.Repo.Commands.Test})
	if err != nil {
		return ""
	}
	out, rerr := runner.Run(ctx, "run_tests", string(args), cfg)
	if rerr != nil {
		out = "错误: " + rerr.Error()
	}
	callID := uuid.NewString()
	_ = onEvent(domain.StreamEvent{Type: domain.EventToolCall, MessageID: msgID, ToolCallID: callID, ToolName: "run_tests", Arguments: string(args)})
	_ = onEvent(domain.StreamEvent{Type: domain.EventToolRes, MessageID: msgID, ToolCallID: callID, ToolName: "run_tests", Result: out})
	return "（自动验证）改动 " + strings.Join(edited, ", ") + " 后运行 " + cfg.Repo.Commands.Test + "：\n" + out
}
