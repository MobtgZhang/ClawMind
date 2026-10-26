package evolution

import (
	"context"
	"fmt"
	"log/slog"
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
	// Workdir and TestCommand drive the executing probe. Without ProbeExec the engine can only
	// predict, and every probe is labelled as a prediction.
	ProbeExec   bool
	Workdir     string
	TestCommand string
	// Resume reuses a finished run with the same task + harness + mode + k instead of repeating it.
	Resume bool
	// EnforceLedger turns the budget identity into a recorded error when a round does not land on it.
	EnforceLedger bool
	// SeedSkills starts an empty scope from the software-engineering surface. Seeding writes a
	// trigger line for every skill, so any later rewrite of always-on text must restate them.
	SeedSkills bool
}

// Engine runs task-level recursion (Propose-Probe-Compose-Reflect) and keeps the
// harnessing-level recursion (the per-project experience library).
type Engine struct {
	store       *store.Store
	llm         Caller
	cfg         Config
	probeRunner ProbeRunner
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
	if !cfg.Resume {
		cfg.Resume = true
	}
	return &Engine{store: st, llm: caller, cfg: cfg}
}

// SetProbeRunner installs the executor that turns a candidate into measured evidence.
func (e *Engine) SetProbeRunner(r ProbeRunner) {
	if r != nil {
		e.probeRunner = r
	}
}

// Candidate is one proposed harness variant on one dimension.
type Candidate struct {
	Label    string       `json:"label"`
	Dim      string       `json:"dim"`
	Tree     harness.Tree `json:"tree"`
	Notes    string       `json:"notes,omitempty"`
	KeptBase bool         `json:"keptBase"`
	// Base is the harness this candidate was proposed against. Without it a change cannot be
	// attributed to a single component, so confounding cannot be detected.
	Base  harness.Tree `json:"-"`
	Probe Probe        `json:"probe"`
}

// Probe is one candidate's measured or predicted effect on the recorded trajectory.
type Probe struct {
	Label      string `json:"label"`
	Score      int    `json:"score"`
	Observed   string `json:"observed,omitempty"`
	Confounded bool   `json:"confounded"`
	Executed   bool   `json:"executed"`
	TestPassed bool   `json:"testPassed"`
	Graded     bool   `json:"graded"`
	Predicted  bool   `json:"predicted"`
}

// Result is one completed round.
type Result struct {
	Round      *domain.HarnessRound  `json:"round"`
	Candidates []Candidate           `json:"candidates"`
	Finals     []Candidate           `json:"finals"`
	Edits      []domain.HarnessEdit  `json:"edits"`
	Lesson     *domain.HarnessLesson `json:"lesson,omitempty"`
	Final      harness.Tree          `json:"final"`
	Chosen     string                `json:"chosen"`
	Ledger     *Ledger               `json:"ledger"`
	Selection  *Selection            `json:"selection"`
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
		if e.cfg.SeedSkills {
			t = harness.SeedTree()
		} else {
			t = defaultHarness()
		}
	}
	return t, nil
}

// Seed writes the software-engineering surface for a scope and returns what was added. An existing
// harness is not overwritten: seeding fills gaps, it does not replace a harness that already works.
func (e *Engine) Seed(projectID string) (harness.Tree, []string, error) {
	dir := e.Dir(projectID)
	t, err := harness.Read(dir)
	if err != nil {
		return nil, nil, err
	}
	seed := harness.SeedTree()
	var added []string
	for rel, content := range seed {
		if _, ok := t[rel]; ok {
			continue
		}
		t[rel] = content
		added = append(added, rel)
	}
	if len(added) == 0 {
		return t, nil, nil
	}
	if problems := t.Validate(); len(problems) > 0 {
		return t, added, fmt.Errorf("seeded harness is invalid: %s", strings.Join(problems, "; "))
	}
	sort.Strings(added)
	if err := t.Write(dir); err != nil {
		return t, added, err
	}
	return t, added, nil
}

