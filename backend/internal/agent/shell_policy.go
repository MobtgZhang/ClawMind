package agent

import (
	"encoding/json"
	"strings"
)

// ShellApprovalRequest is sent when a high-risk shell_exec needs user confirmation.
type ShellApprovalRequest struct {
	SessionID      string
	MessageID      string
	ToolCallID     string
	ToolName       string
	Arguments      string
	CommandSummary string
}

// ShellRiskHigh reports whether the command should require explicit user approval.
func ShellRiskHigh(command string) bool {
	c := strings.TrimSpace(strings.ToLower(command))
	if c == "" {
		return false
	}
	// Destructive / network / privilege patterns (heuristic, not exhaustive).
	patterns := []string{
		"rm ", "rm\t", "rmdir", "mkfs", "dd ", "> /dev/", "chmod 777",
		"curl ", "wget ", "nc ", "netcat", "ssh ", "scp ",
		"sudo ", "su ", "chown ", "chmod +s",
		"docker ", "kubectl ",
		"apt-get", "apt install", "yum install", "dnf install",
		"powershell", "format ",
	}
	for _, p := range patterns {
		if strings.Contains(c, p) {
			return true
		}
	}
	if strings.Contains(c, "|") && (strings.Contains(c, "curl") || strings.Contains(c, "wget")) {
		return true
	}
	return false
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
