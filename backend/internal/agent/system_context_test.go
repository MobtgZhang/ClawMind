package agent

import (
	"strings"
	"testing"
	"time"
)

func TestSystemRuntimeContextBlock(t *testing.T) {
	ts := time.Date(2026, 3, 30, 15, 4, 5, 0, time.FixedZone("CST", 8*3600))
	s := systemRuntimeContextBlock(ts)
	if !strings.Contains(s, "ClawMind") {
		t.Fatal("missing product name")
	}
	if !strings.Contains(s, "2026-03-30") {
		t.Fatal("missing date")
	}
	if !strings.Contains(s, "RFC3339") {
		t.Fatal("missing RFC3339 label")
	}
}
