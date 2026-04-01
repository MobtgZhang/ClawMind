package domain

import "time"

// Session is a chat thread.
type Session struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Model     string            `json:"model"`
	ProjectID *string           `json:"projectId,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// MessageRole aligns with OpenAI-style roles.
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
	RoleTool      MessageRole = "tool"
)

// PartType mirrors Cursor-style segmented assistant content.
type PartType string

const (
	PartText       PartType = "text"
	PartCode       PartType = "code"
	PartToolCall   PartType = "tool_call"
	PartToolResult PartType = "tool_result"
	PartReasoning  PartType = "reasoning"
	// Cursor 风格分段：任务流程 → 思考 → 最终输出（依次展示）
	PartTaskFlow PartType = "task_flow"
	PartThinking PartType = "thinking"
)

// Part is one segment inside a message.
type Part struct {
	Type       PartType `json:"type"`
	Text       string   `json:"text,omitempty"`
	Language   string   `json:"language,omitempty"`
	ToolCallID string   `json:"tool_call_id,omitempty"`
	Name       string   `json:"name,omitempty"`
	Arguments  string   `json:"arguments,omitempty"`
	Result     string   `json:"result,omitempty"`
}

// Message is a persisted turn.
type Message struct {
	ID              string      `json:"id"`
	SessionID       string      `json:"sessionId"`
	Role            MessageRole `json:"role"`
	CreatedAt       time.Time   `json:"createdAt"`
	Parts           []Part      `json:"parts"`
	ParentMessageID *string     `json:"parentMessageId,omitempty"`
	BranchID        *string     `json:"branchId,omitempty"`
}

// StreamEventType is the SSE payload discriminator.
type StreamEventType string

const (
	EventDelta             StreamEventType = "delta"
	EventPartStart         StreamEventType = "part_start"
	EventPartEnd           StreamEventType = "part_end"
	EventToolCall          StreamEventType = "tool_call"
	EventToolRes           StreamEventType = "tool_result"
	EventToolApprovalReq   StreamEventType = "tool_approval_request"
	EventToolApprovalRes   StreamEventType = "tool_approval_result"
	EventDone              StreamEventType = "done"
	EventError             StreamEventType = "error"
)

// StreamEvent is JSON sent as each SSE data line.
type StreamEvent struct {
	Type       StreamEventType `json:"type"`
	MessageID  string          `json:"messageId,omitempty"`
	PartIndex  int             `json:"partIndex,omitempty"`
	PartType   PartType        `json:"partType,omitempty"`
	Text       string          `json:"text,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Arguments  string          `json:"arguments,omitempty"`
	Result     string          `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	ApprovalID string          `json:"approvalId,omitempty"`
	SessionID  string          `json:"sessionId,omitempty"`
	Approved   *bool           `json:"approved,omitempty"`
}
