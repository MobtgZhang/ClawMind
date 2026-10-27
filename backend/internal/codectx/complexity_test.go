package codectx

import (
	"testing"
)

func TestClassifySeparatesLargeFromSmallWorkspaces(t *testing.T) {
	large := &Profile{
		Scanned:   900,
		Languages: []LanguageStat{{Name: "go"}, {Name: "ts"}, {Name: "py"}},
		Manifests: []string{"go.mod", "package.json", "pyproject.toml"},
		Commands:  Commands{Test: "go test ./..."},
		TestFiles: 12,
		HasCI:     true,
	}
	shape := GraphShape{Files: 900, Symbols: 1500, Modules: 14, MaxFanIn: 18, Cycles: 1}

	b := Classify(large, shape, "重构这个模块的依赖边界", 8, 24000)
	if b.Class != ClassComplex || b.Loop != "graph" {
		t.Fatalf("class=%s loop=%s, want a complex project to run the graph shape", b.Class, b.Loop)
	}
	if b.Reentry < 2 || b.Fanout < 2 || b.MaxNodes < 8 {
		t.Fatalf("complex shape is not a graph: %+v", b)
	}
	if len(b.Reasons) == 0 {
		t.Fatal("a shape that cannot be read cannot be audited")
	}

	small := &Profile{Scanned: 6, Languages: []LanguageStat{{Name: "go"}}}
	b = Classify(small, GraphShape{Files: 6, Symbols: 14}, "加个日志", 8, 24000)
	if b.Class != ClassSimple || b.Loop != "linear" {
		t.Fatalf("class=%s loop=%s, want a small workspace to stay a single pass", b.Class, b.Loop)
	}
	if b.MaxNodes > 6 || b.Fanout > 1 {
		t.Fatalf("simple shape grew into a graph: %+v", b)
	}
}

// A large repository asked for a one-line change is still a one-line change: the graph would be paid
// for a change that cannot break a dependant.
func TestLargeWorkspaceWithOneLineRequestStaysSimple(t *testing.T) {
	large := &Profile{Scanned: 900, Languages: []LanguageStat{{Name: "go"}, {Name: "ts"}, {Name: "py"}}, Manifests: []string{"go.mod", "package.json", "pyproject.toml"}}
	shape := GraphShape{Files: 900, Symbols: 1500, Modules: 14, MaxFanIn: 18, Cycles: 1}
	b := Classify(large, shape, "改一行拼写错误", 8, 24000)
	if b.Class != ClassSimple {
		t.Fatalf("class=%s, want the request to override the workspace size", b.Class)
	}
}

// The reverse case: a small repository asked for a boundary change has dependants to check, so the
// score has to reach the graph even without the size signals.
func TestSmallWorkspaceWithBoundaryRequestIsComplex(t *testing.T) {
	small := &Profile{Scanned: 45, Languages: []LanguageStat{{Name: "go"}}}
	shape := GraphShape{Files: 45, Symbols: 210, Modules: 4, MaxFanIn: 5}
	b := Classify(small, shape, "refactor the contract across modules", 8, 24000)
	if b.Class != ClassComplex {
		t.Fatalf("class=%s score=%d reasons=%v, want the request to reach the graph", b.Class, b.Score, b.Reasons)
	}
}

// The classifier may raise a cap it was given, but it must not replace one the operator set: a
// reported reason that describes a value the runtime never used is not a report.
func TestBoundedIntKeepsOperatorValues(t *testing.T) {
	if got := boundedInt(20, 16, 32); got != 20 {
		t.Fatalf("boundedInt(20,16,32) = %d, want the operator value kept", got)
	}
	if got := boundedInt(0, 16, 32); got != 16 {
		t.Fatalf("boundedInt(0,16,32) = %d, want the class default for an unset value", got)
	}
	if got := boundedInt(64, 16, 32); got != 32 {
		t.Fatalf("boundedInt(64,16,32) = %d, want the class ceiling", got)
	}
}

// An override has to produce the same numbers the score would have produced, otherwise the flag
// changes the budgets in a way the operator cannot predict from the class it set.
func TestFixedBudgetMatchesTheScoredShape(t *testing.T) {
	large := &Profile{Scanned: 900, Languages: []LanguageStat{{Name: "go"}}}
	shape := GraphShape{Files: 900, Symbols: 1500, Modules: 14, MaxFanIn: 18}
	scored := Classify(large, shape, "refactor the pipeline", 12, 30000)
	forced := FixedBudget(ClassComplex, 12, 30000)
	if scored.Class != ClassComplex {
		t.Fatalf("scored class=%s, want complex for the comparison", scored.Class)
	}
	if scored.MaxRounds != forced.MaxRounds || scored.ContextTokens != forced.ContextTokens || scored.MaxNodes != forced.MaxNodes {
		t.Fatalf("forced shape %+v differs from scored shape %+v", forced, scored)
	}
}
