package evolution

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/store"
)

func TestExecProbeRunsCandidateScriptsAndGradesTheResult(t *testing.T) {
	base := harness.Tree{harness.AgentsFile: "角色说明。加载 grep 技能。\n"}
	c := Candidate{
		Label: "INV1",
		Dim:   "INV",
		Base:  base,
		Tree: harness.Tree{
			harness.AgentsFile: "角色说明。加载 grep 技能。\n",
			"scripts/check.py": "print('ok')\n",
		},
	}
	p := NewExecProbe("")
	p.TestCommand = "true"
	ev := p.Probe(context.Background(), c, "轨迹：改完没有跑测试。")
	if !ev.Executed || !ev.Graded {
		t.Fatalf("probe should have executed: %+v", ev)
	}
	if !ev.TestPassed {
		t.Fatalf("test should have passed: %+v", ev)
	}
	if ev.FilesChanged != 1 || ev.Confounded {
		t.Fatalf("single component change should not be confounded: %+v", ev)
	}
	if ev.Score() <= 0 {
		t.Fatalf("a measured run must score above the prediction floor: %+v", ev)
	}
}

func TestExecProbeMarksMultiComponentChangesAsConfounded(t *testing.T) {
	base := harness.Tree{harness.AgentsFile: "a\n"}
	c := Candidate{
		Label: "INV1", Dim: "INV", Base: base,
		Tree: harness.Tree{
			harness.AgentsFile:  "b\n加载 x 技能。\n",
			"skills/x/SKILL.md": "a\n",
		},
	}
	ev := NewExecProbe("").Probe(context.Background(), c, "证据")
	if !ev.Confounded {
		t.Fatalf("two changed components must be confounded: %+v", ev)
	}
	if ev.Executed {
		t.Fatalf("an invalid proposal is discarded entire, not executed: %+v", ev)
	}
	if !strings.Contains(ev.Observed, "not executed") {
		t.Fatalf("observed = %q", ev.Observed)
	}
}

func TestExecProbeRefusesDeniedTestCommand(t *testing.T) {
	c := Candidate{Label: "INV1", Dim: "INV", Base: harness.Tree{harness.AgentsFile: "a"}, Tree: harness.Tree{harness.AgentsFile: "a"}}
	p := NewExecProbe("")
	p.TestCommand = "rm -rf /"
	ev := p.Probe(context.Background(), c, "证据")
	if ev.Executed || ev.TestPassed {
		t.Fatalf("denied command must not run: %+v", ev)
	}
	if !strings.Contains(ev.Error, "denied") {
		t.Fatalf("error = %q", ev.Error)
	}
}

func TestInferTestCommandOnlyUsesCommandsPresentInEvidence(t *testing.T) {
	if got := InferTestCommand("第 4 步运行 go test ./... 失败"); got != "go test ./..." {
		t.Fatalf("inferred = %q", got)
	}
	if got := InferTestCommand("模型直接改了文件，没有任何命令"); got != "" {
		t.Fatalf("inferred should be empty, got %q", got)
	}
}

func TestOrderGradedFirstKeepsOriginalRelativeOrder(t *testing.T) {
	cands := []Candidate{
		{Label: "a", Probe: Probe{Score: 10}},
		{Label: "b", Probe: Probe{Score: 0, Graded: true}},
		{Label: "c", Probe: Probe{Score: 5, Graded: true}},
		{Label: "d", Probe: Probe{Score: 99}},
	}
	ordered := OrderGradedFirst(cands)
	var got []string
	for _, c := range ordered {
		got = append(got, c.Label)
	}
	if strings.Join(got, ",") != "b,c,a,d" {
		t.Fatalf("ordered = %v", got)
	}
}

func TestAssignIDsIsStableAcrossRerunsAndAnonymises(t *testing.T) {
	first := AssignIDs([]string{"agg1", "agg2", "agg3"}, "task-abc", 3, "parallel")
	again := AssignIDs([]string{"agg3", "agg1", "agg2"}, "task-abc", 3, "parallel")
	if len(first) != 3 {
		t.Fatalf("ids = %v", first)
	}
	for id, label := range first {
		if again[id] != label {
			t.Fatalf("mapping is not stable: %v vs %v", first, again)
		}
	}
	if AnonID(0) != "A" || AnonID(25) != "Z" || AnonID(26) != "AA" {
		t.Fatalf("anon ids = %s %s %s", AnonID(0), AnonID(25), AnonID(26))
	}
	other := AssignIDs([]string{"agg1", "agg2", "agg3"}, "task-abc", 3, "sequential")
	same := 0
	for id, label := range first {
		if other[id] == label {
			same++
		}
	}
	if same == len(first) {
		t.Fatal("a different mode must produce a different anonymisation")
	}
}

func TestSelectionPoolIsExactlyTheKFinals(t *testing.T) {
	finals := []Candidate{
		{Label: "INV1", Tree: harness.Tree{harness.AgentsFile: "a"}},
		{Label: "agg1", Tree: harness.Tree{harness.AgentsFile: "b"}},
		{Label: "agg2", Tree: harness.Tree{harness.AgentsFile: "c"}},
	}
	pool := SelectionPool("parallel", 2, finals)
	if len(pool) != 2 || pool[0].Label != "agg1" || pool[1].Label != "agg2" {
		t.Fatalf("pool = %v", labels(pool))
	}
	seq := SelectionPool("sequential", 2, []Candidate{
		{Label: "c1", Tree: harness.Tree{harness.AgentsFile: "b"}},
		{Label: "c2", Tree: harness.Tree{harness.AgentsFile: "c"}},
	})
	if len(seq) != 2 {
		t.Fatalf("sequential pool = %v", labels(seq))
	}
}

