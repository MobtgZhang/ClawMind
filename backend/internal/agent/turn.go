package agent

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mobtgzhang/clawmind/backend/internal/codectx"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
	"github.com/mobtgzhang/clawmind/backend/internal/memory"
	"github.com/mobtgzhang/clawmind/backend/internal/thread"
)

// turn is one generation pass. Both loop shapes need the same message, event stream, tool layer,
// hook surface and token accounting; only the control graph differs. Building the shared state once
// is what lets the graph loop reuse the sandbox, approval and hook policy instead of
// reimplementing them.
type turn struct {
	ctx     context.Context
	st      MessageUpdater
	mem     memory.Store
	client  llm.Provider
	cfg     RunConfig
	onEvent func(domain.StreamEvent) error

	msg      *domain.Message
	parts    []domain.Part
	history  []llm.ChatMessage
	system   string
	lastUser string
	pid      string
	prompts  promptBundle
	runner   *ToolRunner

	tokenAccum int
}

// buildTurn resolves the message, the history prefix, the system prompt and the tool layer for one
// pass. It is the only place the two loops agree on what a turn contains.
func buildTurn(
	ctx context.Context,
	st MessageUpdater,
	mem memory.Store,
	client llm.Provider,
	cfg RunConfig,
	onEvent func(domain.StreamEvent) error,
) (*turn, error) {
	msg, err := st.GetMessage(ctx, cfg.AssistantMessageID)
	if err != nil || msg == nil {
		return nil, err
	}
	history, err := st.ListMessages(ctx, msg.SessionID)
	if err != nil {
		return nil, err
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

	t := &turn{
		ctx:     ctx,
		st:      st,
		mem:     mem,
		client:  client,
		cfg:     cfg,
		onEvent: onEvent,
		msg:     msg,
		runner: &ToolRunner{
			Workspace: cfg.Workspace,
			Client:    client,
			Harness:   cfg.Harness,
			MCPCall:   cfg.MCPCall,
			Policy:    cfg.ShellPolicy,
			Graph:     cfg.Graph,
		},
	}
	t.runner.Hooks = cfg.Hooks

	// The request is read before the shape is chosen. The workspace alone cannot decide the loop: a
	// typo fix in a three-thousand-file repository is still a single-span change, and a boundary
	// change in a forty-file repository is not. Scoring after trimming would size the window from a
	// shape the turn is not actually running.
	t.lastUser = lastUserText(upto)
	if cfg.ClassifierEnabled {
		t.cfg.Budget = classifyTurn(cfg, t.lastUser)
	}

	// The graph loop can be given a larger window than the single-pass loop, but never a smaller
	// one: trimming to the shape the turn will actually run is what makes the budget meaningful.
	rounds := cfg.MaxAgentRounds
	if t.cfg.Budget.MaxRounds > rounds {
		rounds = t.cfg.Budget.MaxRounds
	}
	maxTokens := cfg.MaxContextTokens
	if t.cfg.Budget.ContextTokens > maxTokens {
		maxTokens = t.cfg.Budget.ContextTokens
	}

	kept := TrimHistoryByAssistantRounds(upto, rounds)
	kept = TrimHistoryByEstimatedTokens(kept, maxTokens)
	// PostCompact only fires when context was actually dropped. A hook that watches compaction has
	// to be told when it happened; otherwise the event is a promise the runtime never keeps.
	if cfg.Hooks != nil && len(kept) < len(upto) {
		if _, err := t.runner.fireHooks(ctx, "PostCompact", cfg, "", map[string]any{"droppedMessages": len(upto) - len(kept)}, ""); err != nil {
			return t, t.stopTurn("PostCompact", err)
		}
		upto = kept
	}

	t.history = llm.ToChatMessages(cfg.SystemPrompt, upto)
	if sess != nil && sess.ProjectID != nil {
		t.pid = *sess.ProjectID
	}

	system := cfg.SystemPrompt
	system += "\n\n" + systemRuntimeContextBlock(time.Now())
	if mem != nil {
		memQuery := truncateRunes(strings.TrimSpace(t.lastUser), 512)
		lines, _ := mem.RetrieveLevels(ctx, msg.SessionID, t.pid, memQuery)
		if len(lines) > 0 {
			system += "\n\n多级记忆（L3 全局 → L2 项目 → L1 会话）：\n- " + strings.Join(lines, "\n- ")
		}
	}
	t.prompts = loadPromptBundle(cfg.PromptsDir)
	system += "\n\n" + t.prompts.SystemSuffix

	if cfg.Repo != nil {
		if block := cfg.Repo.PromptBlock(4000); block != "" {
			system += "\n\n" + block
		}
	}
	if len(cfg.Harness) > 0 {
		if block := cfg.Harness.PromptBlock(6000); block != "" {
			system += "\n\n" + block
		}
	}
	if block := harnessLessonsBlock(cfg.HarnessLessons); block != "" {
		system += "\n\n" + block
	}
	if block := loadoutBlock(cfg.Loadout); block != "" {
		system += "\n\n" + block
	}
	t.system = system
	return t, nil
}

// classifyTurn resolves the shape for one turn. An operator override wins over the score, because
// the override is a decision the operator made on purpose; without it the classifier would silently
// re-decide a project the operator already classified.
func classifyTurn(cfg RunConfig, task string) codectx.Budget {
	switch strings.ToLower(strings.TrimSpace(cfg.ProjectClass)) {
	case string(codectx.ClassSimple):
		return codectx.FixedBudget(codectx.ClassSimple, cfg.MaxAgentRounds, cfg.MaxContextTokens)
	case string(codectx.ClassComplex):
		return codectx.FixedBudget(codectx.ClassComplex, cfg.MaxAgentRounds, cfg.MaxContextTokens)
	}
	var shape codectx.GraphShape
	if cfg.Graph != nil {
		shape = cfg.Graph.Shape()
	}
	return codectx.Classify(cfg.Repo, shape, task, cfg.MaxAgentRounds, cfg.MaxContextTokens)
}

// RunStream is the dispatcher: the project class decides which shape executes, and the two shapes
// share everything else. Keeping the dispatch narrow is what stops the graph loop from becoming a
// second runtime with its own approval and hook policy.
func RunStream(
	ctx context.Context,
	st MessageUpdater,
	mem memory.Store,
	client llm.Provider,
	cfg RunConfig,
	onEvent func(domain.StreamEvent) error,
) error {
	t, err := buildTurn(ctx, st, mem, client, cfg, onEvent)
	if err != nil || t == nil {
		return err
	}

	// Stop fires on every exit path, including a cancelled or failed turn: a hook that watches the
	// end of a run has to see the run end, not only the successful branch.
	defer func() {
		if cfg.Hooks == nil {
			return
		}
		_, _ = t.runner.fireHooks(ctx, "Stop", cfg, "", map[string]any{"answer": lastAnswerText(t.parts)}, "")
	}()

	// Context-level hooks inject guidance into the system prompt. A veto here stops the turn before
	// any model call is paid for, which is the point of a pre-turn hook.
	if cfg.Hooks != nil {
		guidance, herr := t.runner.fireHooks(ctx, "SessionStart", cfg, "", nil, "")
		if herr != nil {
			return t.stopTurn("SessionStart", herr)
		}
		if guidance != "" {
			t.system += "\n\nhook 注入（SessionStart）：\n" + guidance
		}
		guidance, herr = t.runner.fireHooks(ctx, "UserPromptSubmit", cfg, "", map[string]any{"prompt": t.lastUser}, t.lastUser)
		if herr != nil {
			return t.stopTurn("UserPromptSubmit", herr)
		}
		if guidance != "" {
			t.system += "\n\nhook 注入（UserPromptSubmit）：\n" + guidance
		}
	}

	if useGraphLoop(t.cfg) {
		return runGraphTurn(t)
	}
	return runLinearTurn(t)
}

// useGraphLoop is the gate the operator controls: the graph shape runs only when it is enabled, an
// index exists, and the classifier actually scored the project as complex. A large repository with a
// one-line request still runs the single pass, because the graph would be pure cost there.
func useGraphLoop(cfg RunConfig) bool {
	return cfg.GraphLoop && cfg.Graph != nil && cfg.Budget.Class == codectx.ClassComplex && cfg.Budget.Loop == "graph"
}

var errNoModelClient = errors.New("graph: no model client")

// Run implements graphloop.ToolExecutor on the same tool layer the single-pass loop uses, so the
// sandbox policy, hook firing and approval stay in one place.
func (t *turn) Run(ctx context.Context, name, argsJSON string) (string, error) {
	return t.runner.Run(ctx, name, argsJSON, t.cfg)
}

func (t *turn) recordUsage(res *llm.CompleteResult) {
	if res == nil {
		return
	}
	tokens := res.TotalTokens
	if tokens <= 0 {
		tokens = res.PromptTokens + res.CompletionTokens
	}
	if tokens > 0 {
		t.tokenAccum += tokens
		slog.Info("llm.completion_usage",
			"prompt_tokens", res.PromptTokens,
			"completion_tokens", res.CompletionTokens,
			"total_tokens", tokens,
			"accum", t.tokenAccum,
			"assistant_message_id", t.cfg.AssistantMessageID,
		)
	}
}

func (t *turn) appendPart(p domain.Part) int {
	t.parts = append(t.parts, p)
	return len(t.parts) - 1
}

func (t *turn) emitStart(pt domain.PartType, idx int) error {
	return t.onEvent(domain.StreamEvent{Type: domain.EventPartStart, MessageID: t.msg.ID, PartIndex: idx, PartType: pt})
}

func (t *turn) emitDelta(idx int, text string) error {
	if text == "" {
		return nil
	}
	return t.onEvent(domain.StreamEvent{Type: domain.EventDelta, MessageID: t.msg.ID, PartIndex: idx, Text: text})
}

func (t *turn) emitEnd(idx int) error {
	return t.onEvent(domain.StreamEvent{Type: domain.EventPartEnd, MessageID: t.msg.ID, PartIndex: idx})
}

func (t *turn) flush() {
	_ = t.st.UpdateMessageParts(t.ctx, t.msg.ID, cloneParts(t.parts))
}

func (t *turn) finish() {
	_ = t.onEvent(domain.StreamEvent{Type: domain.EventDone, MessageID: t.msg.ID})
	_ = t.st.TouchSession(t.ctx, t.msg.SessionID)
}

// stopTurn records a hook veto as the turn's answer. A veto is a decision made by a plugin, so it
// belongs in the transcript rather than only in the error stream.
func (t *turn) stopTurn(event string, veto error) error {
	warn := event + " hook 否决：" + veto.Error()
	_ = t.onEvent(domain.StreamEvent{Type: domain.EventError, MessageID: t.msg.ID, Error: warn})
	t.appendPart(domain.Part{Type: domain.PartText, Text: warn})
	t.flush()
	t.finish()
	return nil
}

// reportWarning is the shared exit for a turn that stopped for a runtime reason rather than a model
// answer: the reason has to reach the transcript, not only the event stream.
func (t *turn) reportWarning(warn string) {
	_ = t.onEvent(domain.StreamEvent{Type: domain.EventError, MessageID: t.msg.ID, Error: warn})
	t.appendPart(domain.Part{Type: domain.PartText, Text: warn})
	t.flush()
	t.finish()
}

// streamPhase streams one node's prose into one part, so the transcript keeps the same shape as the
// single-pass loop: the work is visible while it is produced.
func (t *turn) streamPhase(pt domain.PartType, user string) (string, error) {
	idx := t.appendPart(domain.Part{Type: pt, Text: ""})
	if err := t.emitStart(pt, idx); err != nil {
		return "", err
	}
	if t.client == nil {
		return "", errNoModelClient
	}
	var acc strings.Builder
	temp := t.cfg.Temperature * 0.75
	if temp < 0.1 {
		temp = 0.3
	}
	err := t.client.StreamChat(t.ctx, llm.StreamParams{
		BaseURL:     t.cfg.BaseURL,
		APIKey:      t.cfg.APIKey,
		Model:       t.cfg.Model,
		Messages:    []llm.ChatMessage{{Role: "system", Content: truncateRunes(t.system, 6000)}, {Role: "user", Content: user}},
		Temperature: temp,
		TopP:        t.cfg.TopP,
		TopK:        t.cfg.TopK,
	}, func(delta string) error {
		acc.WriteString(delta)
		t.parts[idx].Text = acc.String()
		t.flush()
		return t.emitDelta(idx, delta)
	})
	if err != nil && acc.Len() == 0 {
		t.parts[idx].Text = composeFailureReply(err)
		t.flush()
	}
	if err := t.emitEnd(idx); err != nil {
		return "", err
	}
	return strings.TrimSpace(acc.String()), nil
}

// toolEvent records one tool call and its result as the transcript already does, so a graph node and
// the single-pass loop produce the same parts.
func (t *turn) toolEvent(name, args, result string) error {
	callID := uuid.NewString()
	_ = t.onEvent(domain.StreamEvent{
		Type:       domain.EventToolCall,
		MessageID:  t.msg.ID,
		ToolCallID: callID,
		ToolName:   name,
		Arguments:  args,
	})
	_ = t.onEvent(domain.StreamEvent{
		Type:       domain.EventToolRes,
		MessageID:  t.msg.ID,
		ToolCallID: callID,
		ToolName:   name,
		Result:     result,
	})
	t.appendPart(domain.Part{Type: domain.PartToolCall, ToolCallID: callID, Name: name, Arguments: args})
	t.appendPart(domain.Part{Type: domain.PartToolResult, ToolCallID: callID, Result: result})
	t.flush()
	return nil
}
