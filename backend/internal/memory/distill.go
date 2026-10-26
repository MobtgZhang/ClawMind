package memory

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/mobtgzhang/clawmind/backend/internal/llm"
)

// Caller is the narrow LLM surface the distiller needs. A nil caller is not an error: the
// pipeline still distils deterministically, so memory keeps working without a model.
type Caller interface {
	Complete(ctx context.Context, p llm.CompleteParams) (*llm.CompleteResult, error)
}

// Lister is the optional read side a pipeline needs to merge a layer with what is already there.
type Lister interface {
	Rows(ctx context.Context, sessionID, projectID string, limit int) ([]Row, error)
}

// DistillConfig bounds one pipeline. Every cap exists because an uncapped layer eventually
// pushes the rest of the context out.
type DistillConfig struct {
	BaseURL          string
	APIKey           string
	Model            string
	MaxAtoms         int
	MaxScenarioChars int
	MaxPersonaChars  int
	Async            bool
}

func DefaultDistillConfig() DistillConfig {
	return DistillConfig{MaxAtoms: 12, MaxScenarioChars: 1200, MaxPersonaChars: 1500}
}

// Pipeline distils layers: raw conversation is stored as L0, then refined into L1 atoms, L2
// scenario blocks, and L3 persona. Generation is layered as well as retrieval.
type Pipeline struct {
	store  Store
	caller Caller
	cfg    DistillConfig

	mu      sync.Mutex
	pending int
	wg      sync.WaitGroup
	closed  bool
}

func NewPipeline(store Store, caller Caller, cfg DistillConfig) *Pipeline {
	if cfg.MaxAtoms <= 0 {
		cfg.MaxAtoms = DefaultDistillConfig().MaxAtoms
	}
	if cfg.MaxScenarioChars <= 0 {
		cfg.MaxScenarioChars = DefaultDistillConfig().MaxScenarioChars
	}
	if cfg.MaxPersonaChars <= 0 {
		cfg.MaxPersonaChars = DefaultDistillConfig().MaxPersonaChars
	}
	return &Pipeline{store: store, caller: caller, cfg: cfg}
}

// Observe stores the raw turn as L0 and schedules distillation. Distillation is async by design:
// a turn is not the place to pay for the work of organising it.
func (p *Pipeline) Observe(ctx context.Context, sessionID, projectID, text string) error {
	if p.store == nil {
		return nil
	}
	if err := p.store.AppendLevel(ctx, sessionID, projectID, int(LayerConversation), "conversation", text); err != nil {
		return err
	}
	if !p.cfg.Async {
		return p.Run(ctx, sessionID, projectID, text)
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return p.Run(ctx, sessionID, projectID, text)
	}
	p.pending++
	p.wg.Add(1)
	p.mu.Unlock()
	go func() {
		defer p.wg.Done()
		_ = p.Run(context.Background(), sessionID, projectID, text)
		p.mu.Lock()
		p.pending--
		p.mu.Unlock()
	}()
	return nil
}

// Pending reports queued distillation work.
func (p *Pipeline) Pending() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pending
}

// Wait blocks until queued distillation finishes.
func (p *Pipeline) Wait() {
	p.wg.Wait()
}

// Close stops accepting new work.
func (p *Pipeline) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.wg.Wait()
}

// Run performs one full L0 → L1 → L2 → L3 pass for a turn.
func (p *Pipeline) Run(ctx context.Context, sessionID, projectID, text string) error {
	atoms := p.Atoms(ctx, text)
	if len(atoms) == 0 {
		return nil
	}
	for _, atom := range atoms {
		if err := p.store.AppendLevel(ctx, sessionID, projectID, int(LayerAtom), "atom", atom); err != nil {
			return err
		}
	}
	scenario, err := p.Scenario(ctx, atoms)
	if err != nil {
		return err
	}
	if scenario != "" && projectID != "" {
		if err := p.store.AppendLevel(ctx, sessionID, projectID, int(LayerScenario), "scenario", scenario); err != nil {
			return err
		}
	}
	persona, err := p.Persona(ctx, projectID, scenario)
	if err != nil {
		return err
	}
	if persona != "" {
		if err := p.store.AppendLevel(ctx, sessionID, projectID, int(LayerPersona), "persona", persona); err != nil {
			return err
		}
	}
	return nil
}

