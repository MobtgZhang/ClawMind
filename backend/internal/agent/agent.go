package agent

import (
	"context"
	"encoding/json"
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

	lastUser := lastUserText(upto)
	pid := ""
	if sess != nil && sess.ProjectID != nil {
		pid = *sess.ProjectID
	}

	system := cfg.SystemPrompt
	if mem != nil {
		lines, _ := mem.RetrieveLevels(ctx, msg.SessionID, pid, "")
		if len(lines) > 0 {
			system += "\n\n多级记忆（L3 全局 → L2 项目 → L1 会话）：\n- " + strings.Join(lines, "\n- ")
		}
	}
	system += "\n\n你是带工具的自进化助手。需要时调用工具；最终用清晰中文回答用户。"

	runner := &ToolRunner{Workspace: cfg.Workspace, Client: client}

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
	planUser := "用 3 条以内的 Markdown 要点（以 - 开头）概括：用户要什么、你打算怎么做。不要调用工具，不要写最终答案。\n\n用户最新输入：\n" + lastUser
	if err := streamPhaseMarkdown(ctx, client, cfg, phaseSystem, planUser, domain.PartTaskFlow, &parts, msg.ID, st, emitStartIdx, emitDeltaIdx, emitEndIdx); err != nil {
		return err
	}

	// --- 思考流程（流式） ---
	thinkUser := "用 2～4 句中文说明你的推理思路（Markdown，不写最终答案、不列工具结果）。\n\n用户最新输入：\n" + lastUser
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

	for round := 0; round < maxTools; round++ {
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
			out, rerr := runner.Run(ctx, tc.Name, tc.Arguments, cfg)
			if rerr != nil {
				out = "错误: " + rerr.Error()
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
	BaseURL            string
	APIKey             string
	Model              string
	SystemPrompt       string
	Temperature        float64
	TopP               float64
	TopK               *int
	MaxAgentRounds     int
	Workspace          string
	ToolsJSON          json.RawMessage // JSON array of OpenAI tool defs
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
