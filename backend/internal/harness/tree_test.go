package harness

import (
	"strings"
	"testing"
)

func TestValidate_acceptsSkillWithTriggerAndScript(t *testing.T) {
	tree := Tree{
		AgentsFile:               "遇到跨文件重命名时，加载 rename 技能。\n",
		"skills/rename/SKILL.md": "---\nname: rename\ndescription: 跨文件重命名符号\n---\n先 grep 调用点，再运行 scripts/check.py。\n",
		"scripts/check.py":       "print(1)\n",
	}
	if problems := tree.Validate(); len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
}

func TestValidate_rejectsSkillWithoutTrigger(t *testing.T) {
	tree := Tree{
		AgentsFile:              "角色说明。",
		"skills/other/SKILL.md": "---\nname: other\ndescription: 某种情形\n---\n正文\n",
	}
	problems := tree.Validate()
	if len(problems) == 0 {
		t.Fatal("expected a missing-trigger problem")
	}
	if !strings.Contains(problems[0], "not named in always-on text") {
		t.Fatalf("problems = %v", problems)
	}
}

func TestValidate_rejectsTemplateBracesAndMissingScript(t *testing.T) {
	tree := Tree{
		AgentsFile:            "用 {{value}} 填充。加载 x 技能。",
		"skills/x/SKILL.md":   "---\nname: x\ndescription: d\n---\n运行 scripts/missing.py\n",
		"scripts/Bad_Name.py": "print(1)\n",
	}
	problems := tree.Validate()
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "template braces") {
		t.Fatalf("expected template brace problem, got %v", problems)
	}
	if !strings.Contains(joined, "missing script scripts/missing.py") {
		t.Fatalf("expected missing script problem, got %v", problems)
	}
	if !strings.Contains(joined, "snake_case") {
		t.Fatalf("expected script name problem, got %v", problems)
	}
}

func TestAutoTriggerAddsMissingTrigger(t *testing.T) {
	tree := Tree{
		AgentsFile:                   "角色说明。",
		"skills/grep_calls/SKILL.md": "---\nname: grep_calls\ndescription: 改动公共 API 前先查调用点\n---\n正文\n",
	}
	fixed := tree.AutoTrigger()
	if len(fixed) != 1 || fixed[0] != "grep_calls" {
		t.Fatalf("fixed = %v", fixed)
	}
	if problems := tree.Validate(); len(problems) != 0 {
		t.Fatalf("after auto trigger, problems = %v", problems)
	}
}

func TestDiffReportsComponents(t *testing.T) {
	base := Tree{AgentsFile: "a", "skills/x/SKILL.md": "b"}
	next := Tree{AgentsFile: "a2", "scripts/y.py": "c"}
	d := Diff(base, next)
	if d.Empty() {
		t.Fatal("diff should not be empty")
	}
	if !d.Contains(AgentsFile) || !d.Contains("scripts/y.py") || !d.Contains("skills/x/SKILL.md") {
		t.Fatalf("components = %v", d.Components())
	}
	if got := Diff(base, base); !got.Empty() {
		t.Fatalf("KEEP_BASE diff should be empty, got %v", got.Components())
	}
}

func TestAlwaysOnCharsCountsOnlyAlwaysOnComponents(t *testing.T) {
	tree := Tree{
		AgentsFile:          "12345",
		LessonsFile:         "678",
		"skills/x/SKILL.md": "should not count",
	}
	if got := tree.AlwaysOnChars(); got != 8 {
		t.Fatalf("always on chars = %d, want 8", got)
	}
	if len(tree.Skills()) != 1 || tree.Skills()[0].Name != "x" {
		t.Fatalf("skills = %+v", tree.Skills())
	}
}
