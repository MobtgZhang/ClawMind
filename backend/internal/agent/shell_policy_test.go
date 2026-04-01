package agent

import "testing"

func TestShellCommandDenied(t *testing.T) {
	if !ShellCommandDenied("rm -rf /") {
		t.Fatal("expected denied")
	}
	if !ShellCommandDenied(`:(){ :|:& };:`) {
		t.Fatal("expected denied")
	}
	if ShellCommandDenied("ls -la") {
		t.Fatal("unexpected deny")
	}
	if ShellCommandDenied("rm -rf ./build") {
		t.Fatal("project clean should not hit global denylist")
	}
}

func TestShellRiskHigh_IncludesDenied(t *testing.T) {
	if !ShellRiskHigh("rm -rf /") {
		t.Fatal("denied commands should be treated as high risk for policy consistency")
	}
}
