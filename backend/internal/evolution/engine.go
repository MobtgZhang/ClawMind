package evolution

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
	"github.com/mobtgzhang/clawmind/backend/internal/store"
)

// Caller is the narrow LLM surface the evolution engine needs.
type Caller interface {
	Complete(ctx context.Context, p llm.CompleteParams) (*llm.CompleteResult, error)
}

// Config controls one engine instance.
type Config struct {
	K                int
	Mode             string // parallel | sequential | dry
	Root             string // e.g. .clawmind/harness
	BaseURL          string
	APIKey           string
	Model            string
	MaxAlwaysOnChars int
	MaxEvidenceChars int
}

// Engine runs task-level recursion (Propose-Probe-Compose-Reflect) and keeps the
// harnessing-level recursion (the per-project experience library).
type Engine struct {
	store *store.Store
	llm   Caller
	cfg   Config
}

func New(st *store.Store, caller Caller, cfg Config) *Engine {
	if cfg.K < 1 {
		cfg.K = 1
	}
	if cfg.K > 8 {
		cfg.K = 8
	}
	if cfg.Mode == "" {
		cfg.Mode = "parallel"
	}
	if cfg.MaxEvidenceChars <= 0 {
		cfg.MaxEvidenceChars = 12000
	}
	if cfg.MaxAlwaysOnChars <= 0 {
		cfg.MaxAlwaysOnChars = 4000
	}
	return &Engine{store: st, llm: caller, cfg: cfg}
}

// Candidate is one proposed harness variant on one dimension.
type Candidate struct {
	Label    string       `json:"label"`
	Dim      string       `json:"dim"`
	Tree     harness.Tree `json:"tree"`
	Notes    string       `json:"notes,omitempty"`
	KeptBase bool         `json:"keptBase"`
	Probe    Probe        `json:"probe"`
}

// Result is one completed round.
type Result struct {
	Round      *domain.HarnessRound  `json:"round"`
	Candidates []Candidate           `json:"candidates"`
	Edits      []domain.HarnessEdit  `json:"edits"`
	Lesson     *domain.HarnessLesson `json:"lesson,omitempty"`
	Final      harness.Tree          `json:"final"`
	Chosen     string                `json:"chosen"`
}

// Dir is the on-disk harness for a scope (project id, or "global" when empty).
func (e *Engine) Dir(projectID string) string {
	scope := strings.TrimSpace(projectID)
	if scope == "" {
		scope = "global"
	}
	return filepath.Join(e.cfg.Root, scope)
}

// Load reads the current harness for a scope, seeding defaults when empty.
func (e *Engine) Load(projectID string) (harness.Tree, error) {
	t, err := harness.Read(e.Dir(projectID))
	if err != nil {
		return nil, err
	}
	if len(t) == 0 {
		t = defaultHarness()
	}
	return t, nil
}

// RunRound performs one Propose-Probe-Compose-Reflect step for a recorded task trajectory.
func (e *Engine) RunRound(ctx context.Context, projectID, sessionID, evidence string) (*Result, error) {
	base, err := e.Load(projectID)
	if err != nil {
		return nil, err
	}
	evidence = bound(evidence, e.cfg.MaxEvidenceChars)
	res := &Result{Round: &domain.HarnessRound{
		ID: uuid.NewString(), ProjectID: projectID, SessionID: sessionID,
		Mode: e.cfg.Mode, K: e.cfg.K,
	}}
	var errors []string

	checkpoint := base
	rounds := 1
	if e.cfg.Mode == "sequential" {
		rounds = e.cfg.K
	}
	for t := 1; t <= rounds; t++ {
		cands, err := e.propose(ctx, checkpoint, evidence, t)
		if err != nil {
			errors = append(errors, fmt.Sprintf("round %d: %v", t, err))
			continue
		}
		res.Candidates = append(res.Candidates, cands...)
		if len(cands) == 0 {
			continue
		}
		if err := e.probe(ctx, cands, evidence); err != nil {
			errors = append(errors, fmt.Sprintf("probe round %d: %v", t, err))
		}
		chosen, err := e.selectCandidate(ctx, cands)
		if err != nil {
			errors = append(errors, fmt.Sprintf("select round %d: %v", t, err))
		}
		if chosen != nil {
			res.Chosen = chosen.Label
			checkpoint = chosen.Tree
		}
	}

	final, err := e.compose(ctx, base, res.Candidates, evidence)
	if err != nil {
		errors = append(errors, "compose: "+err.Error())
		final = base
	}
	if problems := final.Validate(); len(problems) > 0 {
		// A proposal that fails validation is discarded entire, not partially applied.
		errors = append(errors, "validation: "+strings.Join(problems, "; "))
		final = base
	}
	if final.AlwaysOnChars() > e.cfg.MaxAlwaysOnChars {
		errors = append(errors, fmt.Sprintf("always-on text %d chars exceeds budget %d", final.AlwaysOnChars(), e.cfg.MaxAlwaysOnChars))
	}

	edits, lesson, err := e.reflect(ctx, base, final, res.Candidates, evidence)
	if err != nil {
		errors = append(errors, "reflect: "+err.Error())
	}
	res.Edits = edits
	res.Lesson = lesson
	res.Final = final
	res.Round.Errors = errors
	res.Round.Adopted = res.Chosen
	res.Round.Notes = composeNotes(res.Candidates)
	res.Round.Inventory = final.Inventory()
	res.Round.AlwaysOn = final.AlwaysOnChars()
	res.Round.KeptBase = harness.Diff(base, final).Empty()

	if err := e.persist(ctx, res); err != nil {
		return res, err
	}
	return res, nil
}

