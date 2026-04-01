package agent

import (
	"encoding/json"
	"strings"
	"time"
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
	c := strings.TrimSpace(strings.ToLower(command))
	if c == "" {
		return false
	}
	// Block obvious host-wide destruction and fork bombs.
	if strings.Contains(c, ":(){") || strings.Contains(c, ": (){") {
		return true
	}
	if strings.Contains(c, "rm -rf /") || strings.Contains(c, "rm -fr /") ||
		strings.Contains(c, "rm -rf /*") || strings.Contains(c, "rm -fr /*") {
		return true
	}
	if strings.Contains(c, "> /dev/sd") || strings.Contains(c, "of=/dev/sd") {
		return true
	}
	if strings.Contains(c, "/dev/tcp/") && strings.Contains(c, "bash") {
		return true
	}
	return false
}

// ShellRiskHigh reports whether the command should require explicit user approval.
func ShellRiskHigh(command string) bool {
	c := strings.TrimSpace(strings.ToLower(command))
	if c == "" {
		return false
	}
	if ShellCommandDenied(c) {
		return true
	}
	// Destructive / network / privilege patterns (heuristic, not exhaustive).
	patterns := []string{
		"rm ", "rm\t", "rmdir", "mkfs", "dd ", "> /dev/", "chmod 777",
		"curl ", "wget ", "nc ", "netcat", "ssh ", "scp ",
		"sudo ", "su ", "chown ", "chmod +s",
		"docker ", "kubectl ",
		"apt-get", "apt install", "yum install", "dnf install",
		"powershell", "format ",
		"systemctl ", "mount ", "umount ", "iptables ", "firewall-cmd",
		"crontab ", "at ", "launchctl ",
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
