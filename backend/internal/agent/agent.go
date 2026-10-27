package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mobtgzhang/clawmind/backend/internal/codectx"
	"github.com/mobtgzhang/clawmind/backend/internal/codegraph"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
	"github.com/mobtgzhang/clawmind/backend/internal/memory"
	"github.com/mobtgzhang/clawmind/backend/internal/security"
	"github.com/mobtgzhang/clawmind/backend/internal/store"
)

// MessageUpdater is the store subset the agent needs.
type MessageUpdater interface {
	GetMessage(ctx context.Context, id string) (*domain.Message, error)
	ListMessages(ctx context.Context, sessionID string) ([]domain.Message, error)
	UpdateMessageParts(ctx context.Context, id string, parts []domain.Part) error
	TouchSession(ctx context.Context, id string) error
	GetSession(ctx context.Context, id string) (*domain.Session, error)
}

// Observer is the memory distiller seam. The raw turn is stored as L0 and the higher layers are
// built off the turn; without it the agent writes the layers itself, which is the fallback path.
type Observer interface {
	Observe(ctx context.Context, sessionID, projectID, text string) error
}

// appendMemoryAfterTurn records one turn. With a distiller the raw text goes to L0 and the higher
// layers are built off the turn; without one the agent writes the layers itself, which is the
// fallback path that keeps memory usable when no model is configured.
func appendMemoryAfterTurn(mem memory.Store, ctx context.Context, sessionID, projectID, lastUser, fullText string, obs Observer) {
	if mem == nil {
		return
	}
	summary := strings.TrimSpace(fullText)
	if len(summary) > 400 {
		summary = summary[:400] + "…"
	}
	if obs != nil {
		if err := obs.Observe(ctx, sessionID, projectID, fullText); err == nil {
			return
		}
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

// harnessLessonsBlock renders transferable lessons from the experience library. The library is
// data about past runs, so it is presented as prior experience rather than rules to obey.
func harnessLessonsBlock(lessons []domain.HarnessLesson) string {
	if len(lessons) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 经验库（本项目此前被采纳的教训）\n\n")
	for _, l := range lessons {
		if b.Len() > 2400 {
			break
		}
		confidence := strings.TrimSpace(l.Confidence)
		if confidence == "" {
			confidence = "low"
		}
		b.WriteString("- " + l.Cue + " → " + l.Lesson + "（" + confidence + "）\n")
	}
	return strings.TrimSpace(b.String())
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
	client llm.Provider,
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
	// ProjectID is the scope the harness, memory and hooks are resolved against.
	ProjectID    string
	BaseURL      string
	APIKey       string
	Model        string
	SystemPrompt string
	// PromptsDir is the directory containing system_suffix.md, task_flow_user.md, thinking_user.md.
	// Empty: auto-resolve backend/data/prompts from cwd; missing files use built-in defaults.
	PromptsDir       string
	Temperature      float64
	TopP             float64
	TopK             *int
	MaxAgentRounds   int
	MaxContextTokens int // <=0: skip token-based history trim
	TokenBudget      int // <=0: no cap on accumulated completion usage per RunStream
	Workspace        string
	ToolsJSON        json.RawMessage // JSON array of OpenAI tool defs
	// Repo is the code workspace profile: languages, manifests and inferred commands.
	Repo *codectx.Profile
	// Budget is the loop shape the project class produced: node count, re-entry, fan-out and search
	// caps. It is reported to the transcript because the shape is a decision, not a default.
	Budget codectx.Budget
	// Graph is the workspace symbol index. nil means the graph shape cannot answer dependency
	// questions and the turn runs as a single pass.
	Graph *codegraph.Index
	// GraphLoop enables the graph shape for complex projects.
	GraphLoop bool
	// ClassReasons is the classifier's justification, surfaced so a reader can see why the shape
	// changed for this turn.
	ClassifierEnabled bool
	// ProjectClass forces a class ("simple" or "complex") and skips the score. It is the operator's
	// override for a repository the score cannot see, such as one whose complexity lives in runtime
	// behaviour rather than in file count.
	ProjectClass string
	// Harness is the editable component surface injected into the system prompt.
	Harness harness.Tree
	// HarnessLessons are transferable lessons read from the experience library.
	HarnessLessons []domain.HarnessLesson
	// Loadout is the asset set bound to this agent by the memory asset registry.
	Loadout []memory.Asset
	// AutoVerify runs the inferred verification command after edits and feeds the result back.
	AutoVerify        bool
	WaitShellApproval func(context.Context, ShellApprovalRequest) (bool, error)
	MCPCall           func(context.Context, string, string) (string, error)
	// Hooks is the plugin dispatcher; nil means no mounted plugin registered a hook.
	Hooks HookSink
	// ShellPolicy is the sandbox layer resolved from the plugin registry; nil means the built-in floor.
	ShellPolicy *security.Policy
	// Observer distils memory layers from the turn. nil falls back to direct layer writes.
	Observer Observer
}

func reflexionAppend(
	ctx context.Context,
	client llm.Provider,
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
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
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
