package harness

import (
	"strings"
	"testing"
)

func TestPromptBlockIndexesSubagentsForDelegate(t *testing.T) {
	tree := Tree{
		AgentsFile:                       "需要审查一段改动时，加载 code-review 技能。\n",
		"skills/code-review/SKILL.md":    "---\nname: code-review\ndescription: 审查改动\n---\n正文\n",
		"sub_agents/researcher/AGENT.md": "---\nname: researcher\ndescription: bounded investigation\nmode: subagent\n---\n只读，简短回答。\n",
	}
	block := tree.PromptBlock(6000)
	if !strings.Contains(block, "可委派的子代理") {
		t.Fatalf("block = %q", block)
	}
	if !strings.Contains(block, "researcher：bounded investigation") {
		t.Fatalf("subagent was not indexed: %q", block)
	}
	if !strings.Contains(block, "可用技能") {
		t.Fatalf("skill index missing: %q", block)
	}
}

func TestWithContributionsMaterialisesPluginComponents(t *testing.T) {
	base := Tree{AgentsFile: "角色说明。\n"}
	view := base.WithContributions(
		[]Skill{{Name: "plugin_skill", Description: "from a plugin", Body: "body text"}},
		[]Subagent{{Name: "plugin_agent", Description: "from a plugin", Body: "agent body"}},
	)
	if len(base) != 1 {
		t.Fatalf("the original tree was modified: %v", base)
	}
	skill, ok := view[SkillDir+"/plugin_skill/SKILL.md"]
	if !ok || !strings.Contains(skill, "from a plugin") {
		t.Fatalf("plugin skill was not materialised: %v", view)
	}
	agent, ok := view[SubagentDir+"/plugin_agent/AGENT.md"]
	if !ok || !strings.Contains(agent, "agent body") {
		t.Fatalf("plugin subagent was not materialised: %v", view)
	}
	if len(view.Subagents()) != 1 {
		t.Fatalf("subagents = %v", view.Subagents())
	}
}
