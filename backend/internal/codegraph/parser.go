package codegraph

import (
	"path/filepath"
	"regexp"
	"strings"
)

// The graph is a heuristic by design: a full AST per language would need a new dependency, and the
// loop only needs definitions and reference sites, not type correctness. Every result the graph
// produces is reported as a candidate, never as proof.
type defPattern struct {
	re   *regexp.Regexp
	kind string
}

type languagePatterns struct {
	pkg     *regexp.Regexp
	imports []*regexp.Regexp
	defs    []defPattern
	calls   []*regexp.Regexp
}

var patterns = map[string]*languagePatterns{
	"Go": {
		pkg:     regexp.MustCompile(`^package\s+([A-Za-z_][A-Za-z0-9_]*)`),
		imports: []*regexp.Regexp{regexp.MustCompile(`^\s*(?:[\w.]+\s+)?"([^"]+)"`)},
		defs: []defPattern{
			{regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`), "func"},
			{regexp.MustCompile(`^type\s+([A-Za-z_][A-Za-z0-9_]*)\s+(?:struct|interface)`), "type"},
		},
		calls: []*regexp.Regexp{
			regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
			regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
		},
	},
	"Python": {
		imports: []*regexp.Regexp{
			regexp.MustCompile(`^\s*import\s+([A-Za-z_][A-Za-z0-9_.]*)`),
			regexp.MustCompile(`^\s*from\s+([A-Za-z_][A-Za-z0-9_.]*)\s+import`),
		},
		defs: []defPattern{
			{regexp.MustCompile(`^(?:\s*)def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`), "func"},
			{regexp.MustCompile(`^(?:\s*)class\s+([A-Za-z_][A-Za-z0-9_]*)\s*[\(:]`), "class"},
		},
		calls: []*regexp.Regexp{
			regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.]*)\.([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
			regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
		},
	},
	"TypeScript": {
		imports: []*regexp.Regexp{
			regexp.MustCompile(`import\s+(?:type\s+)?[^;]*?from\s+["']([^"']+)["']`),
			regexp.MustCompile(`require\(["']([^"']+)["']\)`),
		},
		defs: []defPattern{
			{regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_][A-Za-z0-9_$]*)\s*\(`), "func"},
			{regexp.MustCompile(`^(?:export\s+)?class\s+([A-Za-z_][A-Za-z0-9_$]*)`), "class"},
			{regexp.MustCompile(`^(?:export\s+)?const\s+([A-Za-z_][A-Za-z0-9_$]*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_][A-Za-z0-9_$]*)\s*=>`), "func"},
		},
		calls: []*regexp.Regexp{
			regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_$]*)\.([A-Za-z_][A-Za-z0-9_$]*)\s*\(`),
			regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_$]*)\s*\(`),
		},
	},
	"JavaScript": {
		imports: []*regexp.Regexp{
			regexp.MustCompile(`import\s+[^;]*?from\s+["']([^"']+)["']`),
			regexp.MustCompile(`require\(["']([^"']+)["']\)`),
		},
		defs: []defPattern{
			{regexp.MustCompile(`^(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_][A-Za-z0-9_$]*)\s*\(`), "func"},
			{regexp.MustCompile(`^(?:export\s+)?class\s+([A-Za-z_][A-Za-z0-9_$]*)`), "class"},
			{regexp.MustCompile(`^(?:export\s+)?const\s+([A-Za-z_][A-Za-z0-9_$]*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_][A-Za-z0-9_$]*)\s*=>`), "func"},
		},
		calls: []*regexp.Regexp{
			regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_$]*)\.([A-Za-z_][A-Za-z0-9_$]*)\s*\(`),
			regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_$]*)\s*\(`),
		},
	},
	"Rust": {
		imports: []*regexp.Regexp{regexp.MustCompile(`^\s*use\s+([A-Za-z_][A-Za-z0-9_:]*)`)},
		defs: []defPattern{
			{regexp.MustCompile(`^(?:pub\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`), "func"},
			{regexp.MustCompile(`^(?:pub\s+)?(?:struct|enum|trait)\s+([A-Za-z_][A-Za-z0-9_]*)`), "type"},
		},
		calls: []*regexp.Regexp{
			regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)::([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
			regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
		},
	},
	"Java": {
		imports: []*regexp.Regexp{regexp.MustCompile(`^\s*import\s+([A-Za-z_][A-Za-z0-9_.]*)`)},
		defs: []defPattern{
			{regexp.MustCompile(`^\s*(?:public|private|protected)?\s*(?:static\s+)?[A-Za-z_][A-Za-z0-9_<>\[\]]*\s+([a-z_][A-Za-z0-9_]*)\s*\(`), "func"},
			{regexp.MustCompile(`^\s*(?:public|private|protected)?\s*(?:abstract\s+)?class\s+([A-Za-z_][A-Za-z0-9_]*)`), "class"},
		},
		calls: []*regexp.Regexp{
			regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.]*)\.([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
			regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*\(`),
		},
	},
}

// rawSymbol is a definition found during parsing; the index turns these into Symbol values once
// every file is known.
type rawSymbol struct {
	ID   string
	Name string
	Kind string
	Line int
}

// parseFile turns one file into a graph node, its definitions and its unresolved references.
// References are resolved in build because a reference into another package cannot be named before
// that package has been parsed.
func parseFile(rel, language, content string, maxSymbols int) (*FileNode, []rawSymbol, []PendingCall) {
	node := &FileNode{Path: rel, Language: language, Package: packageOf(rel, language, content)}
	pat := patterns[language]
	if pat == nil {
		return node, nil, nil
	}

	lines := strings.Split(content, "\n")
	var symbols []rawSymbol
	seenSymbol := map[string]struct{}{}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, re := range pat.imports {
			if m := re.FindStringSubmatch(line); m != nil {
				node.Imports = append(node.Imports, m[1])
				break
			}
		}
		for _, dp := range pat.defs {
			m := dp.re.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			name := m[1]
			id := rel + "#" + name
			if _, ok := seenSymbol[id]; ok {
				break
			}
			if len(symbols) >= maxSymbols {
				break
			}
			seenSymbol[id] = struct{}{}
			symbols = append(symbols, rawSymbol{ID: id, Name: name, Kind: dp.kind, Line: i + 1})
			node.Symbols = append(node.Symbols, id)
			break
		}
	}

	// Owner attribution: a reference belongs to the definition that encloses it. Without a real
	// AST, the most recent definition above the reference line is the honest approximation, and it
	// is stated as such wherever the graph is rendered.
	defIdx := 0
	owner := ""
	var pending []PendingCall
	for i, line := range lines {
		for defIdx < len(symbols) && symbols[defIdx].Line <= i+1 {
			owner = symbols[defIdx].ID
			defIdx++
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		for _, re := range pat.calls {
			// Every match on the line counts: `fmt.Println(store.Load())` references two symbols,
			// and taking only the first one loses the reference the analysis was asked about.
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				// A one-group pattern carries no qualifier: leaving it as the name makes an
				// unqualified call look like a cross-package one, and the resolver then looks for an
				// import that does not exist instead of the definition in the same file.
				qualifier, name := "", m[1]
				if len(m) > 2 {
					qualifier, name = m[1], m[2]
				}
				if name == "" || name == node.Package || controlWords[name] {
					continue
				}
				call := PendingCall{Owner: owner, Name: name, Qualifier: qualifier, Line: i + 1}
				if call.Owner == "" {
					call.Owner = rel
				}
				pending = append(pending, call)
			}
		}
	}

	return node, symbols, pending
}

var controlWords = map[string]bool{
	"if": true, "for": true, "switch": true, "return": true, "while": true, "func": true,
	"def": true, "catch": true, "except": true, "case": true, "match": true, "select": true,
	"range": true, "defer": true, "go": true, "print": true, "println": true, "console": true,
}

func packageOf(rel, language, content string) string {
	pat := patterns[language]
	if pat != nil && pat.pkg != nil {
		if m := pat.pkg.FindStringSubmatch(content); m != nil {
			return m[1]
		}
	}
	dir := filepath.Dir(filepath.FromSlash(rel))
	if dir == "." || dir == "/" {
		return language
	}
	return filepath.Base(dir)
}
