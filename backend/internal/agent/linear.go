package agent

import (
	"encoding/json"
	"strings"

	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
)

// runLinearTurn is the single-pass loop: plan → think → tool rounds → answer. Its shape is the one
// the project already had; only the shared state moved into turn so the graph loop can reuse the
// same tool layer, hook surface and approval policy rather than reimplementing them.
func runLinearTurn(t *turn) error {
	ctx, st, mem, client, cfg, onEvent := t.ctx, t.st, t.mem, t.client, t.cfg, t.onEvent
	msg, runner, prompts, lastUser, pid := t.msg, t.runner, t.prompts, t.lastUser, t.pid
	parts := &t.parts

	emitStartIdx := func(pt domain.PartType, idx int) error { return t.emitStart(pt, idx) }
	emitDeltaIdx := func(idx int, text string) error { return t.emitDelta(idx, text) }
	emitEndIdx := func(idx int) error { return t.emitEnd(idx) }

	phaseSystem := truncateRunes(t.system, 6000)

	// --- 任务流程（流式） ---
	planUser := expandLastUser(prompts.TaskFlowUser, lastUser)
	if err := streamPhaseMarkdown(ctx, client, cfg, phaseSystem, planUser, domain.PartTaskFlow, parts, msg.ID, st, emitStartIdx, emitDeltaIdx, emitEndIdx); err != nil {
		return err
	}

	// --- 思考流程（流式） ---
	thinkUser := expandLastUser(prompts.ThinkingUser, lastUser)
	if err := streamPhaseMarkdown(ctx, client, cfg, phaseSystem, thinkUser, domain.PartThinking, parts, msg.ID, st, emitStartIdx, emitDeltaIdx, emitEndIdx); err != nil {
		return err
	}

	work := t.history
	var toolsRaw json.RawMessage
	if len(cfg.ToolsJSON) > 0 {
		toolsRaw = cfg.ToolsJSON
	}

	maxTools := cfg.MaxAgentRounds
	if maxTools < 1 {
		maxTools = 8
	}
	if maxTools > 32 {
		maxTools = 32
	}
	// The project class can raise the round cap for a large repository, but it never lowers an
	// operator's configured value: a cap the runtime silently shrinks is not a budget the caller can
	// reason about.
	if cfg.Budget.MaxRounds > maxTools {
		maxTools = cfg.Budget.MaxRounds
	}

	var hitToolRoundCap bool
	var prevToolRoundSig string

	for round := 0; round < maxTools; round++ {
		if cfg.TokenBudget > 0 && t.tokenAccum > cfg.TokenBudget {
			_ = onEvent(domain.StreamEvent{Type: domain.EventError, MessageID: msg.ID, Error: "已达到本次对话的 token 预算上限（CLAWMIND_TOKEN_BUDGET）"})
			*parts = append(*parts, domain.Part{Type: domain.PartText, Text: "已停止：超出配置的 token 预算。"})
			_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(*parts))
			_ = onEvent(domain.StreamEvent{Type: domain.EventDone, MessageID: msg.ID})
			_ = st.TouchSession(ctx, msg.SessionID)
			return nil
		}
		res, err := client.Complete(ctx, llm.CompleteParams{
			BaseURL:     cfg.BaseURL,
			APIKey:      cfg.APIKey,
			Model:       cfg.Model,
			Messages:    work,
			Tools:       toolsRaw,
			ToolChoice:  "auto",
			Temperature: cfg.Temperature,
			TopP:        cfg.TopP,
			TopK:        cfg.TopK,
		})
		if err != nil {
			_ = onEvent(domain.StreamEvent{Type: domain.EventError, MessageID: msg.ID, Error: err.Error()})
			*parts = append(*parts, domain.Part{Type: domain.PartText, Text: composeFailureReply(err)})
			_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(*parts))
			_ = onEvent(domain.StreamEvent{Type: domain.EventDone, MessageID: msg.ID})
			_ = st.TouchSession(ctx, msg.SessionID)
			return nil
		}
		t.recordUsage(res)
		if len(res.ToolCalls) == 0 {
			// 模型直接文本结束：不再发起第二次补全，避免重复与额外费用。
			if strings.TrimSpace(res.Content) != "" {
				*parts = append(*parts, domain.Part{Type: domain.PartText, Text: res.Content})
				ti := len(*parts) - 1
				if err := emitStartIdx(domain.PartText, ti); err != nil {
					return err
				}
				if err := emitDeltaIdx(ti, res.Content); err != nil {
					return err
				}
				if err := emitEndIdx(ti); err != nil {
					return err
				}
				_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(*parts))
				_ = onEvent(domain.StreamEvent{Type: domain.EventDone, MessageID: msg.ID})
				_ = st.TouchSession(ctx, msg.SessionID)
				appendMemoryAfterTurn(mem, ctx, msg.SessionID, pid, lastUser, res.Content, cfg.Observer)
				return nil
			}
			break
		}

		if round == maxTools-1 {
			hitToolRoundCap = true
		}

		var sigBuilder strings.Builder
		for _, tc := range res.ToolCalls {
			sigBuilder.WriteString(tc.Name)
			sigBuilder.WriteByte(0)
			sigBuilder.WriteString(tc.Arguments)
			sigBuilder.WriteByte(30)
		}
		toolSig := sigBuilder.String()
		if toolSig != "" && toolSig == prevToolRoundSig {
			warn := "检测到连续两轮相同的工具调用，已停止以免无效循环。"
			_ = onEvent(domain.StreamEvent{Type: domain.EventError, MessageID: msg.ID, Error: warn})
			*parts = append(*parts, domain.Part{Type: domain.PartText, Text: warn})
			_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(*parts))
			_ = onEvent(domain.StreamEvent{Type: domain.EventDone, MessageID: msg.ID})
			_ = st.TouchSession(ctx, msg.SessionID)
			return nil
		}
		prevToolRoundSig = toolSig

		var tco []llm.ToolCallOut
		var editedThisRound []string
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
			_ = onEvent(domain.StreamEvent{
				Type:       domain.EventToolCall,
				MessageID:  msg.ID,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Arguments:  tc.Arguments,
			})
			var out string
			var rerr error
			if tc.Name == "shell_exec" {
				cmd := ShellCommandFromArgs(tc.Arguments)
				policy := runner.policy()
				if policy.CommandDenied(cmd) {
					out = "错误: 该命令被安全策略禁止执行（命中 shell denylist）。"
				}
				// A plugin layer that turns approval off means high-risk commands are refused rather
				// than prompted: the layer is a policy, not a suggestion the agent can decline.
				if out == "" && policy.CommandHighRisk(cmd) && !policy.AllowApproval {
					out = "错误: 该命令属于高风险且沙箱策略关闭了人工确认。"
				}
				if out == "" && policy.CommandHighRisk(cmd) && cfg.WaitShellApproval != nil {
					ok, aerr := cfg.WaitShellApproval(ctx, ShellApprovalRequest{
						SessionID:      cfg.SessionID,
						MessageID:      msg.ID,
						ToolCallID:     tc.ID,
						ToolName:       tc.Name,
						Arguments:      tc.Arguments,
						CommandSummary: truncateRunes(cmd, 240),
					})
					if aerr != nil {
						out = "错误: " + aerr.Error()
					} else if !ok {
						out = "用户拒绝执行该 shell 命令。"
					}
				}
				if out == "" {
					out, rerr = runner.Run(ctx, tc.Name, tc.Arguments, cfg)
				}
			} else {
				out, rerr = runner.Run(ctx, tc.Name, tc.Arguments, cfg)
			}
			if rerr != nil {
				out = "错误: " + rerr.Error()
			}
			if rerr != nil || strings.HasPrefix(strings.TrimSpace(out), "错误:") {
				reflexionAppend(ctx, client, cfg, &work, mem, msg.SessionID, pid, tc, out, t.recordUsage)
			}
			_ = onEvent(domain.StreamEvent{
				Type:       domain.EventToolRes,
				MessageID:  msg.ID,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Result:     out,
			})
			*parts = append(*parts, domain.Part{
				Type:       domain.PartToolCall,
				ToolCallID: tc.ID,
				Name:       tc.Name,
				Arguments:  tc.Arguments,
			})
			*parts = append(*parts, domain.Part{
				Type:       domain.PartToolResult,
				ToolCallID: tc.ID,
				Result:     out,
			})
			work = append(work, llm.ChatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    out,
			})
			if (tc.Name == "file_edit" || tc.Name == "file_write") && rerr == nil {
				if p := toolArgPath(tc.Arguments); p != "" {
					editedThisRound = append(editedThisRound, p)
				}
			}
			_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(*parts))
		}

		if gate := verifyGate(ctx, cfg, runner, editedThisRound, msg.ID, onEvent); gate != "" {
			work = append(work, llm.ChatMessage{Role: "user", Content: gate})
			if mem != nil {
				_ = mem.AppendLevel(ctx, msg.SessionID, pid, 1, "verification", truncateRunes(gate, 800))
			}
		}
	}

	if hitToolRoundCap {
		warn := "已达到工具调用轮次上限（maxAgentRounds）：本轮工具已执行，但不再继续请求模型。可在设置中提高 maxAgentRounds 或拆分任务。"
		_ = onEvent(domain.StreamEvent{Type: domain.EventError, MessageID: msg.ID, Error: warn})
		*parts = append(*parts, domain.Part{Type: domain.PartText, Text: warn})
		_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(*parts))
		_ = onEvent(domain.StreamEvent{Type: domain.EventDone, MessageID: msg.ID})
		_ = st.TouchSession(ctx, msg.SessionID)
		return nil
	}

	idx := t.appendPart(domain.Part{Type: domain.PartText, Text: ""})
	if err := emitStartIdx(domain.PartText, idx); err != nil {
		return err
	}
	var acc strings.Builder
	streamErr := client.StreamChat(ctx, llm.StreamParams{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		Messages:    work,
		Temperature: cfg.Temperature,
		TopP:        cfg.TopP,
		TopK:        cfg.TopK,
	}, func(delta string) error {
		acc.WriteString(delta)
		(*parts)[idx].Text = acc.String()
		_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(*parts))
		return emitDeltaIdx(idx, delta)
	})
	if streamErr != nil {
		_ = onEvent(domain.StreamEvent{Type: domain.EventError, MessageID: msg.ID, Error: streamErr.Error()})
		if acc.Len() == 0 {
			(*parts)[idx].Text = composeFailureReply(streamErr)
		}
	}
	if err := emitEndIdx(idx); err != nil {
		return err
	}
	_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(*parts))
	_ = onEvent(domain.StreamEvent{Type: domain.EventDone, MessageID: msg.ID})
	_ = st.TouchSession(ctx, msg.SessionID)

	if mem != nil && acc.Len() > 0 {
		appendMemoryAfterTurn(mem, ctx, msg.SessionID, pid, lastUser, acc.String(), cfg.Observer)
	}
	return nil
}

// unusedUUID keeps the import honest when the file is read in isolation.
