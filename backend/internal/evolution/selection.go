package evolution

import (
	"crypto/sha1"
	"fmt"
	"math/rand"
	"sort"
	"strings"
)

// OrderGradedFirst returns measured candidates before predicted ones, keeping the original
// relative order inside each group. Nothing is discarded: an ungraded candidate is still a
// candidate, it is simply not allowed to outrank a measurement.
func OrderGradedFirst(cands []Candidate) []Candidate {
	out := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		if c.Probe.Graded {
			out = append(out, c)
		}
	}
	for _, c := range cands {
		if !c.Probe.Graded {
			out = append(out, c)
		}
	}
	return out
}

// AssignIDs maps an anonymous id to a label. Ids are assigned by canonical sort then a seeded
// shuffle, so the same task, k and mode always produce the same mapping and an id carries no
// information about which candidate produced what.
func AssignIDs(labels []string, taskID string, k int, mode string) map[string]string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%s", taskID, k, mode)))
	var seed int64
	for _, b := range sum[:6] {
		seed = seed*256 + int64(b)
	}
	ordered := append([]string(nil), labels...)
	sort.Strings(ordered)
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(ordered), func(i, j int) { ordered[i], ordered[j] = ordered[j], ordered[i] })
	out := map[string]string{}
	for i, label := range ordered {
		out[AnonID(i)] = label
	}
	return out
}

// AnonID is A..Z, AA..AZ, BA... — always accepted by the choice parser.
func AnonID(index int) string {
	var out string
	n := index + 1
	for n > 0 {
		rem := (n - 1) % 26
		out = string(rune('A'+rem)) + out
		n = (n - 1) / 26
	}
	if out == "" {
		return "A"
	}
	return out
}

// Selection is one blind choice over the finals, with every drop recorded.
type Selection struct {
	TaskID      string            `json:"taskId"`
	Mode        string            `json:"mode"`
	K           int               `json:"k"`
	ChosenID    string            `json:"chosenId,omitempty"`
	ChosenLabel string            `json:"chosenLabel,omitempty"`
	Why         string            `json:"why,omitempty"`
	IDMap       map[string]string `json:"idMap,omitempty"`
	Dropped     []string          `json:"dropped,omitempty"`
	NCandidates int               `json:"nCandidates"`
	Ok          bool              `json:"ok"`
	Fallback    bool              `json:"fallback"`
	Error       string            `json:"error,omitempty"`
	Experience  map[string]string `json:"experience,omitempty"`

	pool []Candidate
}

func NewSelection(taskID, mode string, k int) *Selection {
	return &Selection{TaskID: taskID, Mode: mode, K: k}
}

// SetPool records the pool the choice was made over, so an id can be mapped back later.
func (s *Selection) SetPool(pool []Candidate) { s.pool = append([]Candidate(nil), pool...) }

// Pool returns the pool as it was presented to the selector.
func (s *Selection) Pool() []Candidate { return s.pool }

// SelectionPool is the single place the pool is built: exactly the k finals of this mode.
// Reference executions are not in the pool by construction, so there is no abstention.
func SelectionPool(mode string, k int, finals []Candidate) []Candidate {
	byLabel := map[string]Candidate{}
	for _, c := range finals {
		byLabel[c.Label] = c
	}
	var out []Candidate
	for _, label := range poolLabels(mode, k) {
		if c, ok := byLabel[label]; ok {
			out = append(out, c)
		}
	}
	return out
}

// poolLabels names the finals for a mode: agg1..aggk when parallel, c1..ck when sequential.
func poolLabels(mode string, k int) []string {
	out := make([]string, 0, k)
	for i := 1; i <= k; i++ {
		if mode == "parallel" {
			out = append(out, fmt.Sprintf("agg%d", i))
		} else {
			out = append(out, fmt.Sprintf("c%d", i))
		}
	}
	return out
}

// Drop records a candidate excluded before anonymising, with its cause. A drop is never silent:
// a pool that quietly shrank is a pool the ledger counted but never produced.
func (s *Selection) Drop(label, reason string) {
	if strings.TrimSpace(reason) == "" {
		reason = "empty answer"
	}
	s.Dropped = append(s.Dropped, fmt.Sprintf("%s (%s)", label, truncateOutput(reason, 200)))
}

// BuildPool drops non-answers, records each drop, then anonymises what is left.
func (s *Selection) BuildPool(finals []Candidate) []Candidate {
	s.NCandidates = len(finals)
	var pool []Candidate
	for _, c := range finals {
		if len(c.Tree) == 0 {
			s.Drop(c.Label, "no components")
			continue
		}
		if problems := c.Tree.Validate(); len(problems) > 0 {
			s.Drop(c.Label, "invalid: "+strings.Join(problems, "; "))
			continue
		}
		if renderTree(c.Tree) == "" {
			s.Drop(c.Label, "rendered empty")
			continue
		}
		pool = append(pool, c)
	}
	var labels []string
	for _, c := range pool {
		labels = append(labels, c.Label)
	}
	s.IDMap = AssignIDs(labels, s.TaskID, s.K, s.Mode)
	s.SetPool(pool)
	return pool
}

// Resolve maps an anonymous choice back to the candidate it came from.
func (s *Selection) Resolve(anon string) (*Candidate, bool) {
	label, ok := s.IDMap[strings.TrimSpace(anon)]
	if !ok {
		return nil, false
	}
	s.ChosenID = strings.TrimSpace(anon)
	s.ChosenLabel = label
	c := findByLabel(s.pool, label)
	return c, c != nil
}

// Choose records the outcome of a blind selection.
func (s *Selection) Choose(anon, why string) (*Candidate, error) {
	c, ok := s.Resolve(anon)
	if !ok {
		s.Error = fmt.Sprintf("selection returned unknown id %q", anon)
		return nil, fmt.Errorf("%s", s.Error)
	}
	s.Ok = true
	s.Why = strings.TrimSpace(why)
	return c, nil
}
