package harness

import (
	"strings"
	"testing"
)

func TestSeedTreePassesItsOwnValidation(t *testing.T) {
	t.Parallel()
	tree := SeedTree()
	if problems := tree.Validate(); len(problems) != 0 {
		t.Fatalf("seeded surface fails validation: %v", problems)
	}
	if len(PrioritySkills) != 5 {
		t.Fatalf("priority skills = %d", len(PrioritySkills))
	}
	for _, s := range PrioritySkills {
		if tree[s.Path] == "" {
			t.Fatalf("skill %s was not seeded", s.Name)
		}
		if !strings.Contains(tree[AgentsFile], s.Name) {
			t.Fatalf("skill %s has no trigger line in always-on text", s.Name)
		}
	}
	if got := len(tree.Skills()); got != 5 {
		t.Fatalf("skills parsed = %d", got)
	}
	if n := tree.AlwaysOnChars(); n > DefaultSpec().AlwaysOnMax {
		t.Fatalf("seeded always-on text %d chars exceeds cap %d", n, DefaultSpec().AlwaysOnMax)
	}
}

func TestSeedTreeReferencesOnlyScriptsThatExist(t *testing.T) {
	t.Parallel()
	tree := SeedTree()
	for _, s := range tree.Skills() {
		for _, ref := range tree.Scripts() {
			if strings.Contains(s.Body, ref) && tree[ref] == "" {
				t.Fatalf("skill %s references missing script %s", s.Name, ref)
			}
		}
	}
	if len(tree.Scripts()) == 0 {
		t.Fatal("the verification skill needs its deterministic helper")
	}
}
