package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mobtgzhang/clawmind/backend/internal/domain"
)

func TestHarnessRoundAndEditCRUD(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	round := &domain.HarnessRound{
		ProjectID: "p1", Mode: "parallel", K: 2, Adopted: "r1-INV-1",
		Notes: "技能有明确触发条件", Inventory: "1 skills, 320 always-on chars",
		AlwaysOn: 320, KeptBase: false, Errors: []string{"always-on text exceeds budget"},
	}
	if err := s.CreateHarnessRound(ctx, round); err != nil {
		t.Fatal(err)
	}
	if round.ID == "" {
		t.Fatal("round id should be assigned")
	}
	rounds, err := s.ListHarnessRounds(ctx, "p1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 1 {
		t.Fatalf("rounds = %d", len(rounds))
	}
	if len(rounds[0].Errors) != 1 || rounds[0].Adopted != "r1-INV-1" {
		t.Fatalf("round = %+v", rounds[0])
	}

	edits := []domain.HarnessEdit{
		{Component: "AGENTS.md", Candidates: "r1-INV-1", Change: "加了触发句", Verdict: domain.VerdictHelped},
		{Component: "scripts/check.py", Change: "新增脚本", Verdict: domain.VerdictConfounded},
	}
	if err := s.AddHarnessEdits(ctx, round.ID, edits); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListHarnessEdits(ctx, round.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("edits = %d", len(got))
	}
	if got[0].Verdict != domain.VerdictHelped || got[1].Verdict != domain.VerdictConfounded {
		t.Fatalf("edits = %+v", got)
	}
}

func TestHarnessLessonDeduplicates(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	l := &domain.HarnessLesson{ProjectID: "p1", Cue: "改动公共 API 时", Lesson: "先 grep 调用点", Confidence: "medium"}
	if err := s.AddHarnessLesson(ctx, l); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHarnessLesson(ctx, l); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHarnessLesson(ctx, &domain.HarnessLesson{ProjectID: "p1", Cue: "改动公共 API 时", Lesson: "改完跑受影响测试", Confidence: "high"}); err != nil {
		t.Fatal(err)
	}
	lessons, err := s.ListHarnessLessons(ctx, "p1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lessons) != 2 {
		t.Fatalf("lessons = %d", len(lessons))
	}
	if err := s.AddHarnessLesson(ctx, &domain.HarnessLesson{ProjectID: "p1", Cue: "x"}); err != nil {
		t.Fatal(err)
	}
	lessons, _ = s.ListHarnessLessons(ctx, "p1", 10)
	if len(lessons) != 2 {
		t.Fatalf("empty lesson should not be stored, got %d", len(lessons))
	}
}
