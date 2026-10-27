package codegraph

import (
	"fmt"
	"sort"
	"strings"
)

// Hit is one graph result: a symbol reached from the query, with the distance it was found at.
type Hit struct {
	Symbol   *Symbol
	Via      string
	Distance int
}

// Lookup returns definitions whose name matches the query, exact first then prefix.
func (ix *Index) Lookup(name string, limit int) []*Symbol {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if limit <= 0 {
		limit = 20
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return nil
	}
	var out []*Symbol
	for _, id := range ix.byName[key] {
		out = append(out, ix.symbols[id])
		if len(out) >= limit {
			return out
		}
	}
	for _, sym := range ix.symbols {
		if len(out) >= limit {
			break
		}
		if strings.HasPrefix(strings.ToLower(sym.Name), key) && sym.Name != key {
			out = append(out, sym)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// Callers walks the reverse call adjacency: who depends on this symbol. This is the query the
// linear loop cannot answer, and the reason the graph exists.
func (ix *Index) Callers(name string, depth, limit int) []Hit {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.walk(name, ix.callers, depth, limit)
}

// Callees walks the forward call adjacency: what this symbol depends on.
func (ix *Index) Callees(name string, depth, limit int) []Hit {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.walk(name, ix.callees, depth, limit)
}

func (ix *Index) walk(name string, adjacency map[string][]string, depth, limit int) []Hit {
	if limit <= 0 {
		limit = 40
	}
	if depth <= 0 {
		depth = 2
	}
	starts := ix.LookupLocked(name, limit)
	if len(starts) == 0 {
		return nil
	}
	visited := map[string]struct{}{}
	var out []Hit
	frontier := make([]string, 0, len(starts))
	for _, sym := range starts {
		frontier = append(frontier, sym.ID)
		visited[sym.ID] = struct{}{}
	}
	for d := 1; d <= depth && len(out) < limit; d++ {
		var next []string
		for _, id := range frontier {
			for _, other := range adjacency[id] {
				if _, ok := visited[other]; ok {
					continue
				}
				visited[other] = struct{}{}
				sym := ix.symbols[other]
				if sym == nil {
					continue
				}
				out = append(out, Hit{Symbol: sym, Via: id, Distance: d})
				if len(out) >= limit {
					return out
				}
				next = append(next, other)
			}
		}
		frontier = next
	}
	return out
}

// LookupLocked is the lock-free lookup used inside a walk that already holds the read lock.
func (ix *Index) LookupLocked(name string, limit int) []*Symbol {
	key := strings.ToLower(strings.TrimSpace(name))
	var out []*Symbol
	for _, id := range ix.byName[key] {
		out = append(out, ix.symbols[id])
		if len(out) >= limit {
			return out
		}
	}
	for _, sym := range ix.symbols {
		if len(out) >= limit {
			break
		}
		if strings.HasPrefix(strings.ToLower(sym.Name), key) && sym.Name != key {
			out = append(out, sym)
		}
	}
	return out
}

// ModuleDeps lists the packages one package imports.
func (ix *Index) ModuleDeps(pkg string, limit int) []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if limit <= 0 {
		limit = 30
	}
	set := map[string]struct{}{}
	for _, rel := range ix.byPackage[pkg] {
		for _, imp := range ix.files[rel].Imports {
			set[imp] = struct{}{}
		}
	}
	return sortedKeys(set, limit)
}

// ImportedBy lists the files that import a package or path.
func (ix *Index) ImportedBy(pkg string, limit int) []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if limit <= 0 {
		limit = 30
	}
	set := map[string]struct{}{}
	for _, rel := range ix.importedBy[pkg] {
		set[rel] = struct{}{}
	}
	// A package is usually named by the last segment of its import path, so a query for "store"
	// should match an import written as "demo/internal/store".
	want := lastSegment(pkg)
	for imp, rels := range ix.importedBy {
		if lastSegment(imp) != want {
			continue
		}
		for _, rel := range rels {
			set[rel] = struct{}{}
		}
	}
	return sortedKeys(set, limit)
}

// PackageCycles finds import cycles between packages. A cycle is the concrete reason a change in
// one module can propagate back into the module that started it, so it is reported rather than
// assumed away.
func (ix *Index) PackageCycles(limit int) [][]string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.packageCyclesLocked(limit)
}

