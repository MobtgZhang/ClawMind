package agent

import (
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
)

// roughTokens estimates token count (~4 chars per token) for budgeting.
func roughTokens(s string) int {
	if s == "" {
		return 0
	}
	n := len([]rune(s))
	return (n + 3) / 4
}

// TrimHistoryByEstimatedTokens keeps the tail of history so that concatenated
// user/assistant text (excluding the last user message) stays under maxTokens.
// maxTokens <= 0 means no trimming beyond prior round-based trim.
func TrimHistoryByEstimatedTokens(msgs []domain.Message, maxTokens int) []domain.Message {
	if maxTokens <= 0 || len(msgs) == 0 {
		return msgs
	}
	var total int
	for _, m := range msgs {
		total += messageRoughTokens(m)
	}
	if total <= maxTokens {
		return msgs
	}
	// Drop oldest messages until under budget (never drop below 1 message).
	for len(msgs) > 1 {
		total -= messageRoughTokens(msgs[0])
		msgs = msgs[1:]
		if total <= maxTokens {
			break
		}
	}
	return msgs
}

func messageRoughTokens(m domain.Message) int {
	var b int
	for _, p := range m.Parts {
		switch p.Type {
		case domain.PartText, domain.PartCode, domain.PartReasoning, domain.PartTaskFlow, domain.PartThinking:
			b += roughTokens(p.Text)
		case domain.PartToolResult:
			b += roughTokens(p.Result)
		case domain.PartToolCall:
			b += roughTokens(p.Arguments)
		}
	}
	return b
}
