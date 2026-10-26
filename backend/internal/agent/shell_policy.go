package agent

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/mobtgzhang/clawmind/backend/internal/security"
)

// ShellExecTimeout is the maximum wall time for a single shell_exec subprocess.
const ShellExecTimeout = 60 * time.Second

// ShellApprovalRequest is sent when a high-risk shell_exec needs user confirmation.
type ShellApprovalRequest struct {
	SessionID      string
	MessageID      string
	ToolCallID     string
	ToolName       string
	Arguments      string
	CommandSummary string
}

// ShellCommandDenied reports commands that are never executed, even if the user approves a high-risk prompt.
func ShellCommandDenied(command string) bool {
	return security.CommandDenied(command)
}

// ShellRiskHigh reports whether the command should require explicit user approval.
func ShellRiskHigh(command string) bool {
	return security.CommandHighRisk(command)
}

// ShellPolicy returns the policy a runner applies. A nil policy is the built-in floor: a plugin
// layer can add patterns to it, so an unset layer never weakens the check.
func ShellPolicy(p *security.Policy) security.Policy {
	if p == nil {
		return security.DefaultPolicy()
	}
	return security.ResolvePolicy(*p)
}

// ShellCommandFromArgs parses shell_exec JSON arguments for the command string.
func ShellCommandFromArgs(argsJSON string) string {
	var a struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return ""
	}
	return strings.TrimSpace(a.Command)
}
