package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/mobtgzhang/clawmind/backend/internal/security"
)

// HookPayload is what a dispatcher hands a hook: the event, the tool that triggered it, and the
// result it produced. Hooks read this as data, never as instructions.
type HookPayload struct {
	Event     string         `json:"event"`
	SessionID string         `json:"sessionId,omitempty"`
	ProjectID string         `json:"projectId,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Args      map[string]any `json:"args,omitempty"`
	Output    string         `json:"output,omitempty"`
}

// HookResult is one hook's outcome. Skipped records a hook that was declared but never executed,
// so a plugin that silently does nothing is visible in the trace.
type HookResult struct {
	PluginID string `json:"pluginId"`
	Event    string `json:"event"`
	Tool     string `json:"tool,omitempty"`
	Command  string `json:"command"`
	Output   string `json:"output,omitempty"`
	Skipped  bool   `json:"skipped,omitempty"`
	Err      string `json:"error,omitempty"`
	// ExitCode 2 is the documented veto: the caller treats it as "stop this tool call". Any other
	// non-zero code is recorded as a hook failure but does not block the tool.
	ExitCode int `json:"exitCode,omitempty"`
}

// HookRunner executes one command hook. Injecting it keeps the dispatcher testable and lets a
// host refuse to run shell hooks at all.
type HookRunner func(ctx context.Context, event string, hook Hook, payload HookPayload) (string, error)

// Dispatcher fires the effective hooks for an event, in plugin mount order.
type Dispatcher struct {
	registry  *Registry
	run       HookRunner
	maxOutput int
	timeout   time.Duration
	// allowShell records whether command hooks may execute at all. It starts true and a patch layer
	// can only turn it off, so a plugin can never enable shell hooks on a host that refused them.
	allowShell bool
}

func NewDispatcher(registry *Registry) *Dispatcher {
	return &Dispatcher{
		registry:   registry,
		run:        DefaultRunner("", nil),
		maxOutput:  4096,
		timeout:    15 * time.Second,
		allowShell: true,
	}
}

// SetLimits bounds hook output and wall time from the resolved hook layer. A layer may lower a
// limit but never raise it.
func (d *Dispatcher) SetLimits(maxOutput int, timeout time.Duration) {
	if maxOutput > 0 && maxOutput < d.maxOutput {
		d.maxOutput = maxOutput
	}
	if timeout > 0 && timeout < d.timeout {
		d.timeout = timeout
	}
}

// SetAllowShell turns command hooks off. A hook that is declared but not allowed is recorded as
// skipped, so the plugin stays visible in the trace instead of quietly doing nothing.
func (d *Dispatcher) SetAllowShell(allow bool) {
	d.allowShell = allow
}

// SetRunner replaces the executor; a nil runner records hooks without running them.
func (d *Dispatcher) SetRunner(run HookRunner) {
	if run != nil {
		d.run = run
	}
}

// Registry exposes the composed plugin set the dispatcher fires from.
func (d *Dispatcher) Registry() *Registry { return d.registry }

// Fire dispatches every effective hook registered for an event. An unknown event is an error
// rather than a silent no-op, and a hook whose matcher does not match the tool is not run.
func (d *Dispatcher) Fire(ctx context.Context, event string, payload HookPayload) ([]HookResult, error) {
	if _, ok := HookEvents[event]; !ok {
		return nil, fmt.Errorf("unknown hook event %q", event)
	}
	if d.registry == nil {
		return nil, nil
	}
	payload.Event = event
	var out []HookResult
	for _, hook := range d.registry.Hooks()[event] {
		result := HookResult{Event: event, Tool: payload.Tool, Command: hook.Command}
		if !matchHook(hook, payload.Tool) {
			continue
		}
		if hook.Type != "command" {
			result.Skipped = true
			out = append(out, result)
			continue
		}
		if !d.allowShell {
			result.Skipped = true
			result.Err = "shell hooks are disabled by the hook layer"
			out = append(out, result)
			continue
		}
		if security.CommandDenied(hook.Command) {
			result.Err = "hook command is denied by the security policy"
			out = append(out, result)
			continue
		}
		if d.run == nil {
			result.Skipped = true
			out = append(out, result)
			continue
		}
		hctx, cancel := context.WithTimeout(ctx, d.timeout)
		output, err := d.run(hctx, event, hook, payload)
		cancel()
		result.Output = truncate(output, d.maxOutput)
		if err != nil {
			result.Err = err.Error()
			result.ExitCode = exitCode(err)
		}
		out = append(out, result)
	}
	return out, nil
}

// exitCode reads the process status out of an error so a veto (exit 2) survives the runner boundary.
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}

// matchHook narrows a hook by tool name: empty or "*" matches every tool, otherwise the matcher
// is an exact name or a regular expression.
func matchHook(hook Hook, tool string) bool {
	m := strings.TrimSpace(hook.Matcher)
	if m == "" || m == "*" {
		return true
	}
	if m == tool {
		return true
	}
	re, err := regexp.Compile(m)
	if err != nil {
		return false
	}
	return re.MatchString(tool)
}

// DefaultRunner executes a hook as a shell command inside dir, with the event and payload
// exposed through the environment. High-risk commands only run when approve allows them.
func DefaultRunner(dir string, approve func(command string) bool) HookRunner {
	return func(ctx context.Context, event string, hook Hook, payload HookPayload) (string, error) {
		command := strings.TrimSpace(hook.Command)
		if command == "" {
			return "", fmt.Errorf("empty hook command")
		}
		if security.CommandDenied(command) {
			return "", fmt.Errorf("hook command is denied by the security policy")
		}
		if security.CommandHighRisk(command) && (approve == nil || !approve(command)) {
			return "", fmt.Errorf("hook command needs approval: %s", command)
		}
		shell, err := exec.LookPath("sh")
		if err != nil {
			return "", fmt.Errorf("no shell available for hook command")
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		cmd := exec.CommandContext(ctx, shell, "-c", command)
		if dir != "" {
			cmd.Dir = dir
		}
		cmd.Env = append(os.Environ(),
			"CLAWMIND_HOOK_EVENT="+event,
			"CLAWMIND_HOOK_JSON="+string(body),
		)
		out, err := cmd.CombinedOutput()
		text := strings.TrimSpace(string(out))
		if err != nil {
			if text != "" {
				return text, err
			}
			return "", err
		}
		return text, nil
	}
}

func truncate(s string, max int) string {
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n…(hook output truncated)"
}
