package graphloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mobtgzhang/clawmind/backend/internal/codectx"
	"github.com/mobtgzhang/clawmind/backend/internal/codegraph"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
)

// ToolExecutor is the tool surface a node uses. The agent implements it so the sandbox policy,
// hook firing and human approval stay in one place instead of being reimplemented per loop.
type ToolExecutor interface {
	Run(ctx context.Context, name, argsJSON string) (string, error)
}

// Emitter writes node output into the message. Part types stay with the agent because the
// transcript shape is the agent's concern, not the graph's.
type Emitter interface {
	Start(ctx context.Context, pt domain.PartType) error
	Delta(ctx context.Context, pt domain.PartType, text string) error
	End(ctx context.Context, pt domain.PartType) error
	Tool(ctx context.Context, name, args, result string) error
}

// Deps is everything one graph run needs.
type Deps struct {
	Client      llm.Provider
	Tools       ToolExecutor
	Emitter     Emitter
	Graph       *codegraph.Index
	Profile     *codectx.Profile
	Budget      codectx.Budget
	System      string
	LastUser    string
	History     []llm.ChatMessage
	ToolsJSON   json.RawMessage
	Prompts     Prompts
	Approve     func(ctx context.Context, tool, args string) (bool, error)
	TokenBudget int
	Usage       func(tokens int)
	// Settings carries the endpoint and sampling values for one node call. It is kept as a value so
	// the graph does not depend on the agent's RunConfig shape.
	Settings ClientSettings
}

// Step is one node outcome, recorded in execution order. The trace is what makes a graph loop
// auditable: without it a branch that was taken cannot be distinguished from one that was imagined.
type Step struct {
	Node     string `json:"node"`
	Title    string `json:"title"`
	Kind     string `json:"kind"` // plan | tool | gate | review | answer | decision
	Text     string `json:"text,omitempty"`
	Tool     string `json:"tool,omitempty"`
	Args     string `json:"args,omitempty"`
	Result   string `json:"result,omitempty"`
	Status   string `json:"status"` // ok | fail | skipped
	Decision string `json:"decision,omitempty"`
}

// State is the mutable working set the nodes share: what was planned, located, changed and proven.
// A loop that cannot state what it already knows re-does the work on every pass.
type State struct {
	Visits     map[string]int `json:"visits"`
	EdgeUsed   map[string]int `json:"edgeUsed"`
	Trace      []Step         `json:"trace"`
	Plan       string         `json:"plan,omitempty"`
	Targets    []string       `json:"targets,omitempty"`
	Symbols    []string       `json:"symbols,omitempty"`
	Candidates []string       `json:"candidates,omitempty"`
	Edited     []string       `json:"edited,omitempty"`
	Evidence   []string       `json:"evidence,omitempty"`
	Findings   []string       `json:"findings,omitempty"`
	NodesRun   int            `json:"nodesRun"`
	Tokens     int            `json:"tokens"`
	Rounds     int            `json:"rounds"`
}

func newState() *State {
	return &State{Visits: map[string]int{}, EdgeUsed: map[string]int{}}
}

// NewState returns an empty working set for one run. It is exported so the caller can inspect the
// trace after the run without reaching through an unexported constructor.
func NewState() *State { return newState() }

// Node is one step of the graph. It returns the decision its output produced, which selects the
// outgoing edge.
type Node struct {
	ID    string
	Title string
	Run   func(ctx context.Context, d *Deps, s *State) (string, error)
}

// Edge is one transition. Max bounds how many times the edge may be taken: a back edge without a
// bound is not a loop, it is a hang.
type Edge struct {
	From     string
	On       string
	To       string
	Max      int
	Fallback string
}

// Graph is the executable shape: an entry node, nodes, and conditional edges.
type Graph struct {
	Entry string
	Nodes map[string]*Node
	Edges []Edge
}

