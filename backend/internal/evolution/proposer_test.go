package evolution

import (
	"strings"
	"testing"

	"github.com/mobtgzhang/clawmind/backend/internal/harness"
)

func TestParseProposal_appliesFencedBlocksAndDeletes(t *testing.T) {
	base := harness.Tree{harness.AgentsFile: "old\n", "skills/a/SKILL.md": "keep\n"}
	reply := "<proposal>\n```path\nAGENTS.md\nnew text\n```\n</proposal>\n<delete>\nskills/a/SKILL.md\n</delete>\n<notes>\n轨迹中模型改完没有跑测试。\n</notes>"
	p := ParseProposal(reply, base)
	if p.Tree[harness.AgentsFile] != "new text" {
		t.Fatalf("agents = %q", p.Tree[harness.AgentsFile])
	}
	if _, ok := p.Tree["skills/a/SKILL.md"]; ok {
		t.Fatal("delete not applied")
	}
	if p.KeptBase() {
		t.Fatal("changed proposal should not report KeptBase")
	}
	if !strings.Contains(p.Notes, "测试") {
		t.Fatalf("notes = %q", p.Notes)
	}
}

func TestParseProposal_keepsBaseWhenNothingProposed(t *testing.T) {
	base := harness.Tree{harness.AgentsFile: "old\n"}
	p := ParseProposal("<proposal></proposal>\n<notes>KEEP_BASE：证据不支持改动。</notes>", base)
	if !p.KeptBase() {
		t.Fatalf("tree = %v", p.Tree)
	}
	if !strings.Contains(p.Notes, "KEEP_BASE") {
		t.Fatalf("notes = %q", p.Notes)
	}
}

func TestParseProposal_ignoresOffSurfacePaths(t *testing.T) {
	base := harness.Tree{harness.AgentsFile: "old\n"}
	p := ParseProposal("<proposal>\n```path\nREADME.md\nx\n```\n</proposal>", base)
	if _, ok := p.Tree["README.md"]; ok {
		t.Fatal("off-surface path should be dropped")
	}
	if !p.KeptBase() {
		t.Fatalf("tree = %v", p.Tree)
	}
}

func TestParseProbe(t *testing.T) {
	p := parseProbe("<probe>\nlabel: r1-INV-1\nscore: 72\nobserved: 会在第 3 步先 grep 调用点\nconfounded: true\n</probe>", "r1-INV-1")
	if p.Label != "r1-INV-1" || p.Score != 72 || !p.Confounded || p.Observed == "" {
		t.Fatalf("probe = %+v", p)
	}
}

func TestParseChoice(t *testing.T) {
	if got := parseChoice("推理若干行\n\n```choice\nB\n```"); got != "B" {
		t.Fatalf("choice = %q", got)
	}
}

func TestParseExperience(t *testing.T) {
	exp := parseExperience("<experience>\ncue: 改动公共 API 时\nlesson: 先 grep 调用点\nevidence: edit 1\nconfidence: high\n</experience>")
	if exp["cue"] == "" || exp["lesson"] == "" || exp["confidence"] != "high" {
		t.Fatalf("experience = %v", exp)
	}
	if got := parseExperience("<experience>\nNO_ENTRY\n</experience>"); got != nil {
		t.Fatalf("NO_ENTRY should produce nothing, got %v", got)
	}
}

func TestParseEditRows(t *testing.T) {
	rows := parseEditRows("<edits>\ncomponent: AGENTS.md\ncandidates: r1-INV-1\nchange: 加了触发句\nobserved: 第 2 步先搜索\nverdict: helped\n</edits>")
	if len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
	if rows[0].Component != "AGENTS.md" || rows[0].Verdict != "helped" {
		t.Fatalf("row = %+v", rows[0])
	}
}
