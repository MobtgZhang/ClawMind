package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
	"github.com/mobtgzhang/clawmind/backend/internal/memory"
	"github.com/mobtgzhang/clawmind/backend/internal/store"
	"github.com/mobtgzhang/clawmind/backend/internal/thread"
)

// MessageUpdater is the store subset the agent needs.
type MessageUpdater interface {
	GetMessage(ctx context.Context, id string) (*domain.Message, error)
	ListMessages(ctx context.Context, sessionID string) ([]domain.Message, error)
	UpdateMessageParts(ctx context.Context, id string, parts []domain.Part) error
	TouchSession(ctx context.Context, id string) error
	GetSession(ctx context.Context, id string) (*domain.Session, error)
}

// RunStream generates assistant content with task → thinking → tools → streamed answer.
func RunStream(
	ctx context.Context,
	st MessageUpdater,
	mem memory.Store,
	client *llm.Client,
	cfg RunConfig,
	onEvent func(domain.StreamEvent) error,
) error {
	msg, err := st.GetMessage(ctx, cfg.AssistantMessageID)
	if err != nil || msg == nil {
		return err
	}
	history, err := st.ListMessages(ctx, msg.SessionID)
	if err != nil {
		return err
	}
	sess, _ := st.GetSession(ctx, msg.SessionID)
	choice := thread.ParseSiblingChoice(nil)
	if sess != nil {
		choice = thread.ParseSiblingChoice(sess.Metadata)
	}
	path := thread.BuildPathForSession(history, choice)
	var upto []domain.Message
	if msg.ParentMessageID != nil && *msg.ParentMessageID != "" {
		upto = thread.PrefixThroughUser(path, *msg.ParentMessageID)
	}
	if len(upto) == 0 {
		for _, m := range history {
			if m.ID == cfg.AssistantMessageID {
				break
			}
			upto = append(upto, m)
		}
	}
	upto = TrimHistoryByAssistantRounds(upto, cfg.MaxAgentRounds)
	upto = TrimHistoryByEstimatedTokens(upto, cfg.MaxContextTokens)

	lastUser := lastUserText(upto)
	pid := ""
	if sess != nil && sess.ProjectID != nil {
		pid = *sess.ProjectID
	}

	system := cfg.SystemPrompt
	system += "\n\n" + systemRuntimeContextBlock(time.Now())
	if mem != nil {
		memQuery := truncateRunes(strings.TrimSpace(lastUser), 512)
		lines, _ := mem.RetrieveLevels(ctx, msg.SessionID, pid, memQuery)
		if len(lines) > 0 {
			system += "\n\n多级记忆（L3 全局 → L2 项目 → L1 会话）：\n- " + strings.Join(lines, "\n- ")
		}
	}
	prompts := loadPromptBundle(cfg.PromptsDir)
	system += "\n\n" + prompts.SystemSuffix

	runner := &ToolRunner{Workspace: cfg.Workspace, Client: client, MCPCall: cfg.MCPCall}

	var parts []domain.Part

	emitStartIdx := func(pt domain.PartType, idx int) error {
		return onEvent(domain.StreamEvent{Type: domain.EventPartStart, MessageID: msg.ID, PartIndex: idx, PartType: pt})
	}
	emitDeltaIdx := func(idx int, t string) error {
		if t == "" {
			return nil
		}
		return onEvent(domain.StreamEvent{Type: domain.EventDelta, MessageID: msg.ID, PartIndex: idx, Text: t})
	}
	emitEndIdx := func(idx int) error {
		return onEvent(domain.StreamEvent{Type: domain.EventPartEnd, MessageID: msg.ID, PartIndex: idx})
	}

	phaseSystem := truncateRunes(system, 6000)

	// --- 任务流程（流式） ---
	planUser := expandLastUser(prompts.TaskFlowUser, lastUser)
	if err := streamPhaseMarkdown(ctx, client, cfg, phaseSystem, planUser, domain.PartTaskFlow, &parts, msg.ID, st, emitStartIdx, emitDeltaIdx, emitEndIdx); err != nil {
		return err
	}

	// --- 思考流程（流式） ---
	thinkUser := expandLastUser(prompts.ThinkingUser, lastUser)
	if err := streamPhaseMarkdown(ctx, client, cfg, phaseSystem, thinkUser, domain.PartThinking, &parts, msg.ID, st, emitStartIdx, emitDeltaIdx, emitEndIdx); err != nil {
		return err
	}

	work := llm.ToChatMessages(system, upto)
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

	var tokenAccum int
	recordUsage := func(res *llm.CompleteResult) {
		if res == nil {
			return
		}
		t := res.TotalTokens
		if t <= 0 {
			t = res.PromptTokens + res.CompletionTokens
		}
		tokenAccum += t
		slog.Info("llm.completion_usage",
			"prompt_tokens", res.PromptTokens,
			"completion_tokens", res.CompletionTokens,
			"total_tokens", t,
			"accum", tokenAccum,
			"assistant_message_id", cfg.AssistantMessageID,
		)
	}

	for round := 0; round < maxTools; round++ {
		if cfg.TokenBudget > 0 && tokenAccum > cfg.TokenBudget {
			_ = onEvent(domain.StreamEvent{Type: domain.EventError, MessageID: msg.ID, Error: "已达到本次对话的 token 预算上限（CLAWMIND_TOKEN_BUDGET）"})
			parts = append(parts, domain.Part{Type: domain.PartText, Text: "已停止：超出配置的 token 预算。"})
			_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(parts))
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
			parts = append(parts, domain.Part{Type: domain.PartText, Text: composeFailureReply(err)})
			_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(parts))
			_ = onEvent(domain.StreamEvent{Type: domain.EventDone, MessageID: msg.ID})
			_ = st.TouchSession(ctx, msg.SessionID)
			return nil
		}
		recordUsage(res)
		if len(res.ToolCalls) == 0 {
			// 模型直接文本结束：不再发起第二次补全，避免重复与额外费用。
			if strings.TrimSpace(res.Content) != "" {
				parts = append(parts, domain.Part{Type: domain.PartText, Text: res.Content})
				ti := len(parts) - 1
				if err := emitStartIdx(domain.PartText, ti); err != nil {
					return err
				}
				if err := emitDeltaIdx(ti, res.Content); err != nil {
					return err
				}
				if err := emitEndIdx(ti); err != nil {
					return err
				}
				_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(parts))
				_ = onEvent(domain.StreamEvent{Type: domain.EventDone, MessageID: msg.ID})
				_ = st.TouchSession(ctx, msg.SessionID)
				appendMemoryAfterTurn(mem, ctx, msg.SessionID, pid, lastUser, res.Content)
				return nil
			}
			break
		}

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
				if ShellRiskHigh(cmd) && cfg.WaitShellApproval != nil {
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
					t0 := time.Now()
					out, rerr = runner.Run(ctx, tc.Name, tc.Arguments, cfg)
					slog.Info("agent.tool", "name", tc.Name, "ms", time.Since(t0).Milliseconds(), "message_id", msg.ID)
				}
			} else {
				t0 := time.Now()
				out, rerr = runner.Run(ctx, tc.Name, tc.Arguments, cfg)
				slog.Info("agent.tool", "name", tc.Name, "ms", time.Since(t0).Milliseconds(), "message_id", msg.ID)
			}
			if rerr != nil {
				out = "错误: " + rerr.Error()
			}
			if rerr != nil || strings.HasPrefix(strings.TrimSpace(out), "错误:") {
				reflexionAppend(ctx, client, cfg, &work, mem, msg.SessionID, pid, tc, out, recordUsage)
			}
			_ = onEvent(domain.StreamEvent{
				Type:       domain.EventToolRes,
				MessageID:  msg.ID,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Result:     out,
			})
			parts = append(parts, domain.Part{
				Type:       domain.PartToolCall,
				ToolCallID: tc.ID,
				Name:       tc.Name,
				Arguments:  tc.Arguments,
			})
			parts = append(parts, domain.Part{
				Type:       domain.PartToolResult,
				ToolCallID: tc.ID,
				Result:     out,
			})
			work = append(work, llm.ChatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    out,
			})
			_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(parts))
		}
	}

	parts = append(parts, domain.Part{Type: domain.PartText, Text: ""})
	textIdx := len(parts) - 1
	if err := emitStartIdx(domain.PartText, textIdx); err != nil {
		return err
	}
	var acc strings.Builder
	streamParams := llm.StreamParams{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		Messages:    work,
		Temperature: cfg.Temperature,
		TopP:        cfg.TopP,
		TopK:        cfg.TopK,
	}
	streamErr := client.StreamChat(ctx, streamParams, func(delta string) error {
		acc.WriteString(delta)
		parts[textIdx].Text = acc.String()
		_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(parts))
		return emitDeltaIdx(textIdx, delta)
	})
	if streamErr != nil {
		_ = onEvent(domain.StreamEvent{Type: domain.EventError, MessageID: msg.ID, Error: streamErr.Error()})
		if acc.Len() == 0 {
			parts[textIdx].Text = composeFailureReply(streamErr)
		}
	}
	if err := emitEndIdx(textIdx); err != nil {
		return err
	}
	_ = st.UpdateMessageParts(ctx, msg.ID, cloneParts(parts))
	_ = onEvent(domain.StreamEvent{Type: domain.EventDone, MessageID: msg.ID})
	_ = st.TouchSession(ctx, msg.SessionID)

	if mem != nil && acc.Len() > 0 {
		appendMemoryAfterTurn(mem, ctx, msg.SessionID, pid, lastUser, acc.String())
	}
	return nil
}