// Shape builds the graph for a project class. The complex shape adds the locate, analyze and review
// nodes plus the back edges that a linear pipeline cannot express; the simple shape degenerates to
// plan → act → verify → answer, which is the same path the single-pass loop already takes.
func Shape(b codectx.Budget) *Graph {
	nodes := map[string]*Node{
		"plan":    {ID: "plan", Title: "计划", Run: nodePlan},
		"locate":  {ID: "locate", Title: "定位", Run: nodeLocate},
		"analyze": {ID: "analyze", Title: "影响面", Run: nodeAnalyze},
		"act":     {ID: "act", Title: "改动", Run: nodeAct},
		"verify":  {ID: "verify", Title: "验证", Run: nodeVerify},
		"review":  {ID: "review", Title: "评审", Run: nodeReview},
		"answer":  {ID: "answer", Title: "交付", Run: nodeAnswer},
	}

	g := &Graph{Entry: "plan", Nodes: nodes}
	if b.Class == codectx.ClassComplex {
		g.Edges = []Edge{
			{From: "plan", On: "ok", To: "locate"},
			{From: "locate", On: "found", To: "analyze"},
			{From: "locate", On: "empty", To: "plan", Max: 1, Fallback: "answer"},
			{From: "analyze", On: "ok", To: "act"},
			{From: "act", On: "edited", To: "verify"},
			{From: "act", On: "no_edit", To: "answer"},
			{From: "verify", On: "pass", To: "review"},
			{From: "verify", On: "fail", To: "act", Max: b.Reentry, Fallback: "answer"},
			{From: "verify", On: "none", To: "answer"},
			{From: "review", On: "ok", To: "answer"},
			{From: "review", On: "issues", To: "act", Max: 1, Fallback: "answer"},
			{From: "answer", On: "done", To: ""},
		}
		return g
	}

	g.Edges = []Edge{
		{From: "plan", On: "ok", To: "act"},
		{From: "act", On: "edited", To: "verify"},
		{From: "act", On: "no_edit", To: "answer"},
		{From: "verify", On: "pass", To: "answer"},
		{From: "verify", On: "fail", To: "act", Max: b.Reentry, Fallback: "answer"},
		{From: "verify", On: "none", To: "answer"},
		{From: "answer", On: "done", To: ""},
	}
	return g
}

// Run executes the graph until a node ends the run, the node budget is spent, or the context is
// cancelled. Every transition is recorded so the caller can report which path was taken.
func (g *Graph) Run(ctx context.Context, d *Deps, s *State) error {
	if s == nil {
		s = newState()
	}
	cur := g.Entry
	for cur != "" {
		if ctx.Err() != nil {
			s.record(Step{Node: cur, Title: g.Nodes[cur].Title, Kind: "decision", Status: "skipped", Decision: "cancelled"})
			return ctx.Err()
		}
		if s.NodesRun >= d.Budget.MaxNodes {
			s.record(Step{Node: cur, Title: g.Nodes[cur].Title, Kind: "decision", Status: "skipped", Decision: "node budget"})
			break
		}
		node := g.Nodes[cur]
		s.Visits[cur]++
		s.NodesRun++
		decision, err := node.Run(ctx, d, s)
		if err != nil {
			s.record(Step{Node: node.ID, Title: node.Title, Kind: "decision", Status: "fail", Decision: decision, Text: err.Error()})
			break
		}
		s.record(Step{Node: node.ID, Title: node.Title, Kind: "decision", Status: "ok", Decision: decision})
		next := g.next(cur, decision, s)
		if next == "" {
			break
		}
		cur = next
	}
	return nil
}

// next resolves the edge for a decision. A back edge that has already been taken its bound is not
// silently retried: it falls through to the honest exit, which is the difference between a loop
// that terminates and a loop that repeats the same failing call until the round cap.
func (g *Graph) next(from, decision string, s *State) string {
	for _, e := range g.Edges {
		if e.From != from || e.On != decision {
			continue
		}
		key := e.From + "->" + e.On
		s.EdgeUsed[key]++
		// Max <= 0 is forward progress: a node that is re-entered has to be able to move on again,
		// otherwise the second visit dead-ends. Only back edges carry a bound.
		if e.Max > 0 && s.EdgeUsed[key] > e.Max {
			if e.Fallback != "" {
				return e.Fallback
			}
			return ""
		}
		return e.To
	}
	if _, ok := g.Nodes["answer"]; ok && from != "answer" {
		return "answer"
	}
	return ""
}

func (s *State) record(step Step) {
	s.Trace = append(s.Trace, step)
}

// TraceBlock renders the executed path for the transcript. A graph run that only reports the answer
// hides which branch produced it, so the reader cannot tell evidence from assumption.
func (s *State) TraceBlock() string {
	if len(s.Trace) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 执行图\n\n")
	for _, st := range s.Trace {
		if st.Kind == "decision" {
			b.WriteString(fmt.Sprintf("- %s → %s（%s）\n", st.Title, st.Decision, st.Status))
			continue
		}
		b.WriteString(fmt.Sprintf("- %s · %s\n", st.Title, firstLine(st.Text)))
	}
	return strings.TrimSpace(b.String())
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	if len([]rune(s)) > 160 {
		return string([]rune(s)[:160]) + "…"
	}
	return s
}

// recordUsage accounts one completion against the turn budget. A graph run that only counts nodes
// can spend the token budget several times over, because a back edge multiplies the model calls.
// recordUsage accounts one completion against the turn budget. A graph run that only counts nodes
// can spend the token budget several times over, because a back edge multiplies the model calls.
func (d *Deps) recordUsage(s *State, res *llm.CompleteResult) {
	if res == nil {
		return
	}
	t := res.TotalTokens
	if t <= 0 {
		t = res.PromptTokens + res.CompletionTokens
	}
	if t > 0 {
		s.Tokens += t
	}
	if d.Usage != nil {
		d.Usage(t)
	}
}