// Atoms extracts actionable facts from a turn.
func (p *Pipeline) Atoms(ctx context.Context, text string) []string {
	if p.caller != nil {
		reply, err := p.call(ctx, atomMission, text)
		if err == nil {
			lines := cleanLines(reply, p.cfg.MaxAtoms)
			if len(lines) > 0 {
				return lines
			}
		}
	}
	return deterministicAtoms(text, p.cfg.MaxAtoms)
}

// Scenario groups atoms into a knowledge block for the project or scenario.
func (p *Pipeline) Scenario(ctx context.Context, atoms []string) (string, error) {
	if len(atoms) == 0 {
		return "", nil
	}
	if p.caller != nil {
		reply, err := p.call(ctx, scenarioMission, strings.Join(atoms, "\n"))
		if err == nil {
			if out := boundText(strings.TrimSpace(reply), p.cfg.MaxScenarioChars); out != "" {
				return out, nil
			}
		}
	}
	return strings.Join(atoms, "\n"), nil
}

// Persona merges a new scenario into the long-term profile. The profile is the layer that lets an
// agent enter a user's context quickly, so it is merged rather than appended.
func (p *Pipeline) Persona(ctx context.Context, projectID, scenario string) (string, error) {
	if scenario == "" {
		return "", nil
	}
	var existing []string
	if lister, ok := p.store.(Lister); ok {
		rows, err := lister.Rows(ctx, "", projectID, 40)
		if err == nil {
			for _, row := range rows {
				if row.Layer == LayerPersona {
					existing = append(existing, row.Content)
				}
			}
		}
	}
	if p.caller != nil {
		user := scenario
		if len(existing) > 0 {
			user = "已有画像：\n" + strings.Join(existing, "\n") + "\n\n新场景：\n" + scenario
		}
		reply, err := p.call(ctx, personaMission, user)
		if err == nil {
			if out := boundText(strings.TrimSpace(reply), p.cfg.MaxPersonaChars); out != "" {
				return out, nil
			}
		}
	}
	return deterministicPersona(existing, scenario, p.cfg.MaxPersonaChars), nil
}

func (p *Pipeline) call(ctx context.Context, mission, user string) (string, error) {
	res, err := p.caller.Complete(ctx, llm.CompleteParams{
		BaseURL:     p.cfg.BaseURL,
		APIKey:      p.cfg.APIKey,
		Model:       p.cfg.Model,
		Messages:    []llm.ChatMessage{{Role: "system", Content: mission}, {Role: "user", Content: user}},
		Temperature: 0.2,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Content), nil
}

const atomMission = `把这段对话压缩成可复用的原子事实：事实、偏好、约束、决定、事件。
每行一条，保留可泛化的流程，不要保留只帮助本次任务的具体细节。没有值得保留的内容就输出空。`

const scenarioMission = `把这些原子事实整理成围绕该项目或场景的知识块：当前状态、关键约束、已验证的结论。
用简短的 Markdown，不要逐字复述输入。`

const personaMission = `把新场景合并进长期画像：稳定模式、长期偏好、高频决定。保留已有画像中仍然成立的部分，
删除被新证据推翻的部分。`

var atomSignals = []string{
	"决定", "偏好", "约束", "必须", "不能", "不要", "因为", "所以", "使用", "选择", "修复", "验证", "风险",
	"decided", "prefer", "constraint", "must", "should", "because", "uses", "fixed", "verified", "risk",
}

func deterministicAtoms(text string, max int) []string {
	sentences := SplitSentences(text)
	var out []string
	for _, sentence := range sentences {
		lower := strings.ToLower(sentence)
		runes := []rune(sentence)
		if len(runes) < 8 || len(runes) > 200 {
			continue
		}
		for _, signal := range atomSignals {
			if strings.Contains(lower, signal) {
				out = append(out, sentence)
				break
			}
		}
		if len(out) >= max {
			break
		}
	}
	return out
}

func deterministicPersona(existing []string, scenario string, max int) string {
	counts := map[string]int{}
	for _, block := range append(existing, scenario) {
		for _, line := range SplitSentences(block) {
			counts[line]++
		}
	}
	var repeated []string
	for line, n := range counts {
		if n >= 2 {
			repeated = append(repeated, line)
		}
	}
	sort.Strings(repeated)
	if len(repeated) == 0 {
		return boundText(scenario, max)
	}
	return boundText(strings.Join(repeated, "\n"), max)
}

func cleanLines(text string, max int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= max {
			break
		}
	}
	return out
}

func boundText(s string, max int) string {
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	return string(r[:max])
}