// RunRound performs one Propose-Probe-Compose-Reflect step for a recorded task trajectory.
//
// The shape is fixed: k branches, each with one INV and one DOM proposal, each proposal probed,
// each branch composed into a final. Selection then chooses among the k finals, and reflection
// records what the chosen final actually did.
func (e *Engine) RunRound(ctx context.Context, projectID, sessionID, evidence string) (*Result, error) {
	base, err := e.Load(projectID)
	if err != nil {
		return nil, err
	}
	evidence = bound(evidence, e.cfg.MaxEvidenceChars)
	taskID := TaskKey(evidence)

	res := &Result{Round: &domain.HarnessRound{
		ID: uuid.NewString(), ProjectID: projectID, SessionID: sessionID,
		Mode: e.cfg.Mode, K: e.cfg.K, TaskID: taskID,
	}}
	res.Ledger = NewLedger(e.cfg.Mode, taskID, e.cfg.K)
	res.Selection = NewSelection(taskID, e.cfg.Mode, e.cfg.K)
	var errors []string

	// A run that already finished for this exact configuration is reused, not repeated. Presence
	// of output is not completion: only a recorded marker naming the same harness counts.
	if e.store != nil && e.cfg.Resume {
		mark, err := e.store.LookupRunmark(ctx, taskID, Fingerprint(base), e.cfg.Mode, e.cfg.K)
		if err != nil {
			errors = append(errors, "runmark: "+err.Error())
		}
		if mark != nil && mark.Completed {
			res.Round.Runmark = mark.ID
			res.Round.Cached = true
			res.Round.Adopted = mark.Chosen
			res.Final = base
			res.Round.Inventory = base.Inventory()
			res.Round.AlwaysOn = base.AlwaysOnChars()
			res.Round.KeptBase = true
			res.Round.Errors = errors
			res.Ledger.Session("resume", true, 1)
			res.Round.LedgerR, res.Round.LedgerP, res.Round.MeasuredR = res.Ledger.R(), res.Ledger.P(), res.Ledger.MeasuredR()
			return res, nil
		}
	}

	// k shared base rollouts: every branch starts from the same measured base.
	for i := 1; i <= e.cfg.K; i++ {
		res.Ledger.Rollout(fmt.Sprintf("base%d", i), false, false)
	}

	checkpoint := base
	for t := 1; t <= e.cfg.K; t++ {
		final, err := e.branch(ctx, checkpoint, evidence, t, res, &errors)
		if err != nil {
			errors = append(errors, fmt.Sprintf("branch %d: %v", t, err))
			continue
		}
		if final != nil {
			res.Finals = append(res.Finals, *final)
			if e.cfg.Mode == "sequential" {
				// Sequential mode carries the chosen branch forward; parallel branches stay independent.
				checkpoint = final.Tree
			}
		}
	}

	pool := res.Selection.BuildPool(SelectionPool(e.cfg.Mode, e.cfg.K, res.Finals))
	if len(pool) > 0 {
		chosen, err := e.selectCandidate(ctx, pool, res)
		if err != nil {
			errors = append(errors, "select: "+err.Error())
			res.Selection.Fallback = true
			chosen = bestByEvidence(pool)
			if chosen != nil {
				res.Selection.ChosenLabel = chosen.Label
			}
		}
		if chosen != nil {
			res.Chosen = chosen.Label
			res.Final = chosen.Tree
		}
	}

	if res.Final == nil || len(res.Final) == 0 {
		res.Final = copyTree(base)
	}
	if problems := res.Final.Validate(); len(problems) > 0 {
		// A proposal that fails validation is discarded entire, not partially applied.
		errors = append(errors, "validation: "+strings.Join(problems, "; "))
		res.Final = copyTree(base)
	}
	if res.Final.AlwaysOnChars() > e.cfg.MaxAlwaysOnChars {
		errors = append(errors, fmt.Sprintf("always-on text %d chars exceeds budget %d", res.Final.AlwaysOnChars(), e.cfg.MaxAlwaysOnChars))
	}

	edits, lesson, err := e.reflect(ctx, base, res.Final, res.Candidates, res.Finals, evidence)
	if err != nil {
		errors = append(errors, "reflect: "+err.Error())
	}
	res.Edits = edits
	res.Lesson = lesson
	res.Round.Errors = errors
	res.Round.Adopted = res.Chosen
	res.Round.Notes = composeNotes(res.Candidates)
	res.Round.Inventory = res.Final.Inventory()
	res.Round.AlwaysOn = res.Final.AlwaysOnChars()
	res.Round.KeptBase = harness.Diff(base, res.Final).Empty()
	res.Round.Pool = labels(res.Selection.Pool())
	res.Round.Dropped = res.Selection.Dropped
	res.Round.LedgerR, res.Round.LedgerP, res.Round.MeasuredR = res.Ledger.R(), res.Ledger.P(), res.Ledger.MeasuredR()

	if e.cfg.EnforceLedger && e.cfg.Mode != "dry" {
		if err := res.Ledger.Check(); err != nil {
			errors = append(errors, "ledger: "+err.Error())
			res.Round.Errors = errors
		}
	}

	if err := e.persist(ctx, res); err != nil {
		return res, err
	}
	return res, nil
}