func appendMemoryAfterTurn(mem memory.Store, ctx context.Context, sessionID, projectID, lastUser, fullText string) {
	if mem == nil {
		return
	}
	summary := strings.TrimSpace(fullText)
	if len(summary) > 400 {
		summary = summary[:400] + "…"
	}
	if summary != "" {
		_ = mem.AppendLevel(ctx, sessionID, projectID, 1, "turn", summary)
		if projectID != "" {
			_ = mem.AppendLevel(ctx, sessionID, projectID, 2, "project_digest", summary)
		}
	}
	gline := strings.TrimSpace(lastUser)
	if len(gline) > 160 {
		gline = gline[:160] + "…"
	}
	if gline != "" {
		_ = mem.AppendLevel(ctx, sessionID, "", 3, "topic", gline)
	}
}

func cloneParts(p []domain.Part) []domain.Part {
	out := make([]domain.Part, len(p))
	copy(out, p)
	return out
}

func lastUserText(msgs []domain.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != domain.RoleUser {
			continue
		}
		var b strings.Builder
		for _, p := range msgs[i].Parts {
			if p.Type == domain.PartText || p.Type == domain.PartCode {
				b.WriteString(p.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return ""
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

type emitIdxFn func(domain.PartType, int) error
type emitDeltaIdxFn func(int, string) error
type emitEndIdxFn func(int) error

// streamPhaseMarkdown appends one part and streams Markdown text into it (task / thinking 等).
func streamPhaseMarkdown(
	ctx context.Context,
	client *llm.Client,
	cfg RunConfig,
	system string,
	user string,
	pt domain.PartType,
	parts *[]domain.Part,
	msgID string,
	st MessageUpdater,
	emitStart emitIdxFn,
	emitDelta emitDeltaIdxFn,
	emitEnd emitEndIdxFn,
) error {
	*parts = append(*parts, domain.Part{Type: pt, Text: ""})
	idx := len(*parts) - 1
	if err := emitStart(pt, idx); err != nil {
		return err
	}
	msgs := []llm.ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
	var acc strings.Builder
	temp := cfg.Temperature * 0.75
	if temp < 0.1 {
		temp = 0.3
	}
	streamErr := client.StreamChat(ctx, llm.StreamParams{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		Messages:    msgs,
		Temperature: temp,
		TopP:        cfg.TopP,
		TopK:        cfg.TopK,
	}, func(delta string) error {
		acc.WriteString(delta)
		(*parts)[idx].Text = acc.String()
		_ = st.UpdateMessageParts(ctx, msgID, cloneParts(*parts))
		return emitDelta(idx, delta)
	})
	if streamErr != nil {
		if acc.Len() == 0 {
			(*parts)[idx].Text = composeFailureReply(streamErr)
			_ = st.UpdateMessageParts(ctx, msgID, cloneParts(*parts))
		}
	}
	return emitEnd(idx)
}

// RunConfig controls one generation pass.
type RunConfig struct {
	AssistantMessageID string
	SessionID          string
	BaseURL            string
	APIKey             string
	Model              string
	SystemPrompt       string
	// PromptsDir is the directory containing system_suffix.md, task_flow_user.md, thinking_user.md.
	// Empty: auto-resolve backend/data/prompts from cwd; missing files use built-in defaults.
	PromptsDir     string
	Temperature    float64
	TopP           float64
	TopK           *int
	MaxAgentRounds int
	MaxContextTokens int // <=0: skip token-based history trim
	TokenBudget      int // <=0: no cap on accumulated completion usage per RunStream
	Workspace        string
	ToolsJSON        json.RawMessage // JSON array of OpenAI tool defs
	WaitShellApproval func(context.Context, ShellApprovalRequest) (bool, error)
	MCPCall           func(context.Context, string, string) (string, error)
}

func reflexionAppend(
	ctx context.Context,
	client *llm.Client,
	cfg RunConfig,
	work *[]llm.ChatMessage,
	mem memory.Store,
	sessionID, projectID string,
	tc llm.ToolCallResult,
	toolOut string,
	recordUsage func(*llm.CompleteResult),
) {
	snippet := toolOut
	if len(snippet) > 800 {
		snippet = snippet[:800] + "…"
	}
	ref, err := client.Complete(ctx, llm.CompleteParams{
		BaseURL:     cfg.BaseURL,
		APIKey:      cfg.APIKey,
		Model:       cfg.Model,
		Messages: []llm.ChatMessage{
			{Role: "system", Content: "用 2～4 句中文简要说明上一轮工具失败或异常的可能原因与下一步建议。不要调用工具。"},
			{Role: "user", Content: "工具名: " + tc.Name + "\n输出:\n" + snippet},
		},
		Temperature: minFloat(cfg.Temperature, 0.35),
		TopP:        cfg.TopP,
		TopK:        cfg.TopK,
	})
	if err != nil || ref == nil {
		return
	}
	recordUsage(ref)
	txt := strings.TrimSpace(ref.Content)
	if txt == "" {
		return
	}
	*work = append(*work, llm.ChatMessage{
		Role:    "user",
		Content: "（内部反思，仅供参考）\n" + txt,
	})
	if mem != nil {
		_ = mem.AppendLevel(ctx, sessionID, projectID, 1, "reflection", txt)
	}
}

// NewAssistantPlaceholder creates an empty assistant message after the user message.
func NewAssistantPlaceholder(sessionID, parentUserID string) *domain.Message {
	m := &domain.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      domain.RoleAssistant,
		CreatedAt: time.Now().UTC(),
		Parts:     []domain.Part{{Type: domain.PartText, Text: ""}},
	}
	if parentUserID != "" {
		m.ParentMessageID = &parentUserID
	}
	return m
}

var _ MessageUpdater = (*store.Store)(nil)
