package evolution

import (
	"fmt"
	"sort"
	"strings"
)

// ExpectedRollouts is the budget identity for rollouts: R = 4k, made of k shared base runs,
// 2k candidate probes and k finals.
func ExpectedRollouts(k int) int { return 4 * k }

// ExpectedSessions is the budget identity for stages: P = 3k, made of k INV proposals, k DOM
// proposals and k compositions. Selection and reflection are recorded outside this ledger.
func ExpectedSessions(k int) int { return 3 * k }

// LedgerEntry is one recorded execution or stage.
type LedgerEntry struct {
	Kind     string `json:"kind"` // rollout | session
	Label    string `json:"label"`
	Measured bool   `json:"measured"` // a rollout that really ran, not one that was only predicted
	Cached   bool   `json:"cached"`   // reused from a finished run with the same identity
	Attempts int    `json:"attempts"`
	Tokens   int    `json:"tokens"`
}

// Ledger is everything one mode spent on one task, recorded as it happens.
type Ledger struct {
	Mode    string        `json:"mode"`
	TaskID  string        `json:"taskId"`
	K       int           `json:"k"`
	Entries []LedgerEntry `json:"entries"`
}

func NewLedger(mode, taskID string, k int) *Ledger {
	return &Ledger{Mode: mode, TaskID: taskID, K: k}
}

func (l *Ledger) Rollout(label string, measured, cached bool) {
	l.Entries = append(l.Entries, LedgerEntry{Kind: "rollout", Label: label, Measured: measured, Cached: cached, Attempts: 1})
}

func (l *Ledger) Session(label string, cached bool, attempts int) {
	if attempts < 1 {
		attempts = 1
	}
	l.Entries = append(l.Entries, LedgerEntry{Kind: "session", Label: label, Cached: cached, Attempts: attempts})
}

func (l *Ledger) R() int {
	n := 0
	for _, e := range l.Entries {
		if e.Kind == "rollout" {
			n++
		}
	}
	return n
}

func (l *Ledger) P() int {
	n := 0
	for _, e := range l.Entries {
		if e.Kind == "session" {
			n++
		}
	}
	return n
}

// MeasuredR counts the rollouts that were actually executed. A ledger can satisfy R=4k while
// every entry is a prediction, which is exactly the failure this number exposes.
func (l *Ledger) MeasuredR() int {
	n := 0
	for _, e := range l.Entries {
		if e.Kind == "rollout" && e.Measured {
			n++
		}
	}
	return n
}

func (l *Ledger) Labels(kind string) []string {
	var out []string
	for _, e := range l.Entries {
		if e.Kind == kind {
			out = append(out, e.Label)
		}
	}
	return out
}

// Duplicates lists labels written twice. A repeated label means two stages wrote the same run id
// and one silently overwrote the other, so a candidate the ledger counts was never produced.
func (l *Ledger) Duplicates() []string {
	counts := map[string]int{}
	for _, e := range l.Entries {
		counts[e.Kind+"|"+e.Label]++
	}
	var dupes []string
	for key, n := range counts {
		if n > 1 {
			dupes = append(dupes, key)
		}
	}
	sort.Strings(dupes)
	return dupes
}

// Check verifies the budget identity. Duplicates are checked first because a duplicate can
// satisfy the counts while a stage never actually ran.
func (l *Ledger) Check() error {
	wantR, wantP := ExpectedRollouts(l.K), ExpectedSessions(l.K)
	dupes := l.Duplicates()
	if len(dupes) == 0 && l.R() == wantR && l.P() == wantP {
		return nil
	}
	detail := []string{fmt.Sprintf("%s-%d %s: R=%d (want %d), P=%d (want %d)",
		l.Mode, l.K, l.TaskID, l.R(), wantR, l.P(), wantP)}
	if len(dupes) > 0 {
		detail = append(detail, "duplicate labels: "+strings.Join(dupes, ", "))
	}
	return fmt.Errorf("%s", strings.Join(detail, "; "))
}

// Summary is the one-line form shown in the round record and the API.
func (l *Ledger) Summary() string {
	return fmt.Sprintf("R=%d/%d (measured %d), P=%d/%d", l.R(), ExpectedRollouts(l.K), l.MeasuredR(), l.P(), ExpectedSessions(l.K))
}
