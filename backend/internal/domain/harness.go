package domain

import "time"

// Verdict is the outcome recorded for one harness edit.
type Verdict string

const (
	VerdictHelped     Verdict = "helped"
	VerdictHurt       Verdict = "hurt"
	VerdictNoEffect   Verdict = "no observed effect"
	VerdictConfounded Verdict = "confounded"
)

// HarnessEdit is one component change attributed to a round, with its observed effect.
type HarnessEdit struct {
	ID         string    `json:"id"`
	RoundID    string    `json:"roundId"`
	Component  string    `json:"component"`
	Candidates string    `json:"candidates,omitempty"`
	Change     string    `json:"change"`
	Observed   string    `json:"observed,omitempty"`
	Verdict    Verdict   `json:"verdict"`
	CreatedAt  time.Time `json:"createdAt"`
}

// HarnessLesson is one transferable experience entry (cue -> lesson) kept in the library.
type HarnessLesson struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"projectId"`
	RoundID    string    `json:"roundId,omitempty"`
	Cue        string    `json:"cue"`
	Lesson     string    `json:"lesson"`
	Evidence   string    `json:"evidence,omitempty"`
	Confidence string    `json:"confidence"`
	CreatedAt  time.Time `json:"createdAt"`
}

// HarnessRound is one Propose-Probe-Compose-Reflect step.
type HarnessRound struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	SessionID string    `json:"sessionId,omitempty"`
	Mode      string    `json:"mode"`
	K         int       `json:"k"`
	Adopted   string    `json:"adopted,omitempty"`
	KeptBase  bool      `json:"keptBase"`
	Notes     string    `json:"notes,omitempty"`
	Inventory string    `json:"inventory,omitempty"`
	AlwaysOn  int       `json:"alwaysOnChars"`
	Errors    []string  `json:"errors,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}