// packageCyclesLocked is the lock-free form: build() computes the cycle count while the refresh
// already holds the write lock, and a re-entrant RLock on the same mutex would deadlock.
func (ix *Index) packageCyclesLocked(limit int) [][]string {
	if limit <= 0 {
		limit = 20
	}
	deps := map[string]map[string]struct{}{}
	for pkg, files := range ix.byPackage {
		for _, rel := range files {
			for _, imp := range ix.files[rel].Imports {
				target := strings.ToLower(lastSegment(imp))
				if _, ok := ix.byPackage[target]; !ok {
					continue
				}
				if deps[pkg] == nil {
					deps[pkg] = map[string]struct{}{}
				}
				deps[pkg][target] = struct{}{}
			}
		}
	}

	var cycles [][]string
	state := map[string]int{} // 0 = unvisited, 1 = on stack, 2 = done
	var path []string
	var visit func(pkg string)
	visit = func(pkg string) {
		if len(cycles) >= limit {
			return
		}
		state[pkg] = 1
		path = append(path, pkg)
		for next := range deps[pkg] {
			if state[next] == 1 {
				start := 0
				for i, p := range path {
					if p == next {
						start = i
						break
					}
				}
				cycle := append([]string{}, path[start:]...)
				cycle = append(cycle, next)
				cycles = append(cycles, cycle)
				if len(cycles) >= limit {
					break
				}
			} else if state[next] == 0 {
				visit(next)
			}
		}
		path = path[:len(path)-1]
		state[pkg] = 2
	}
	for pkg := range deps {
		if state[pkg] == 0 {
			visit(pkg)
		}
	}
	return cycles
}

