package agent

import "github.com/mobtgzhang/clawmind/backend/internal/domain"

// TrimHistoryByAssistantRounds keeps the last maxRounds assistant turns (each turn may be preceded by user).
// maxRounds <= 0 means no trimming.
func TrimHistoryByAssistantRounds(msgs []domain.Message, maxRounds int) []domain.Message {
	if maxRounds <= 0 || len(msgs) == 0 {
		return msgs
	}
	asstSeen := 0
	cut := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == domain.RoleAssistant {
			asstSeen++
			if asstSeen == maxRounds {
				cut = i
				break
			}
		}
	}
	if asstSeen < maxRounds {
		return msgs
	}
	for cut > 0 && msgs[cut-1].Role == domain.RoleUser {
		cut--
	}
	return msgs[cut:]
}
