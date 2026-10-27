package agent

import (
	"context"
	"strconv"
	"strings"

	"github.com/mobtgzhang/clawmind/backend/internal/codectx"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/graphloop"
)

// graphEmitter adapts the turn's part stream to the emitter surface the graph nodes write to. The
// transcript shape stays the agent's concern; the graph only reports which node produced what.
type graphEmitter struct {
	t   *turn
	idx map[domain.PartType]int
}

func (e *graphEmitter) Start(ctx context.Context, pt domain.PartType) error {
	e.idx[pt] = e.t.appendPart(domain.Part{Type: pt, Text: ""})
	return e.t.emitStart(pt, e.idx[pt])
}

func (e *graphEmitter) Delta(ctx context.Context, pt domain.PartType, text string) error {
	idx, ok := e.idx[pt]
	if !ok {
		return nil
	}
	e.t.parts[idx].Text += text
	e.t.flush()
	return e.t.emitDelta(idx, text)
}

func (e *graphEmitter) End(ctx context.Context, pt domain.PartType) error {
	idx, ok := e.idx[pt]
	if !ok {
		return nil
	}
	return e.t.emitEnd(idx)
}

func (e *graphEmitter) Tool(ctx context.Context, name, args, result string) error {
	return e.t.toolEvent(name, args, result)
}

// runGraphTurn executes the graph shape for a complex project. It shares the tool layer, hook
// surface, approval policy and token accounting with the single-pass loop; the only difference is the
// control graph, which is what lets a failed gate send the run back to the edit node.
func runGraphTurn(t *turn) error {
	if t.client == nil {
		t.reportWarning("图循环需要模型客户端：本轮未执行。")
		return nil
	}

	emitter := &graphEmitter{t: t, idx: map[domain.PartType]int{}}
	graphIdx := t.appendPart(domain.Part{Type: domain.PartGraph, Text: ""})
	if err := t.emitStart(domain.PartGraph, graphIdx); err != nil {
		return err
	}
	// The shape is a decision the reader can see: which class was chosen, on what evidence, and
	// which caps follow from it. Without this the loop looks like an arbitrary number of steps.
	if block := classificationBlock(t.cfg.Budget); block != "" {
		t.parts[graphIdx].Text = block
		t.flush()
		if err := t.emitDelta(graphIdx, block); err != nil {
			return err
		}
	}

	d := &graphloop.Deps{
		Client:      t.client,
		Tools:       t,
		Emitter:     emitter,
		Graph:       t.cfg.Graph,
		Profile:     t.cfg.Repo,
		Budget:      t.cfg.Budget,
		System:      truncateRunes(t.system, 6000),
		LastUser:    t.lastUser,
		History:     t.history,
		ToolsJSON:   t.cfg.ToolsJSON,
		Prompts:     graphloop.DefaultPrompts(),
		TokenBudget: t.cfg.TokenBudget,
		Usage:       func(tokens int) { t.tokenAccum += tokens },
		Settings: graphloop.ClientSettings{
			BaseURL:     t.cfg.BaseURL,
			APIKey:      t.cfg.APIKey,
			Model:       t.cfg.Model,
			Temperature: t.cfg.Temperature,
			TopP:        t.cfg.TopP,
			TopK:        t.cfg.TopK,
		},
	}

	// Approval goes through the same policy the single-pass loop uses: the graph is not a place where
	// a high-risk command can be run without the human in the loop.
	if t.cfg.WaitShellApproval != nil {
		d.Approve = func(ctx context.Context, tool, args string) (bool, error) {
			cmd := ShellCommandFromArgs(args)
			policy := t.runner.policy()
			if policy.CommandDenied(cmd) {
				return false, nil
			}
			if !policy.CommandHighRisk(cmd) {
				return true, nil
			}
			if !policy.AllowApproval {
				return false, nil
			}
			return t.cfg.WaitShellApproval(ctx, ShellApprovalRequest{
				SessionID:      t.cfg.SessionID,
				MessageID:      t.msg.ID,
				ToolName:       tool,
				Arguments:      args,
				CommandSummary: truncateRunes(cmd, 240),
			})
		}
	}

	state := graphloop.NewState()
	if err := graphloop.Shape(t.cfg.Budget).Run(t.ctx, d, state); err != nil {
		if t.ctx.Err() != nil {
			return t.ctx.Err()
		}
		t.reportWarning("图循环中断：" + err.Error())
		return nil
	}

	if trace := state.TraceBlock(); trace != "" {
		t.parts[graphIdx].Text = strings.TrimSpace(t.parts[graphIdx].Text + "\n\n" + trace)
		t.flush()
		if err := t.emitDelta(graphIdx, "\n\n"+trace); err != nil {
			return err
		}
	}
	if err := t.emitEnd(graphIdx); err != nil {
		return err
	}

	t.finish()

	if t.mem != nil {
		answer := lastAnswerText(t.parts)
		if answer != "" {
			appendMemoryAfterTurn(t.mem, t.ctx, t.msg.SessionID, t.pid, t.lastUser, answer, t.cfg.Observer)
		}
	}
	return nil
}

// classificationBlock renders the classifier's decision for the transcript. A loop shape that is not
// shown cannot be audited: the reader cannot tell whether the extra nodes were earned or invented.
func classificationBlock(b codectx.Budget) string {
	var out []string
	out = append(out, "## 项目分级（"+string(b.Class)+" · 得分 "+strconv.Itoa(b.Score)+"）")
	out = append(out, "循环形状："+b.Loop+" · 节点上限 "+strconv.Itoa(b.MaxNodes)+" · 轮次上限 "+strconv.Itoa(b.MaxRounds)+" · 返工上限 "+strconv.Itoa(b.Reentry)+" · 并行度 "+strconv.Itoa(b.Fanout))
	if len(b.Reasons) > 0 {
		out = append(out, "依据："+strings.Join(b.Reasons, "；"))
	}
	return strings.Join(out, "\n")
}
