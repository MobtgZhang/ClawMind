package evolution

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
	"github.com/mobtgzhang/clawmind/backend/internal/store"
)

func TestRunRoundDryModeKeepsBaseAndRecordsRound(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	e := New(st, nil, Config{K: 2, Mode: "dry", Root: t.TempDir()})
	res, err := e.RunRound(context.Background(), "p1", "", "证据：模型改完没有跑测试。")
	if err != nil {
		t.Fatal(err)
	}
	if res.Round == nil || res.Round.ID == "" {
		t.Fatalf("round = %+v", res.Round)
	}
	if !res.Round.KeptBase {
		t.Fatalf("dry mode should keep base: %+v", res.Round)
	}
	if res.Round.Inventory == "" {
		t.Fatal("inventory should be recorded")
	}
	rounds, err := st.ListHarnessRounds(context.Background(), "p1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 1 {
		t.Fatalf("rounds = %d", len(rounds))
	}
	got, err := harness.Read(e.Dir("p1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("harness should be written to disk")
	}
	if _, err := os.Stat(filepath.Join(e.Dir("p1"), "INDEX.md")); err != nil {
		t.Fatal("INDEX.md should be written")
	}
}

// stubCaller answers each stage by the mission text, so the whole pipeline runs without a model.
type stubCaller struct{}

func (stubCaller) Complete(ctx context.Context, p llm.CompleteParams) (*llm.CompleteResult, error) {
	mission := p.Messages[0].Content
	switch {
	case strings.Contains(mission, "编辑器"):
		return &llm.CompleteResult{Content: "" +
			"<proposal>\n" + "```path\nAGENTS.md\n当改动公共 API 时，先 grep 调用点，并加载 grep_calls 技能。\n```\n" +
			"```path\nskills/grep_calls/SKILL.md\n---\nname: grep_calls\ndescription: 改动公共 API 前先查调用点\n---\n先 code_search 调用点，再改动。\n```\n" +
			"</proposal>\n<notes>轨迹中模型直接改函数，没有查调用点。</notes>"}, nil
	case strings.Contains(mission, "评估"):
		return &llm.CompleteResult{Content: "<probe>\nlabel: r1-INV-1\nscore: 80\nobserved: 会在第 2 步先搜索调用点\nconfounded: false\n</probe>"}, nil
	case strings.Contains(mission, "选出"):
		return &llm.CompleteResult{Content: "<why>技能有明确触发条件，常驻文本最少。</why>\n\n```choice\nA\n```"}, nil
	case strings.Contains(mission, "组装"):
		return &llm.CompleteResult{Content: "" +
			"<proposal>\n" + "```path\nAGENTS.md\n当改动公共 API 时，先 grep 调用点，并加载 grep_calls 技能。\n```\n" +
			"```path\nskills/grep_calls/SKILL.md\n---\nname: grep_calls\ndescription: 改动公共 API 前先查调用点\n---\n先 code_search 调用点，再改动。\n```\n" +
			"</proposal>\n<notes>KEEP_BASE 之外只保留这一项。</notes>"}, nil
	case strings.Contains(mission, "改进记录"):
		return &llm.CompleteResult{Content: "" +
			"<edits>\ncomponent: skills/grep_calls/SKILL.md\ncandidates: r1-INV-1\nchange: 新增按需技能\nobserved: 第 2 步先搜索调用点\nverdict: helped\n</edits>\n\n" +
			"<experience>\ncue: 改动公共 API 或跨模块符号时\nlesson: 先 grep 调用点，改完跑受影响包的测试\nevidence: edit row 1\nconfidence: medium\n</experience>"}, nil
	}
	return &llm.CompleteResult{Content: ""}, nil
}

func TestRunRoundAppliesProposalAndRecordsLesson(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	e := New(st, stubCaller{}, Config{K: 1, Mode: "parallel", Root: t.TempDir()})
	res, err := e.RunRound(context.Background(), "p2", "", "轨迹：模型改了公共函数签名，没有查调用点，也没有跑测试。")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("candidates = %d", len(res.Candidates))
	}
	if res.Chosen == "" {
		t.Fatal("blind selection should pick a candidate")
	}
	if _, ok := res.Final["skills/grep_calls/SKILL.md"]; !ok {
		t.Fatalf("final harness = %v", res.Final)
	}
	if len(res.Edits) == 0 || res.Edits[0].Verdict != "helped" {
		t.Fatalf("edits = %v", res.Edits)
	}
	if res.Lesson == nil || res.Lesson.Lesson == "" {
		t.Fatalf("lesson = %+v", res.Lesson)
	}
	lessons, err := st.ListHarnessLessons(context.Background(), "p2", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lessons) != 1 {
		t.Fatalf("lessons = %d", len(lessons))
	}
	if res.Round.KeptBase {
		t.Fatal("the round changed the harness, KeptBase should be false")
	}
}

func TestRunRoundDiscardsInvalidProposal(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	e := New(st, invalidCaller{}, Config{K: 1, Mode: "parallel", Root: t.TempDir()})
	res, err := e.RunRound(context.Background(), "p3", "", "证据：一次偶发的读取失败。")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Round.Errors) == 0 {
		t.Fatal("expected a validation error")
	}
	if !res.Round.KeptBase {
		t.Fatalf("invalid proposal should be discarded entire, got %v", res.Final)
	}
}

// invalidCaller proposes a skill with no trigger line: the whole proposal must be dropped.
type invalidCaller struct{}

func (invalidCaller) Complete(ctx context.Context, p llm.CompleteParams) (*llm.CompleteResult, error) {
	mission := p.Messages[0].Content
	proposal := "<proposal>\n```path\nskills/no_trigger/SKILL.md\n---\nname: no_trigger\ndescription: 某种情形\n---\n正文\n```\n</proposal>\n<notes>无依据。</notes>"
	if strings.Contains(mission, "编辑器") {
		return &llm.CompleteResult{Content: proposal}, nil
	}
	if strings.Contains(mission, "组装") {
		return &llm.CompleteResult{Content: proposal}, nil
	}
	return &llm.CompleteResult{Content: ""}, nil
}
