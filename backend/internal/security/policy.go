package security

import (
	"strings"
	"time"
)

// MaxShellTimeout is the ceiling a plugin layer may set for one shell call. A patch layer can
// shorten the timeout but never extend it, because a layer that could widen a limit would turn the
// security policy into a suggestion.
const MaxShellTimeout = 60 * time.Second

// Policy is the sandbox seam expressed as data: the built-in heuristics plus whatever a patch layer
// adds. The built-in patterns are a floor, not a default that a plugin can overwrite.
type Policy struct {
	// Denied and HighRisk are extra patterns on top of the built-in heuristics.
	Denied   []string
	HighRisk []string
	// ShellTimeout bounds one shell call; zero means the built-in value.
	ShellTimeout time.Duration
	// Workspace is a narrowing of the host workspace. It is only honoured when it sits inside the
	// workspace the process already has.
	Workspace string
	// AllowApproval records whether a high-risk command may be approved at all.
	AllowApproval bool
}

// DefaultPolicy is the floor: built-in heuristics only, no extra patterns.
func DefaultPolicy() Policy {
	return Policy{ShellTimeout: MaxShellTimeout, AllowApproval: true}
}

// ResolvePolicy applies extra patterns to the floor without removing any of it.
func ResolvePolicy(extra Policy) Policy {
	out := DefaultPolicy()
	out.Denied = append(out.Denied, extra.Denied...)
	out.HighRisk = append(out.HighRisk, extra.HighRisk...)
	if extra.ShellTimeout > 0 && extra.ShellTimeout < MaxShellTimeout {
		out.ShellTimeout = extra.ShellTimeout
	} else {
		out.ShellTimeout = MaxShellTimeout
	}
	out.Workspace = strings.TrimSpace(extra.Workspace)
	out.AllowApproval = extra.AllowApproval
	return out
}

// CommandDenied reports commands that are never executed, even with user approval. The built-in
// patterns always apply; a patch layer can only add to them.
func (p Policy) CommandDenied(command string) bool {
	if builtinDenied(command) {
		return true
	}
	return matches(p.Denied, command)
}

// CommandHighRisk reports commands that need explicit user approval before running.
func (p Policy) CommandHighRisk(command string) bool {
	if p.CommandDenied(command) {
		return true
	}
	if builtinHighRisk(command) {
		return true
	}
	return matches(p.HighRisk, command)
}

func matches(patterns []string, command string) bool {
	c := strings.TrimSpace(strings.ToLower(command))
	if c == "" {
		return false
	}
	for _, p := range patterns {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" && strings.Contains(c, p) {
			return true
		}
	}
	return false
}

func builtinDenied(command string) bool {
	c := strings.TrimSpace(strings.ToLower(command))
	if c == "" {
		return false
	}
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

func builtinHighRisk(command string) bool {
	c := strings.TrimSpace(strings.ToLower(command))
	if c == "" {
		return false
	}
	if builtinDenied(c) {
		return true
	}
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
