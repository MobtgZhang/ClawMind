package graphloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
)

// nodePlan turns the request into an executable plan and the file/symbol targets the next node
// will search for. Parsing targets out of prose is what makes the plan actionable rather than
// decorative.
func nodePlan(ctx context.Context, d *Deps, s *State) (string, error) {
	user := d.Prompts.Plan + "\n\n" + contextBlock(d, s)
	text, err := d.streamNode(ctx, domain.PartTaskFlow, user)
	if err != nil {
		return "ok", err
	}
	s.Plan = text
	s.Targets = parseTargets(text)
	if len(s.Targets) == 0 {
		s.Targets = []string{firstLine(d.LastUser)}
	}
	return decision(text, "ok"), nil
}

// nodeLocate fans out searches over the plan targets. A single search over a large repository is
// the failure mode this node exists to avoid: one query cannot cover a plan that names eight files.
func nodeLocate(ctx context.Context, d *Deps, s *State) (string, error) {
	terms := s.Targets
	if len(terms) == 0 {
		return "empty", nil
	}
	limit := d.Budget.SearchResults
	if limit <= 0 {
		limit = 60
	}
	fanout := d.Budget.Fanout
	if fanout < 1 {
		fanout = 1
	}

	type outcome struct {
		term   string
		args   string
		result string
	}
	results := make([]outcome, len(terms))
	sem := make(chan struct{}, fanout)
	var wg sync.WaitGroup
	for i, term := range terms {
		wg.Add(1)
		go func(i int, term string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				results[i] = outcome{term: term, result: "（已取消）"}
				return
			}
			args, err := json.Marshal(map[string]any{
				"query":       term,
				"max_results": limit,
			})
			if err != nil {
				results[i] = outcome{term: term, result: "错误: " + err.Error()}
				return
			}
			out, rerr := d.Tools.Run(ctx, "code_search", string(args))
			if rerr != nil {
				out = "错误: " + rerr.Error()
			}
			results[i] = outcome{term: term, args: string(args), result: out}
		}(i, term)
	}
	wg.Wait()

	found := 0
	seen := map[string]struct{}{}
	for _, r := range results {
		if r.args != "" {
			if err := d.Emitter.Tool(ctx, "code_search", r.args, r.result); err != nil {
				return "found", err
			}
		}
		for _, file := range matchFiles(r.result) {
			if _, ok := seen[file]; !ok {
				seen[file] = struct{}{}
				s.Candidates = append(s.Candidates, file)
			}
		}
		if !strings.HasPrefix(r.result, "无匹配") && !strings.HasPrefix(r.result, "错误") {
			found++
		}
		s.record(Step{Node: "locate", Title: "定位", Kind: "tool", Tool: "code_search", Args: r.term, Result: firstLine(r.result), Status: statusOf(r.result)})
	}
	if found == 0 {
		return "empty", nil
	}
	return "found", nil
}

// nodeAnalyze reads the graph for the located symbols: who calls them, what they call, which
// modules are entangled. This is the node a linear loop has no equivalent of.
func nodeAnalyze(ctx context.Context, d *Deps, s *State) (string, error) {
	if d.Graph == nil {
		return "ok", nil
	}
	seeds := s.Symbols
	if len(seeds) == 0 {
		for _, term := range s.Targets {
			for _, sym := range d.Graph.Lookup(term, 5) {
				seeds = append(seeds, sym.Name)
			}
		}
	}
	if len(seeds) > 12 {
		seeds = seeds[:12]
	}

	var b strings.Builder
	b.WriteString("## 影响面（代码图，启发式）\n\n")
	b.WriteString(d.Graph.PromptBlock(d.Budget.PromptChars) + "\n\n")
	for _, name := range seeds {
		hits := d.Graph.Callers(name, 2, 20)
		if len(hits) == 0 {
			continue
		}
		s.Symbols = append(s.Symbols, name)
		b.WriteString(fmt.Sprintf("### %s（入边 %d）\n", name, len(hits)))
		for _, h := range hits {
			b.WriteString(fmt.Sprintf("- d%d %s %s · %s:%d\n", h.Distance, h.Symbol.Kind, h.Symbol.Name, h.Symbol.File, h.Symbol.Line))
		}
		b.WriteString("\n")
	}
	if cycles := d.Graph.PackageCycles(10); len(cycles) > 0 {
		b.WriteString("### 包级循环依赖\n")
		for _, c := range cycles {
			b.WriteString("- " + strings.Join(c, " → ") + "\n")
		}
	}
	if len(s.Candidates) > 0 {
		b.WriteString("### 已定位文件\n")
		for _, f := range s.Candidates {
			b.WriteString("- " + f + "\n")
		}
	}

	text := strings.TrimSpace(b.String())
	if text == "" {
		text = "代码图没有可用符号：回退到逐文件读取。"
	}
	if err := d.Emitter.Start(ctx, domain.PartThinking); err != nil {
		return "ok", err
	}
	if err := d.Emitter.Delta(ctx, domain.PartThinking, text); err != nil {
		return "ok", err
	}
	if err := d.Emitter.End(ctx, domain.PartThinking); err != nil {
		return "ok", err
	}
	s.record(Step{Node: "analyze", Title: "影响面", Kind: "analysis", Text: text, Status: "ok"})
	return "ok", nil
}

