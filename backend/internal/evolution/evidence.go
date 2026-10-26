package evolution

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/security"
)

// ProbeRunner executes a candidate and reports what actually happened.
//
// Prediction and execution are kept apart on purpose. A probe that only reasons about a
// trajectory is a prediction; a probe that ran something is evidence. The engine may use a
// prediction when nothing can be executed, but it must never present a prediction as a measurement.
type ProbeRunner interface {
	Probe(ctx context.Context, candidate Candidate, evidence string) Evidence
}

// Evidence is the record of one executed probe: what ran, what passed, and what was confounded.
type Evidence struct {
	Label         string   `json:"label"`
	Executed      bool     `json:"executed"`
	Graded        bool     `json:"graded"`
	TestPassed    bool     `json:"testPassed"`
	TestCommand   string   `json:"testCommand,omitempty"`
	ToolRounds    int      `json:"toolRounds"`
	Tokens        int      `json:"tokens"`
	ElapsedMs     int64    `json:"elapsedMs"`
	FilesChanged  int      `json:"filesChanged"`
	AlwaysOnChars int      `json:"alwaysOnChars"`
	Confounded    bool     `json:"confounded"`
	Observed      string   `json:"observed,omitempty"`
	Error         string   `json:"error,omitempty"`
	Steps         []string `json:"steps,omitempty"`
}

// Score turns an execution record into a comparable number. Only measured runs can score above
// the prediction floor, so a guess never outranks a measurement.
func (ev Evidence) Score() int {
	if !ev.Executed {
		return 0
	}
	score := 40
	if ev.TestPassed {
		score += 40
	}
	if ev.ToolRounds > 0 {
		bonus := ev.ToolRounds
		if bonus > 10 {
			bonus = 10
		}
		score += bonus
	}
	if ev.Confounded {
		score -= 15
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

// ExecProbe runs a candidate in an isolated directory: the candidate tree is written there, its
// scripts are executed, and the inferred test command is run against that tree. Nothing is
// credited without something having run.
type ExecProbe struct {
	// Workdir is the repository the test command runs in. Empty means an isolated temp dir.
	Workdir string
	// TestCommand overrides the command inferred from the evidence.
	TestCommand string
	// Approve gates high-risk commands; without it they are refused, not run.
	Approve        func(command string) bool
	MaxOutputChars int
	Timeout        time.Duration
}

func NewExecProbe(workdir string) *ExecProbe {
	return &ExecProbe{
		Workdir:        workdir,
		MaxOutputChars: 2000,
		Timeout:        60 * time.Second,
	}
}

// Probe executes one candidate. A candidate that fails validation is never executed: the whole
// proposal is dropped, not applied partially.
func (p *ExecProbe) Probe(ctx context.Context, c Candidate, evidence string) Evidence {
	start := time.Now()
	ev := Evidence{Label: c.Label}
	defer func() { ev.ElapsedMs = time.Since(start).Milliseconds() }()

	ev.AlwaysOnChars = c.Tree.AlwaysOnChars()
	ev.FilesChanged = len(harness.Diff(c.Base, c.Tree).Components())
	// More than one component changed at once cannot be attributed to a single edit.
	ev.Confounded = ev.FilesChanged > 1

	if problems := c.Tree.Validate(); len(problems) > 0 {
		ev.Error = strings.Join(problems, "; ")
		ev.Observed = "not executed: proposal failed validation (" + ev.Error + ")"
		return ev
	}

	dir, err := p.workdir(c)
	if err != nil {
		ev.Error = err.Error()
		ev.Observed = "not executed: " + ev.Error
		return ev
	}
	if err := c.Tree.Write(dir); err != nil {
		ev.Error = err.Error()
		ev.Observed = "not executed: " + ev.Error
		return ev
	}

	python, pythonErr := exec.LookPath("python3")
	for _, rel := range sortedRel(c.Tree) {
		if !strings.HasPrefix(rel, harness.ScriptDir+"/") {
			continue
		}
		command := "python3 " + rel
		if security.CommandDenied(command) {
			ev.Steps = append(ev.Steps, "denied: "+command)
			continue
		}
		if pythonErr != nil {
			ev.Steps = append(ev.Steps, "skipped (no python3): "+command)
			continue
		}
		out, err := runIn(ctx, python, []string{filepath.Join(dir, filepath.FromSlash(rel))}, dir, p.maxOutput(), p.timeout())
		ev.ToolRounds++
		if err != nil {
			ev.Steps = append(ev.Steps, "script "+rel+": "+firstLine(out+" "+err.Error()))
			continue
		}
		ev.Steps = append(ev.Steps, "script "+rel+": ok")
	}

	test := strings.TrimSpace(p.TestCommand)
	if test == "" {
		test = InferTestCommand(evidence)
	}
	if test != "" {
		ev.TestCommand = test
		if security.CommandDenied(test) {
			ev.Error = "test command is denied by the security policy: " + test
			ev.Observed = summarize(ev)
			return ev
		}
		if security.CommandHighRisk(test) && (p.Approve == nil || !p.Approve(test)) {
			ev.Error = "test command needs approval: " + test
			ev.Observed = summarize(ev)
			return ev
		}
		shell, err := exec.LookPath("sh")
		if err != nil {
			ev.Error = "no shell available to run the test command"
			ev.Observed = summarize(ev)
			return ev
		}
		out, err := runIn(ctx, shell, []string{"-c", test}, dir, p.maxOutput(), p.timeout())
		ev.ToolRounds++
		if err != nil {
			ev.Error = "test command failed: " + firstLine(out)
		} else {
			ev.TestPassed = true
			ev.Steps = append(ev.Steps, "test: passed")
		}
	}

	ev.Executed = ev.ToolRounds > 0
	ev.Graded = ev.Executed
	ev.Observed = summarize(ev)
	return ev
}

func (p *ExecProbe) workdir(c Candidate) (string, error) {
	if strings.TrimSpace(p.Workdir) != "" {
		base, err := filepath.Abs(filepath.Clean(p.Workdir))
		if err != nil {
			return "", err
		}
		return base, nil
	}
	dir, err := os.MkdirTemp("", "clawmind-probe-*")
	if err != nil {
		return "", err
	}
	return dir, nil
}

func (p *ExecProbe) maxOutput() int {
	if p.MaxOutputChars > 0 {
		return p.MaxOutputChars
	}
	return 2000
}

func (p *ExecProbe) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 60 * time.Second
}

func runIn(ctx context.Context, bin string, args []string, dir string, maxOutput int, timeout time.Duration) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return truncateOutput(string(out), maxOutput), err
}