func (e *Engine) propose(ctx context.Context, base harness.Tree, evidence string, round int) ([]Candidate, error) {
	if e.cfg.Mode == "dry" || e.llm == nil {
		return dryCandidates(base, round), nil
	}
	var out []Candidate
	for _, dim := range []string{"INV", "DOM"} {
		for i := 1; i <= e.cfg.K; i++ {
			mission := ProposePrompt(dim, e.cfg.K, i, evidence)
			reply, err := e.call(ctx, mission, "当前 harness：\n"+renderTree(base)+"\n\n执行轨迹：\n"+evidence)
			if err != nil {
				return out, fmt.Errorf("propose %s#%d: %w", dim, i, err)
			}
			p := ParseProposal(reply, base)
			label := fmt.Sprintf("r%d-%s-%d", round, dim, i)
			if problems := p.Tree.Validate(); len(problems) > 0 {
				// One retry, then inherit the current harness.
				retry, rerr := e.call(ctx, mission+"\n\n上一次提案因以下规则被丢弃：\n"+strings.Join(problems, "\n"),
					"当前 harness：\n"+renderTree(base)+"\n\n执行轨迹：\n"+evidence)
				if rerr != nil {
					return out, fmt.Errorf("propose %s#%d retry: %w", dim, i, rerr)
				}
				p = ParseProposal(retry, base)
				if problems = p.Tree.Validate(); len(problems) > 0 {
					return out, fmt.Errorf("propose %s#%d invalid after retry: %s", dim, i, strings.Join(problems, "; "))
				}
			}
			out = append(out, Candidate{
				Label: label, Dim: dim, Tree: p.Tree, Notes: p.Notes, KeptBase: p.KeptBase(),
			})
		}
	}
	return out, nil
}

func (e *Engine) probe(ctx context.Context, cands []Candidate, evidence string) error {
	if e.cfg.Mode == "dry" || e.llm == nil {
		for i := range cands {
			cands[i].Probe = Probe{Label: cands[i].Label, Score: 0, Observed: "dry run: no execution", Confounded: true}
		}
		return nil
	}
	for i := range cands {
		mission := ProbePrompt(cands[i].Label, evidence)
		reply, err := e.call(ctx, mission, "候选 harness：\n"+renderTree(cands[i].Tree))
		if err != nil {
			return err
		}
		cands[i].Probe = parseProbe(reply, cands[i].Label)
	}
	return nil
}

// selectCandidate performs blind selection over anonymized ids: an id carries no information
// about which candidate produced what.
func (e *Engine) selectCandidate(ctx context.Context, cands []Candidate) (*Candidate, error) {
	if len(cands) == 0 {
		return nil, nil
	}
	if e.cfg.Mode == "dry" || e.llm == nil {
		best := cands[0]
		for _, c := range cands {
			if c.Probe.Score > best.Probe.Score {
				best = c
			}
		}
		return &best, nil
	}
	ids := assignIDs(cands)
	var b strings.Builder
	for _, anon := range sortedKeys(ids) {
		label := ids[anon]
		c := findByLabel(cands, label)
		if c == nil {
			continue
		}
		b.WriteString("## " + anon + "\n" + renderTree(c.Tree) + "\n")
		if c.Notes != "" {
			b.WriteString("notes: " + c.Notes + "\n\n")
		}
	}
	reply, err := e.call(ctx, SelectPrompt(len(cands)), b.String())
	if err != nil {
		return nil, err
	}
	anon := parseChoice(reply)
	label, ok := ids[anon]
	if !ok {
		return nil, fmt.Errorf("selection returned unknown id %q", anon)
	}
	if c := findByLabel(cands, label); c != nil {
		return c, nil
	}
	return nil, nil
}