// nodeAct is the edit node: bounded tool rounds with a repeat guard. The guard refuses the same
// call rather than ending the turn, because re-reading a file after an edit is legitimate while
// repeating a failing edit is not.
func nodeAct(ctx context.Context, d *Deps, s *State) (string, error) {
	if d.Client == nil {
		return "no_edit", fmt.Errorf("graph: no model client")
	}
	maxRounds := d.Budget.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 8
	}

	work := append([]llm.ChatMessage{}, d.History...)
	work = append(work, llm.ChatMessage{Role: "user", Content: actBlock(d, s)})

	seen := map[string]int{}
	for round := 0; round < maxRounds; round++ {
		if ctx.Err() != nil {
			break
		}
		if d.TokenBudget > 0 && s.Tokens >= d.TokenBudget {
			s.record(Step{Node: "act", Title: "改动", Kind: "decision", Status: "skipped", Decision: "token budget"})
			break
		}
		res, err := d.Client.Complete(ctx, llm.CompleteParams{
			BaseURL:     d.ClientConfig().BaseURL,
			APIKey:      d.ClientConfig().APIKey,
			Model:       d.ClientConfig().Model,
			Messages:    work,
			Tools:       d.ToolsJSON,
			ToolChoice:  "auto",
			Temperature: d.ClientConfig().Temperature,
			TopP:        d.ClientConfig().TopP,
			TopK:        d.ClientConfig().TopK,
		})
		if err != nil {
			return "no_edit", err
		}
		d.recordUsage(s, res)
		if len(res.ToolCalls) == 0 {
			break
		}
		s.Rounds++

		var tco []llm.ToolCallOut
		for _, tc := range res.ToolCalls {
			tco = append(tco, llm.ToolCallOut{
				ID:   tc.ID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: tc.Name, Arguments: tc.Arguments},
			})
		}
		work = append(work, llm.ChatMessage{Role: "assistant", ToolCalls: tco})

		for _, tc := range res.ToolCalls {
			sig := tc.Name + "\x00" + tc.Arguments
			seen[sig]++
			if seen[sig] > 1 {
				out := "错误: 该调用在本节点已执行过，先改变输入或文件内容再重试。"
				s.record(Step{Node: "act", Title: "改动", Kind: "tool", Tool: tc.Name, Args: tc.Arguments, Result: out, Status: "fail"})
				work = append(work, llm.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: out})
				if err := d.Emitter.Tool(ctx, tc.Name, tc.Arguments, out); err != nil {
					return "no_edit", err
				}
				continue
			}
			if d.Approve != nil && tc.Name == "shell_exec" {
				ok, aerr := d.Approve(ctx, tc.Name, tc.Arguments)
				if aerr != nil {
					out := "错误: " + aerr.Error()
					work = append(work, llm.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: out})
					if err := d.Emitter.Tool(ctx, tc.Name, tc.Arguments, out); err != nil {
						return "no_edit", err
					}
					continue
				}
				if !ok {
					out := "用户拒绝执行该 shell 命令。"
					work = append(work, llm.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: out})
					if err := d.Emitter.Tool(ctx, tc.Name, tc.Arguments, out); err != nil {
						return "no_edit", err
					}
					continue
				}
			}
			out, rerr := d.Tools.Run(ctx, tc.Name, tc.Arguments)
			if rerr != nil {
				out = "错误: " + rerr.Error()
			}
			s.record(Step{Node: "act", Title: "改动", Kind: "tool", Tool: tc.Name, Args: tc.Arguments, Result: firstLine(out), Status: statusOf(out)})
			// A failed edit is not a change: recording it as one would send the run to the gate and
			// let the answer claim a file was modified.
			if (tc.Name == "file_edit" || tc.Name == "file_write") && rerr == nil && statusOf(out) == "ok" {
				if p := argPath(tc.Arguments); p != "" {
					s.Edited = append(s.Edited, p)
				}
			}
			work = append(work, llm.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: out})
			if err := d.Emitter.Tool(ctx, tc.Name, tc.Arguments, out); err != nil {
				return "no_edit", err
			}
		}
	}
	if len(s.Edited) > 0 {
		return "edited", nil
	}
	return "no_edit", nil
}

