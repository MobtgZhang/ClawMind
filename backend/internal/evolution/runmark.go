package evolution

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/mobtgzhang/clawmind/backend/internal/harness"
)

// RunmarkVersion is bumped when the meaning of a completion marker changes, so an old marker is
// never read as a current one.
const RunmarkVersion = 1

// Runmark records that a run finished, and fingerprints the harness that produced it. Presence of
// output is not completion: only an explicit marker naming the same configuration may be reused.
type Runmark struct {
	ID          string `json:"id"`
	TaskID      string `json:"taskId"`
	HarnessHash string `json:"harnessHash"`
	Mode        string `json:"mode"`
	K           int    `json:"k"`
	RoundID     string `json:"roundId,omitempty"`
	Chosen      string `json:"chosen,omitempty"`
	Completed   bool   `json:"completed"`
	Version     int    `json:"version"`
	CreatedAt   int64  `json:"createdAt"`
}

// byproducts are not part of the harness and must not change its identity.
func byproduct(rel string) bool {
	base := rel
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		base = rel[i+1:]
	}
	if base == "INDEX.md" || base == ".adapt_complete" || base == ".DS_Store" {
		return true
	}
	return strings.HasSuffix(rel, ".pyc") || strings.Contains(rel, "__pycache__")
}

// Fingerprint is a stable digest of the harness tree that produced a run. An empty tree digests
// to "base", which is the shared value for a rollout on the inherited harness.
func Fingerprint(t harness.Tree) string {
	if len(t) == 0 {
		return "base"
	}
	paths := make([]string, 0, len(t))
	for rel := range t {
		if byproduct(rel) {
			continue
		}
		paths = append(paths, rel)
	}
	if len(paths) == 0 {
		return "base"
	}
	sort.Strings(paths)
	h := sha1.New()
	for _, rel := range paths {
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write([]byte(t[rel]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// RunmarkHash is the identity of a trial: task + harness + mode + k. A run produced by a different
// configuration is never reused.
func RunmarkHash(taskID, harnessHash, mode string, k int) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%d|%s|%s|%s|%d", RunmarkVersion, taskID, harnessHash, mode, k)))
	return hex.EncodeToString(sum[:])
}

// WhyNot explains, in one sentence, why a recorded run may or may not be reused.
func WhyNot(mark *Runmark, taskID, harnessHash, mode string, k int) string {
	if mark == nil {
		return "no runmark — never finished, or finished before completion was recorded"
	}
	if mark.Version != RunmarkVersion {
		return fmt.Sprintf("runmark version %d != %d", mark.Version, RunmarkVersion)
	}
	if mark.TaskID != taskID {
		return fmt.Sprintf("task %s != %s", mark.TaskID, taskID)
	}
	if mark.Mode != mode || mark.K != k {
		return fmt.Sprintf("configuration %s-%d != %s-%d", mark.Mode, mark.K, mode, k)
	}
	if want, got := harnessHash, mark.HarnessHash; want != got {
		return fmt.Sprintf("harness fingerprint %s != %s — produced by a different configuration", got, want)
	}
	if !mark.Completed {
		return "runmark exists but completion was never recorded"
	}
	return "reusable"
}