func TestSelectionRecordsEveryDrop(t *testing.T) {
	s := NewSelection("task", "parallel", 2)
	pool := []Candidate{
		{Label: "agg1", Tree: harness.Tree{harness.AgentsFile: "加载 x 技能。\n", "skills/x/SKILL.md": "---\nname: x\ndescription: d\n---\n正文\n"}},
		{Label: "agg2", Tree: harness.Tree{}},
	}
	kept := s.BuildPool(pool)
	if len(kept) != 1 || kept[0].Label != "agg1" {
		t.Fatalf("kept = %v", labels(kept))
	}
	if len(s.Dropped) != 1 || !strings.Contains(s.Dropped[0], "agg2") {
		t.Fatalf("dropped = %v", s.Dropped)
	}
	if _, err := s.Choose("A", "reason"); err != nil {
		t.Fatal(err)
	}
	if s.ChosenLabel != "agg1" || !s.Ok {
		t.Fatalf("choice = %+v", s)
	}
	if _, err := s.Choose("Q", ""); err == nil {
		t.Fatal("unknown id must be an error, not a silent abstention")
	}
}

func TestLedgerIdentityAndDuplicateDetection(t *testing.T) {
	l := NewLedger("parallel", "task", 2)
	for i := 1; i <= 2; i++ {
		l.Rollout("base"+string(rune('0'+i)), false, false)
	}
	for _, dim := range []string{"INV", "DOM"} {
		for i := 1; i <= 2; i++ {
			l.Session(dim+string(rune('0'+i)), false, 1)
			l.Rollout(dim+string(rune('0'+i)), true, false)
		}
	}
	for i := 1; i <= 2; i++ {
		l.Session("agg"+string(rune('0'+i)), false, 1)
		l.Rollout("agg"+string(rune('0'+i)), true, false)
	}
	if l.R() != ExpectedRollouts(2) || l.P() != ExpectedSessions(2) {
		t.Fatalf("ledger = %s", l.Summary())
	}
	if err := l.Check(); err != nil {
		t.Fatal(err)
	}
	// 4 candidate probes plus 2 finals were measured; the k shared base rollouts were not.
	if l.MeasuredR() != 6 {
		t.Fatalf("measured = %d", l.MeasuredR())
	}
	l.Session("INV1", false, 1)
	if err := l.Check(); err == nil {
		t.Fatal("duplicate label must fail the identity check even when counts would pass")
	}
}

func TestRunmarkFingerprintIsConfigurationSpecific(t *testing.T) {
	t1 := harness.Tree{harness.AgentsFile: "a"}
	t2 := harness.Tree{harness.AgentsFile: "b"}
	if Fingerprint(t1) == Fingerprint(t2) {
		t.Fatal("different harnesses must digest differently")
	}
	if Fingerprint(t1) != Fingerprint(harness.Tree{harness.AgentsFile: "a"}) {
		t.Fatal("fingerprint must be stable")
	}
	if Fingerprint(harness.Tree{}) != "base" {
		t.Fatal("empty harness digests to base")
	}
	withIndex := harness.Tree{harness.AgentsFile: "a", "INDEX.md": "report"}
	if Fingerprint(t1) != Fingerprint(withIndex) {
		t.Fatal("a report byproduct must not change the harness identity")
	}
	mark := &Runmark{TaskID: "t", HarnessHash: Fingerprint(t1), Mode: "parallel", K: 1, Completed: true, Version: RunmarkVersion}
	if WhyNot(mark, "t", Fingerprint(t1), "parallel", 1) != "reusable" {
		t.Fatalf("why = %s", WhyNot(mark, "t", Fingerprint(t1), "parallel", 1))
	}
	if !strings.Contains(WhyNot(mark, "t", Fingerprint(t2), "parallel", 1), "different configuration") {
		t.Fatalf("why = %s", WhyNot(mark, "t", Fingerprint(t2), "parallel", 1))
	}
	if !strings.Contains(WhyNot(nil, "t", Fingerprint(t1), "parallel", 1), "no runmark") {
		t.Fatalf("why = %s", WhyNot(nil, "t", Fingerprint(t1), "parallel", 1))
	}
}

func TestRunRoundResumesFromACompletedRunmark(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	e := New(st, stubCaller{}, Config{K: 1, Mode: "parallel", Root: t.TempDir(), EnforceLedger: true})
	first, err := e.RunRound(context.Background(), "p9", "", "轨迹：改公共函数没有查调用点。")
	if err != nil {
		t.Fatal(err)
	}
	if first.Round.TaskID == "" || first.Round.Runmark == "" {
		t.Fatalf("round must record task and runmark: %+v", first.Round)
	}
	if first.Round.LedgerR != ExpectedRollouts(1) || first.Round.LedgerP != ExpectedSessions(1) {
		t.Fatalf("ledger = %s", first.Ledger.Summary())
	}
	second, err := e.RunRound(context.Background(), "p9", "", "轨迹：改公共函数没有查调用点。")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Round.Cached {
		t.Fatalf("the second run must reuse the finished one: %+v", second.Round)
	}
	if second.Round.Runmark != first.Round.Runmark {
		t.Fatalf("runmark = %s vs %s", second.Round.Runmark, first.Round.Runmark)
	}
	// A different task is a different trial: it is not reused.
	third, err := e.RunRound(context.Background(), "p9", "", "轨迹：改公共函数没有查调用点，也没有跑测试。")
	if err != nil {
		t.Fatal(err)
	}
	if third.Round.Cached {
		t.Fatal("a different task must not reuse the runmark")
	}
}