// nodeVerify runs the inferred gates. A completion claim is only accepted with evidence, so this
// node records what was actually executed rather than what was intended.
func nodeVerify(ctx context.Context, d *Deps, s *State) (string, error) {
	if !d.Budget.Verify || d.Profile == nil || len(s.Edited) == 0 {
		return "none", nil
	}
	commands := []string{}
	if d.Profile.Commands.Test != "" {
		commands = append(commands, d.Profile.Commands.Test)
	}
	if d.Profile.Commands.Lint != "" {
		commands = append(commands, d.Profile.Commands.Lint)
	}
	if len(commands) == 0 {
		return "none", nil
	}

	failed := false
	for _, cmd := range commands {
		if ctx.Err() != nil {
			break
		}
		args, err := json.Marshal(map[string]any{"command": cmd})
		if err != nil {
			continue
		}
		out, rerr := d.Tools.Run(ctx, "run_tests", string(args))
		if rerr != nil {
			out = "错误: " + rerr.Error()
		}
		status := statusOf(out)
		if status == "fail" {
			failed = true
		}
		s.Evidence = append(s.Evidence, cmd+" → "+firstLine(out))
		s.record(Step{Node: "verify", Title: "验证", Kind: "gate", Tool: "run_tests", Args: cmd, Result: firstLine(out), Status: status})
		if err := d.Emitter.Tool(ctx, "run_tests", string(args), out); err != nil {
			return "fail", err
		}
		if failed {
			break
		}
	}
	if failed {
		return "fail", nil
	}
	return "pass", nil
}

// nodeReview compares the change against the plan and the evidence, and can send the run back to
// the edit node once.
func nodeReview(ctx context.Context, d *Deps, s *State) (string, error) {
	if !d.Budget.Review || d.Client == nil {
		return "ok", nil
	}
	user := d.Prompts.Review + "\n\n" + reviewBlock(d, s)
	text, err := d.streamNode(ctx, domain.PartReasoning, user)
	if err != nil {
		return "ok", err
	}
	s.Findings = append(s.Findings, text)
	return decision(text, "ok"), nil
}

// nodeAnswer writes the deliverable. It is the only node allowed to make a claim to the user, and
// it has to state whether that claim is backed by the evidence recorded earlier in the run.
func nodeAnswer(ctx context.Context, d *Deps, s *State) (string, error) {
	if d.Client == nil {
		return "done", fmt.Errorf("graph: no model client")
	}
	user := d.Prompts.Answer + "\n\n" + answerBlock(d, s)
	_, err := d.streamNode(ctx, domain.PartText, user)
	if err != nil {
		return "done", err
	}
	return "done", nil
}

// streamNode runs one node as a streamed part so the transcript keeps the same shape as the
// single-pass loop: the node's prose is visible while it is produced.
func (d *Deps) streamNode(ctx context.Context, pt domain.PartType, user string) (string, error) {
	if d.Client == nil {
		return "", fmt.Errorf("graph: no model client")
	}
	if err := d.Emitter.Start(ctx, pt); err != nil {
		return "", err
	}
	var acc strings.Builder
	err := d.Client.StreamChat(ctx, llm.StreamParams{
		BaseURL:     d.ClientConfig().BaseURL,
		APIKey:      d.ClientConfig().APIKey,
		Model:       d.ClientConfig().Model,
		Messages:    []llm.ChatMessage{{Role: "system", Content: d.System}, {Role: "user", Content: user}},
		Temperature: d.ClientConfig().Temperature,
		TopP:        d.ClientConfig().TopP,
		TopK:        d.ClientConfig().TopK,
	}, func(delta string) error {
		acc.WriteString(delta)
		if delta == "" {
			return nil
		}
		return d.Emitter.Delta(ctx, pt, delta)
	})
	if err != nil && acc.Len() == 0 {
		_ = d.Emitter.Delta(ctx, pt, "节点失败: "+err.Error())
		_ = d.Emitter.End(ctx, pt)
		return "", err
	}
	if err := d.Emitter.End(ctx, pt); err != nil {
		return "", err
	}
	return strings.TrimSpace(acc.String()), nil
}

// ClientConfig is the per-request LLM settings the graph nodes use. It is kept as a value so the
// graph does not depend on the agent's RunConfig shape.
func (d *Deps) ClientConfig() ClientSettings { return d.Settings }

// ClientSettings carries the endpoint and sampling values for one node call.
type ClientSettings struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float64
	TopP        float64
	TopK        *int
}