// HotSymbols returns definitions with the most inbound references: the places a change propagates
// furthest, which is where review effort should be concentrated.
func (ix *Index) HotSymbols(limit int) []Hit {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if limit <= 0 {
		limit = 15
	}
	var out []Hit
	for id, list := range ix.callers {
		sym := ix.symbols[id]
		if sym == nil {
			continue
		}
		out = append(out, Hit{Symbol: sym, Distance: len(list)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Distance != out[j].Distance {
			return out[i].Distance > out[j].Distance
		}
		return out[i].Symbol.ID < out[j].Symbol.ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Render formats one query for the model, bounded by limit. The output states its own limits so a
// truncated answer is not read as a complete picture.
func (ix *Index) Render(mode, query string, depth, limit int) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if limit <= 0 {
		limit = 40
	}
	switch mode {
	case "definition", "lookup":
		hits := ix.Lookup(query, limit)
		if len(hits) == 0 {
			return fmt.Sprintf("graph: no definition named %q (heuristic parser; try code_search)", query)
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("graph definitions for %q (%d)\n", query, len(hits)))
		for _, sym := range hits {
			b.WriteString(fmt.Sprintf("  %s %s · %s:%d\n", sym.Kind, sym.Name, sym.File, sym.Line))
		}
		return strings.TrimSpace(b.String())
	case "callers", "impact":
		hits := ix.Callers(query, depth, limit)
		if len(hits) == 0 {
			return fmt.Sprintf("graph: no inbound references to %q", query)
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("graph inbound references to %q (depth %d, %d hits)\n", query, depth, len(hits)))
		for _, h := range hits {
			b.WriteString(fmt.Sprintf("  d%d %s %s · %s:%d via %s\n", h.Distance, h.Symbol.Kind, h.Symbol.Name, h.Symbol.File, h.Symbol.Line, h.Via))
		}
		return strings.TrimSpace(b.String())
	case "callees":
		hits := ix.Callees(query, depth, limit)
		if len(hits) == 0 {
			return fmt.Sprintf("graph: no outbound references from %q", query)
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("graph outbound references from %q (depth %d, %d hits)\n", query, depth, len(hits)))
		for _, h := range hits {
			b.WriteString(fmt.Sprintf("  d%d %s %s · %s:%d\n", h.Distance, h.Symbol.Kind, h.Symbol.Name, h.Symbol.File, h.Symbol.Line))
		}
		return strings.TrimSpace(b.String())
	case "module":
		deps := ix.ModuleDeps(query, limit)
		importers := ix.ImportedBy(query, limit)
		var b strings.Builder
		b.WriteString("graph module " + query + "\n")
		b.WriteString("  imports: " + orNone(deps) + "\n")
		b.WriteString("  imported by: " + orNone(importers) + "\n")
		return strings.TrimSpace(b.String())
	case "cycles":
		cycles := ix.PackageCycles(limit)
		if len(cycles) == 0 {
			return "graph: no package-level import cycles found"
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("graph import cycles (%d)\n", len(cycles)))
		for _, c := range cycles {
			b.WriteString("  " + strings.Join(c, " → ") + "\n")
		}
		return strings.TrimSpace(b.String())
	case "stats":
		shape := ix.Shape()
		parsed, reused, skipped, truncated, builtAt := ix.Stats()
		var b strings.Builder
		b.WriteString("graph index\n")
		b.WriteString(fmt.Sprintf("  files %d · symbols %d · edges %d · modules %d · max fan-in %d · cycles %d\n",
			shape.Files, shape.Symbols, shape.Edges, shape.Modules, shape.MaxFanIn, shape.Cycles))
		b.WriteString(fmt.Sprintf("  parsed %d · reused %d · skipped %d · truncated %v · built %s\n",
			parsed, reused, skipped, truncated, builtAt.Format("15:04:05")))
		hot := ix.HotSymbols(10)
		if len(hot) > 0 {
			b.WriteString("  hot symbols (inbound references):\n")
			for _, h := range hot {
				b.WriteString(fmt.Sprintf("    %s %s · %s:%d (%d)\n", h.Symbol.Kind, h.Symbol.Name, h.Symbol.File, h.Symbol.Line, h.Distance))
			}
		}
		return strings.TrimSpace(b.String())
	default:
		return "graph: unknown mode " + mode + " (use definition, callers, impact, callees, module, cycles, stats)"
	}
}

// PromptBlock renders the graph summary injected into the system prompt, bounded by maxChars.
func (ix *Index) PromptBlock(maxChars int) string {
	shape := ix.Shape()
	if shape.Files == 0 {
		return ""
	}
	if maxChars <= 0 {
		maxChars = 2000
	}
	var b strings.Builder
	b.WriteString("## 代码图（启发式解析，非类型验证）\n\n")
	b.WriteString(fmt.Sprintf("- 文件 %d · 符号 %d · 边 %d · 模块 %d · 最大入度 %d · 包级循环 %d\n",
		shape.Files, shape.Symbols, shape.Edges, shape.Modules, shape.MaxFanIn, shape.Cycles))
	hot := ix.HotSymbols(8)
	if len(hot) > 0 {
		b.WriteString("- 高入度符号（改动影响最大）：\n")
		for _, h := range hot {
			line := fmt.Sprintf("  - %s %s（%s:%d，入度 %d）\n", h.Symbol.Kind, h.Symbol.Name, h.Symbol.File, h.Symbol.Line, h.Distance)
			if b.Len()+len(line) > maxChars {
				b.WriteString("  …（超出注入上限，已省略）\n")
				break
			}
			b.WriteString(line)
		}
	}
	if cycles := ix.PackageCycles(5); len(cycles) > 0 {
		b.WriteString("- 包级循环依赖：\n")
		for _, c := range cycles {
			line := "  - " + strings.Join(c, " → ") + "\n"
			if b.Len()+len(line) > maxChars {
				break
			}
			b.WriteString(line)
		}
	}
	return strings.TrimSpace(b.String())
}

func sortedKeys(set map[string]struct{}, limit int) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func setOf(list []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, v := range list {
		out[v] = struct{}{}
	}
	return out
}

func orNone(list []string) string {
	if len(list) == 0 {
		return "（无）"
	}
	return strings.Join(list, ", ")
}