// branch runs one Propose (INV + DOM) - Probe - Compose step and returns the final it produced.
func (e *Engine) branch(ctx context.Context, base harness.Tree, evidence string, t int, res *Result, errors *[]string) (*Candidate, error) {
	var cands []Candidate
	for _, dim := range []string{"INV", "DOM"} {
		label := fmt.Sprintf("%s%d", dim, t)
		if e.cfg.Mode == "dry" || e.llm == nil {
			c := dryCandidate(base, label, dim)
			res.Candidates = append(res.Candidates, c)
			cands = append(cands, c)
			continue
		}
		mission := ProposePrompt(dim, e.cfg.K, t, evidence)
		user := "当前 harness：\n" + renderTree(base) + "\n\n执行轨迹：\n" + evidence
		reply, err := e.call(ctx, mission, user)
		attempts := 1
		if err != nil {
			*errors = append(*errors, fmt.Sprintf("propose %s: %v", label, err))
			continue
		}
		p := ParseProposal(reply, base)
		if problems := p.Tree.Validate(); len(problems) > 0 {
			// One retry, then the proposal is dropped entire rather than applied partially.
			attempts = 2
			retry, rerr := e.call(ctx, mission+"\n\n上一次提案因以下规则被丢弃：\n"+strings.Join(problems, "\n"), user)
			if rerr != nil {
				*errors = append(*errors, fmt.Sprintf("propose %s retry: %v", label, rerr))
				continue
			}
			p = ParseProposal(retry, base)
			if problems = p.Tree.Validate(); len(problems) > 0 {
				*errors = append(*errors, fmt.Sprintf("propose %s invalid after retry: %s", label, strings.Join(problems, "; ")))
				continue
			}
		}
		res.Ledger.Session(label, false, attempts)
		c := Candidate{Label: label, Dim: dim, Tree: p.Tree, Notes: p.Notes, KeptBase: p.KeptBase(), Base: base}
		res.Candidates = append(res.Candidates, c)
		cands = append(cands, c)
	}

	for i := range cands {
		e.probeOne(ctx, &cands[i], evidence, res)
	}

	finalLabel := fmt.Sprintf("agg%d", t)
	if e.cfg.Mode != "parallel" {
		finalLabel = fmt.Sprintf("c%d", t)
	}
	if len(cands) == 0 {
		return nil, nil
	}
	res.Ledger.Session(finalLabel, false, 1)
	final, err := e.compose(ctx, base, cands, evidence)
	if err != nil {
		*errors = append(*errors, fmt.Sprintf("compose %s: %v", finalLabel, err))
		final = mergeDeterministic(base, cands)
	}
	if problems := final.Validate(); len(problems) > 0 {
		*errors = append(*errors, fmt.Sprintf("compose %s invalid: %s", finalLabel, strings.Join(problems, "; ")))
		final = copyTree(base)
	}
	f := Candidate{Label: finalLabel, Dim: "FINAL", Tree: final, Base: base, KeptBase: harness.Diff(base, final).Empty()}
	res.Ledger.Rollout(finalLabel, e.cfg.ProbeExec, false)
	return &f, nil
}

// probeOne measures a candidate. When an executor is installed the candidate really runs; when it
// is not, the result is explicitly recorded as a prediction and the ledger says so.
func (e *Engine) probeOne(ctx context.Context, c *Candidate, evidence string, res *Result) {
	if e.probeRunner != nil && e.cfg.ProbeExec {
		ev := e.probeRunner.Probe(ctx, *c, evidence)
		c.Probe = Probe{
			Label: c.Label, Score: ev.Score(), Observed: ev.Observed, Confounded: ev.Confounded,
			Executed: ev.Executed, TestPassed: ev.TestPassed, Graded: ev.Graded,
		}
		res.Ledger.Rollout(c.Label, ev.Executed, false)
		return
	}
	if e.cfg.Mode == "dry" || e.llm == nil {
		c.Probe = Probe{Label: c.Label, Score: 0, Observed: "dry run: no execution", Confounded: true}
		return
	}
	mission := ProbePrompt(c.Label, evidence)
	reply, err := e.call(ctx, mission, "候选 harness：\n"+renderTree(c.Tree))
	if err != nil {
		c.Probe = Probe{Label: c.Label, Observed: "probe failed: " + err.Error(), Confounded: true, Predicted: true}
		return
	}
	p := parseProbe(reply, c.Label)
	p.Predicted = true
	c.Probe = p
	res.Ledger.Rollout(c.Label, false, false)
}

// selectCandidate performs blind selection over anonymized ids: an id carries no information about
// which candidate produced what.
func (e *Engine) selectCandidate(ctx context.Context, pool []Candidate, res *Result) (*Candidate, error) {
	if len(pool) == 0 {
		return nil, nil
	}
	if e.cfg.Mode == "dry" || e.llm == nil {
		res.Selection.Fallback = true
		return bestByEvidence(pool), nil
	}
	ids := AssignIDs(labels(pool), res.Selection.TaskID, res.Selection.K, res.Selection.Mode)
	res.Selection.IDMap = ids
	var b strings.Builder
	for _, anon := range sortedKeys(ids) {
		c := findByLabel(pool, ids[anon])
		if c == nil {
			continue
		}
		b.WriteString("## " + anon + "\n" + renderTree(c.Tree) + "\n")
		if c.Notes != "" {
			b.WriteString("notes: " + c.Notes + "\n")
		}
		if c.Probe.Observed != "" {
			b.WriteString("observed: " + c.Probe.Observed + "\n")
		}
		b.WriteString("\n")
	}
	reply, err := e.call(ctx, SelectPrompt(len(pool)), b.String())
	if err != nil {
		return nil, err
	}
	anon := parseChoice(reply)
	why := parseWhy(reply)
	return res.Selection.Choose(anon, why)
}