func (e *Engine) compose(ctx context.Context, base harness.Tree, cands []Candidate, evidence string) (harness.Tree, error) {
	if e.cfg.Mode == "dry" || e.llm == nil || len(cands) == 0 {
		return mergeDeterministic(base, cands), nil
	}
	var b strings.Builder
	b.WriteString("base harness：\n" + renderTree(base) + "\n\n候选：\n")
	for _, c := range cands {
		b.WriteString(fmt.Sprintf("### %s (%s, probe score %d, confounded=%v)\n%s\n",
			c.Label, c.Dim, c.Probe.Score, c.Probe.Confounded, renderTree(c.Tree)))
		if c.Probe.Observed != "" {
			b.WriteString("observed: " + c.Probe.Observed + "\n")
		}
		if c.Notes != "" {
			b.WriteString("notes: " + c.Notes + "\n")
		}
		b.WriteString("\n")
	}
	reply, err := e.call(ctx, ComposePrompt(evidence), b.String())
	if err != nil {
		return mergeDeterministic(base, cands), err
	}
	return ParseProposal(reply, base).Tree, nil
}

func (e *Engine) reflect(ctx context.Context, base, final harness.Tree, cands []Candidate, evidence string) ([]domain.HarnessEdit, *domain.HarnessLesson, error) {
	if e.cfg.Mode == "dry" || e.llm == nil {
		return deterministicRows(base, final, cands), nil, nil
	}
	var b strings.Builder
	b.WriteString("base：\n" + renderTree(base) + "\n\n最终：\n" + renderTree(final) + "\n\n")
	for _, c := range cands {
		b.WriteString(fmt.Sprintf("候选 %s (%s): %s | observed: %s\n", c.Label, c.Dim, c.Notes, c.Probe.Observed))
	}
	reply, err := e.call(ctx, ReflectPrompt(evidence), b.String())
	if err != nil {
		return deterministicRows(base, final, cands), nil, err
	}
	rows := parseEditRows(reply)
	edits := make([]domain.HarnessEdit, 0, len(rows))
	for _, r := range rows {
		edits = append(edits, domain.HarnessEdit{
			ID: uuid.NewString(), Component: r.Component, Candidates: r.Candidates,
			Change: r.Change, Observed: r.Observed, Verdict: domain.Verdict(r.Verdict),
		})
	}
	exp := parseExperience(reply)
	if len(exp) == 0 {
		return edits, nil, nil
	}
	lesson := &domain.HarnessLesson{
		ID: uuid.NewString(), Cue: exp["cue"], Lesson: exp["lesson"],
		Evidence: exp["evidence"], Confidence: exp["confidence"],
	}
	if lesson.Confidence == "" {
		lesson.Confidence = "low"
	}
	return edits, lesson, nil
}

func (e *Engine) persist(ctx context.Context, res *Result) error {
	if e.store == nil {
		return nil
	}
	if err := e.store.CreateHarnessRound(ctx, res.Round); err != nil {
		return err
	}
	for _, edit := range res.Edits {
		edit.RoundID = res.Round.ID
	}
	if err := e.store.AddHarnessEdits(ctx, res.Round.ID, res.Edits); err != nil {
		return err
	}
	if res.Lesson != nil {
		res.Lesson.RoundID = res.Round.ID
		res.Lesson.ProjectID = res.Round.ProjectID
		if err := e.store.AddHarnessLesson(ctx, res.Lesson); err != nil {
			return err
		}
	}
	dir := e.Dir(res.Round.ProjectID)
	if err := res.Final.Write(dir); err != nil {
		return err
	}
	return writeIndex(dir, res)
}

