package plugin

import (
	"sort"
	"time"

	"github.com/mobtgzhang/clawmind/backend/internal/security"
)

// consumedKeys lists the patch keys the host actually reads, per seam. A patch key outside this set
// is reported by Validate rather than silently dropped: a plugin that declares config the runtime
// never reads looks like behaviour it does not have, which is exactly the failure mode a registry
// is supposed to make visible.
var consumedKeys = map[Seam][]string{
	SeamLLM:     {"model", "baseUrl", "apiKey", "temperature", "topP", "topK", "maxAgentRounds"},
	SeamMemory:  {"backend", "embeddingModel", "maxItems", "charBudget", "timeoutMs", "caps", "distillAsync"},
	SeamPrompt:  {"systemPrompt", "promptsDir", "alwaysOnMax"},
	SeamSandbox: {"workspace", "deniedPatterns", "highRiskPatterns", "shellTimeoutMs", "allowApproval"},
	SeamSession: {"maxContextTokens", "tokenBudget"},
	SeamTools:   {"toolsPath", "skillsPath"},
	SeamHooks:   {"allowShell", "maxOutputChars", "timeoutMs"},
}

// nestedKeys lists the sub-keys a nested patch value accepts. A nested value is only consumed when
// its inner key is recognised, so a typo inside caps would otherwise be dropped at boot with no
// report, which is the same failure the consumed-key check exists to prevent.
var nestedKeys = map[string][]string{
	"caps": {"l0", "l1", "l2", "l3"},
}

// NestedKeys returns the sub-keys the host reads for a nested patch key, in sorted order.
func NestedKeys(key string) []string {
	accepted := nestedKeys[key]
	if len(accepted) == 0 {
		return nil
	}
	out := append([]string{}, accepted...)
	sort.Strings(out)
	return out
}

// ConsumedKeys returns the patch keys the host reads for a seam, in sorted order.
func ConsumedKeys(seam Seam) []string {
	keys := append([]string{}, consumedKeys[seam]...)
	sort.Strings(keys)
	return keys
}

// View is the effective config for one seam. Rows apply in mount order and a row targeting an
// existing id replaces the whole config of that row, so reading a View shows which layer wins.
type View map[string]any

func (r *Registry) View(seam Seam) View {
	var rows []PatchRow
	for _, c := range r.all("row") {
		if c.row.Seam == seam {
			rows = append(rows, c.row)
		}
	}
	out := View{}
	for _, row := range ApplyPatch(nil, rows) {
		for k, v := range row.Config {
			out[k] = v
		}
	}
	return out
}

// Provenance reports which mounted plugin last wrote each key of a seam. Reading a View shows the
// winning value but not who set it, and a settings page that hides the author cannot explain why a
// saved value is not taking effect.
func (r *Registry) Provenance(seam Seam) map[string]string {
	out := map[string]string{}
	for _, c := range r.all("row") {
		if c.row.Seam != seam {
			continue
		}
		for key := range c.row.Config {
			out[key] = c.pluginID
		}
	}
	return out
}

func (v View) String(key string) string {
	if s, ok := v[key].(string); ok {
		return s
	}
	return ""
}

