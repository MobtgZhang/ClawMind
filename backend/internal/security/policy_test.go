package security

import (
	"testing"
	"time"
)

func TestPolicyAddsPatternsWithoutRemovingTheFloor(t *testing.T) {
	p := ResolvePolicy(Policy{Denied: []string{"shutdown"}, HighRisk: []string{"git push"}, ShellTimeout: 5 * time.Second})
	if !p.CommandDenied("shutdown now") {
		t.Fatal("added denied pattern did not take effect")
	}
	if !p.CommandHighRisk("git push") {
		t.Fatal("added high-risk pattern did not take effect")
	}
	if !p.CommandDenied("rm -rf /") {
		t.Fatal("the built-in floor must survive a patch layer")
	}
	if p.ShellTimeout != 5*time.Second {
		t.Fatalf("shell timeout = %v, want the lowered value", p.ShellTimeout)
	}

	raised := ResolvePolicy(Policy{ShellTimeout: 5 * time.Minute})
	if raised.ShellTimeout != MaxShellTimeout {
		t.Fatalf("shell timeout = %v, a patch layer may not raise the ceiling", raised.ShellTimeout)
	}
}