func (e *Engine) call(ctx context.Context, mission, user string) (string, error) {
	if e.llm == nil {
		return "", fmt.Errorf("llm client not configured")
	}
	res, err := e.llm.Complete(ctx, llm.CompleteParams{
		BaseURL: e.cfg.BaseURL,
		APIKey:  e.cfg.APIKey,
		Model:   e.cfg.Model,
		Messages: []llm.ChatMessage{
			{Role: "system", Content: mission},
			{Role: "user", Content: user},
		},
		Temperature: 0.2,
		TopP:        0.9,
	})
	if err != nil {
		return "", err
	}
	slog.Info("evolution.stage", "model", e.cfg.Model, "tokens", res.TotalTokens)
	return strings.TrimSpace(res.Content), nil
}

// --- deterministic helpers (dry mode and fallbacks) ---

func dryCandidates(base harness.Tree, round int) []Candidate {
	var out []Candidate
	for _, dim := range []string{"INV", "DOM"} {
		out = append(out, Candidate{
			Label:    fmt.Sprintf("r%d-%s-1", round, dim),
			Dim:      dim,
			Tree:     copyTree(base),
			Notes:    "dry run: no model call; proposal skipped",
			KeptBase: true,
		})
	}
	return out
}

// mergeDeterministic keeps base and applies only the highest-scoring candidate, used when
// no compose stage ran.
func mergeDeterministic(base harness.Tree, cands []Candidate) harness.Tree {
	out := copyTree(base)
	if len(cands) == 0 {
		return out
	}
	best := cands[0]
	for _, c := range cands {
		if c.Probe.Score > best.Probe.Score {
			best = c
		}
	}
	for rel, content := range best.Tree {
		out[rel] = content
	}
	return out
}

func deterministicRows(base, final harness.Tree, cands []Candidate) []domain.HarnessEdit {
	d := harness.Diff(base, final)
	var rows []domain.HarnessEdit
	for _, comp := range d.Components() {
		verdict := domain.VerdictNoEffect
		for _, c := range cands {
			if harness.Diff(base, c.Tree).Contains(comp) {
				verdict = domain.VerdictConfounded
				break
			}
		}
		rows = append(rows, domain.HarnessEdit{
			ID: uuid.NewString(), Component: comp, Change: "changed in final harness",
			Observed: "no execution evidence in dry mode", Verdict: verdict,
		})
	}
	return rows
}

func assignIDs(cands []Candidate) map[string]string {
	var seed int64
	for _, c := range cands {
		for _, r := range c.Label {
			seed += int64(r)
		}
	}
	ordered := make([]string, 0, len(cands))
	for _, c := range cands {
		ordered = append(ordered, c.Label)
	}
	sort.Strings(ordered)
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(ordered), func(i, j int) { ordered[i], ordered[j] = ordered[j], ordered[i] })
	out := map[string]string{}
	for i, label := range ordered {
		out[anonID(i)] = label
	}
	return out
}

func anonID(i int) string {
	if i < 26 {
		return string(rune('A' + i))
	}
	return string(rune('A'+i/26)) + string(rune('A'+i%26))
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func findByLabel(cands []Candidate, label string) *Candidate {
	for i := range cands {
		if cands[i].Label == label {
			return &cands[i]
		}
	}
	return nil
}

func renderTree(t harness.Tree) string {
	var b strings.Builder
	for _, rel := range t.Paths() {
		b.WriteString("### " + rel + "\n" + strings.TrimSpace(t[rel]) + "\n\n")
	}
	if b.Len() == 0 {
		return "(empty harness)"
	}
	return strings.TrimSpace(b.String())
}

func copyTree(t harness.Tree) harness.Tree {
	out := harness.New()
	for k, v := range t {
		out[k] = v
	}
	return out
}

func bound(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n…（证据已截断）"
}

func composeNotes(cands []Candidate) string {
	var b strings.Builder
	for _, c := range cands {
		if c.Notes == "" {
			continue
		}
		b.WriteString(c.Label + ": " + c.Notes + "\n")
	}
	return strings.TrimSpace(b.String())
}

func defaultHarness() harness.Tree {
	return harness.Tree{
		harness.AgentsFile: `# 代码项目 agent

先定位再改动：用 code_search / repo_tree 找到相关符号与调用点，读完整上下文后再编辑。
改动后立刻运行项目推断出的测试命令验证；测试失败要先修复再继续。
不要重命名、重排或扩展用户没有要求的交付物形状。
`,
		harness.GuidanceFile: `# 工具指导

- file_edit 用于精确替换，比整文件重写更安全。
- code_search 用于跨文件定位；先搜索再读取，不要凭记忆假设文件内容。
- run_tests 运行推断出的测试命令；失败输出要完整阅读，不要盲目重试同一命令。
`,
	}
}