func (v View) Int(key string) int {
	switch n := v[key].(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func (v View) Float(key string) float64 {
	if n, ok := v[key].(float64); ok {
		return n
	}
	return 0
}

func (v View) Bool(key string) bool {
	if b, ok := v[key].(bool); ok {
		return b
	}
	return false
}

func (v View) List(key string) []string {
	list, ok := v[key].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range list {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (v View) Nested(key string) map[string]int {
	nested, ok := v[key].(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]int{}
	for k, val := range nested {
		switch n := val.(type) {
		case int:
			out[k] = n
		case float64:
			out[k] = int(n)
		}
	}
	return out
}

func (v View) DurationMs(key string) time.Duration {
	n := v.Int(key)
	if n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Millisecond
}

// Runtime is the effect the patch layers have on the running process. Only keys the host reads are
// represented here, so a seam that is declared but never consumed cannot look like a feature.
type Runtime struct {
	LLM     LLMRuntime
	Memory  MemoryRuntime
	Prompt  PromptRuntime
	Sandbox SandboxRuntime
	Session SessionRuntime
	Tools   ToolsRuntime
	Hooks   HooksRuntime
}

// Optional numbers are pointers so an explicit zero stays distinguishable from "not set".
type LLMRuntime struct {
	Model          string
	BaseURL        string
	APIKey         string
	Temperature    *float64
	TopP           *float64
	TopK           *int
	MaxAgentRounds int
}

type MemoryRuntime struct {
	Backend        string
	EmbeddingModel string
	MaxItems       int
	CharBudget     int
	Timeout        time.Duration
	Caps           map[string]int
	DistillAsync   *bool
}

type PromptRuntime struct {
	SystemPrompt string
	PromptsDir   string
	AlwaysOnMax  int
}

type SandboxRuntime struct {
	Workspace     string
	Denied        []string
	HighRisk      []string
	ShellTimeout  time.Duration
	AllowApproval bool
}

type SessionRuntime struct {
	MaxContextTokens int
	TokenBudget      int
}

type ToolsRuntime struct {
	ToolsPath  string
	SkillsPath string
}

type HooksRuntime struct {
	AllowShell     *bool
	MaxOutputChars int
	Timeout        time.Duration
}

// Resolve reads the effective patch layers once, at boot, into the values the host applies.
func Resolve(r *Registry) Runtime {
	out := Runtime{}
	if r == nil {
		return out
	}
	llm := r.View(SeamLLM)
	out.LLM.Model = llm.String("model")
	out.LLM.BaseURL = llm.String("baseUrl")
	out.LLM.APIKey = llm.String("apiKey")
	out.LLM.MaxAgentRounds = llm.Int("maxAgentRounds")
	if _, ok := llm["temperature"]; ok {
		v := llm.Float("temperature")
		out.LLM.Temperature = &v
	}
	if _, ok := llm["topP"]; ok {
		v := llm.Float("topP")
		out.LLM.TopP = &v
	}
	if _, ok := llm["topK"]; ok {
		v := llm.Int("topK")
		out.LLM.TopK = &v
	}

	mem := r.View(SeamMemory)
	out.Memory.Backend = mem.String("backend")
	out.Memory.EmbeddingModel = mem.String("embeddingModel")
	out.Memory.MaxItems = mem.Int("maxItems")
	out.Memory.CharBudget = mem.Int("charBudget")
	out.Memory.Timeout = mem.DurationMs("timeoutMs")
	out.Memory.Caps = mem.Nested("caps")
	if _, ok := mem["distillAsync"]; ok {
		v := mem.Bool("distillAsync")
		out.Memory.DistillAsync = &v
	}

	prompt := r.View(SeamPrompt)
	out.Prompt.SystemPrompt = prompt.String("systemPrompt")
	out.Prompt.PromptsDir = prompt.String("promptsDir")
	out.Prompt.AlwaysOnMax = prompt.Int("alwaysOnMax")

	sandbox := r.View(SeamSandbox)
	out.Sandbox.Workspace = sandbox.String("workspace")
	out.Sandbox.Denied = sandbox.List("deniedPatterns")
	out.Sandbox.HighRisk = sandbox.List("highRiskPatterns")
	out.Sandbox.ShellTimeout = sandbox.DurationMs("shellTimeoutMs")
	out.Sandbox.AllowApproval = sandbox.Bool("allowApproval")

	session := r.View(SeamSession)
	out.Session.MaxContextTokens = session.Int("maxContextTokens")
	out.Session.TokenBudget = session.Int("tokenBudget")

	tools := r.View(SeamTools)
	out.Tools.ToolsPath = tools.String("toolsPath")
	out.Tools.SkillsPath = tools.String("skillsPath")

	hooks := r.View(SeamHooks)
	if _, ok := hooks["allowShell"]; ok {
		v := hooks.Bool("allowShell")
		out.Hooks.AllowShell = &v
	}
	out.Hooks.MaxOutputChars = hooks.Int("maxOutputChars")
	out.Hooks.Timeout = hooks.DurationMs("timeoutMs")
	return out
}

// Policy turns the sandbox layer into the policy the shell executor uses. The built-in heuristics
// stay as the floor: this adds patterns and can only shorten a limit.
func (s SandboxRuntime) Policy() security.Policy {
	return security.ResolvePolicy(security.Policy{
		Denied:        s.Denied,
		HighRisk:      s.HighRisk,
		ShellTimeout:  s.ShellTimeout,
		Workspace:     s.Workspace,
		AllowApproval: s.AllowApproval,
	})
}