// bestByEvidence is the fallback choice: measured results first, then the original order.
func bestByEvidence(pool []Candidate) *Candidate {
	ordered := OrderGradedFirst(pool)
	if len(ordered) == 0 {
		return nil
	}
	best := ordered[0]
	for _, c := range ordered {
		if c.Probe.Score > best.Probe.Score {
			best = c
		}
	}
	return &best
}

func (e *Engine) compose(ctx context.Context, base harness.Tree, cands []Candidate, evidence string) (harness.Tree, error) {
	if e.cfg.Mode == "dry" || e.llm == nil || len(cands) == 0 {
		return mergeDeterministic(base, cands), nil
	}
	var b strings.Builder
	b.WriteString("base harness：\n" + renderTree(base) + "\n\n候选：\n")
	for _, c := range cands {
		b.WriteString(fmt.Sprintf("### %s (%s, probe score %d, confounded=%v, measured=%v)\n%s\n",
			c.Label, c.Dim, c.Probe.Score, c.Probe.Confounded, c.Probe.Graded, renderTree(c.Tree)))
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

func (e *Engine) reflect(ctx context.Context, base, final harness.Tree, cands, finals []Candidate, evidence string) ([]domain.HarnessEdit, *domain.HarnessLesson, error) {
	if e.cfg.Mode == "dry" || e.llm == nil {
		return deterministicRows(base, final, cands), nil, nil
	}
	var b strings.Builder
	b.WriteString("base：\n" + renderTree(base) + "\n\n最终：\n" + renderTree(final) + "\n\n")
	for _, c := range cands {
		kind := "预测"
		if c.Probe.Graded {
			kind = "实测"
		}
		b.WriteString(fmt.Sprintf("候选 %s (%s, %s): %s | observed: %s\n", c.Label, c.Dim, kind, c.Notes, c.Probe.Observed))
	}
	if len(finals) > 0 {
		b.WriteString("\n选择：池 " + strings.Join(labels(finals), ", "))
		if len(finals) == 1 {
			b.WriteString("；最终由该分支组装")
		} else {
			b.WriteString("；盲选在这些最终产物之间进行")
		}
		b.WriteString("\n")
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
	if res.Ledger != nil {
		if err := e.store.AddHarnessLedger(ctx, res.Round.ID, res.Ledger.Mode, res.Ledger.TaskID, res.Ledger.K, ledgerRows(res.Ledger)); err != nil {
			return err
		}
	}
	dir := e.Dir(res.Round.ProjectID)
	if err := res.Final.Write(dir); err != nil {
		return err
	}
	if err := writeIndex(dir, res); err != nil {
		return err
	}
	// Completion is recorded only after the run returned normally and its output was written.
	if res.Round.TaskID != "" {
		mark := &store.Runmark{
			TaskID: res.Round.TaskID, HarnessHash: Fingerprint(res.Final), Mode: res.Round.Mode, K: res.Round.K,
			RoundID: res.Round.ID, Chosen: res.Chosen, Completed: true, Version: RunmarkVersion,
		}
		mark.ID = store.RunmarkID(mark.TaskID, mark.HarnessHash, mark.Mode, mark.K)
		if err := e.store.SaveRunmark(ctx, mark); err != nil {
			return err
		}
		res.Round.Runmark = mark.ID
	}
	return nil
}

func ledgerRows(l *Ledger) []store.LedgerEntry {
	out := make([]store.LedgerEntry, 0, len(l.Entries))
	for _, e := range l.Entries {
		out = append(out, store.LedgerEntry{Kind: e.Kind, Label: e.Label, Measured: e.Measured, Cached: e.Cached, Attempts: e.Attempts, Tokens: e.Tokens})
	}
	return out
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

func dryCandidate(base harness.Tree, label, dim string) Candidate {
	return Candidate{
		Label:    label,
		Dim:      dim,
		Tree:     copyTree(base),
		Base:     base,
		Notes:    "dry run: no model call; proposal skipped",
		KeptBase: true,
	}
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

func labels(cands []Candidate) []string {
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.Label)
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

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func parseWhy(text string) string {
	if m := reWhy.FindStringSubmatch(text); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// defaultHarness is the fallback used when a scope has no seeded harness yet.
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