// InferTestCommand reads a test command out of the recorded trajectory. It is a lookup, not a
// guess: only commands that actually appear in the evidence are used.
func InferTestCommand(evidence string) string {
	lower := strings.ToLower(evidence)
	candidates := []string{
		"go test ./...", "go test", "pytest", "npm test", "pnpm test", "yarn test",
		"cargo test", "make test", "mvn test", "gradle test", "rspec", "ctest",
	}
	best := ""
	bestAt := -1
	for _, c := range candidates {
		at := strings.Index(lower, c)
		if at >= 0 && (bestAt < 0 || at < bestAt) {
			best = c
			bestAt = at
		}
	}
	return best
}

func summarize(ev Evidence) string {
	if !ev.Executed {
		if ev.Error != "" {
			return "not executed: " + ev.Error
		}
		return "not executed: nothing ran"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("executed %d step(s)", ev.ToolRounds))
	if ev.TestCommand != "" {
		if ev.TestPassed {
			b.WriteString("; test passed")
		} else {
			b.WriteString("; test not passed")
		}
	}
	b.WriteString(fmt.Sprintf("; %d component(s) changed", ev.FilesChanged))
	if ev.Confounded {
		b.WriteString("; confounded (more than one component changed at once)")
	}
	if ev.Error != "" {
		b.WriteString("; " + ev.Error)
	}
	return b.String()
}

func sortedRel(t harness.Tree) []string {
	out := make([]string, 0, len(t))
	for rel := range t {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func truncateOutput(s string, max int) string {
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n…(output truncated)"
}

// TaskKey is the stable identity of the task a round was run for. It is derived from the evidence,
// so rerunning the same task produces the same key and can be resumed instead of repeated.
func TaskKey(evidence string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(evidence)))
	return hex.EncodeToString(sum[:])[:12]
}
