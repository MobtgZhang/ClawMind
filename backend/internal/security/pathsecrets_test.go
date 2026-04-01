package security

import "testing"

func TestBlockAgentReadPath(t *testing.T) {
	if !BlockAgentReadPath(".clawmind/config.json") {
		t.Fatal("expected block")
	}
	if !BlockAgentReadPath("proj/.clawmind/config.json") {
		t.Fatal("expected block")
	}
	if BlockAgentReadPath("src/main.go") {
		t.Fatal("unexpected block")
	}
	if BlockAgentReadPath(".clawmind/skills.json") {
		t.Fatal("skills.json should remain readable for agent workflows")
	}
}
