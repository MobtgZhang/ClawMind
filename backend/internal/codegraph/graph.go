package codegraph

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mobtgzhang/clawmind/backend/internal/codectx"
)

// Symbol is one definition found in a file: function, method, type, class or module.
type Symbol struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Package string `json:"package"`
}

// FileNode is one parsed file: package, imports and the ids of the symbols it defines.
type FileNode struct {
	Path     string   `json:"path"`
	Package  string   `json:"package"`
	Language string   `json:"language"`
	Imports  []string `json:"imports"`
	Symbols  []string `json:"symbols"`
	Hash     string   `json:"hash"`
	Bytes    int      `json:"bytes"`
}

// Edge is one relation: symbol→symbol (call) or file→file (import).
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// PendingCall is a reference recorded while parsing and resolved after every file is read: a call
// to pkg.X cannot be resolved before pkg has been parsed.
type PendingCall struct {
	Owner     string
	Name      string
	Qualifier string
	Line      int
}

// Limits bound one index. An unbounded index is not a feature but a memory and latency risk, so
// every cap here exists to keep a large repository analysable rather than exhaustive.
type Limits struct {
	MaxFiles          int
	MaxFileBytes      int
	MaxSymbolsPerFile int
	RefreshSeconds    int
}

func DefaultLimits() Limits {
	return Limits{
		MaxFiles:          6000,
		MaxFileBytes:      256 * 1024,
		MaxSymbolsPerFile: 120,
		RefreshSeconds:    30,
	}
}

// Index is a bounded symbol graph for one workspace root.
type Index struct {
	Root   string
	Limits Limits

	files   map[string]*FileNode
	symbols map[string]*Symbol
	pending map[string][]PendingCall
	raws    map[string][]rawSymbol
	edges   []Edge

	callers    map[string][]string
	callees    map[string][]string
	importedBy map[string][]string
	byName     map[string][]string
	byPackage  map[string][]string

	shape     codectx.GraphShape
	parsed    int
	reused    int
	skipped   int
	truncated bool
	builtAt   time.Time
	// dirty holds files the agent changed since the last build. An index that answers from a
	// pre-edit snapshot reports the graph the code no longer has, which is worse than reporting
	// nothing: the model cannot tell a stale edge from a real one.
	dirty map[string]struct{}

	mu sync.RWMutex
}

// New returns an empty index for a root.
func New(root string, limits Limits) *Index {
	if limits.MaxFiles <= 0 {
		limits = DefaultLimits()
	}
	return &Index{
		Root:       filepath.Clean(root),
		Limits:     limits,
		files:      map[string]*FileNode{},
		symbols:    map[string]*Symbol{},
		pending:    map[string][]PendingCall{},
		raws:       map[string][]rawSymbol{},
		callers:    map[string][]string{},
		callees:    map[string][]string{},
		importedBy: map[string][]string{},
		byName:     map[string][]string{},
		byPackage:  map[string][]string{},
		dirty:      map[string]struct{}{},
	}
}

// MarkDirty records one file the agent changed. Nothing is re-parsed here: the smallest correct
// unit of work is the file itself, and the next query pays it instead of every edit paying a full
// rebuild.
func (ix *Index) MarkDirty(rel string) {
	if ix == nil {
		return
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." || rel == "" {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.dirty == nil {
		ix.dirty = map[string]struct{}{}
	}
	ix.dirty[rel] = struct{}{}
}

// Dirty reports how many files changed since the last build.
func (ix *Index) Dirty() int {
	if ix == nil {
		return 0
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.dirty)
}

// Refresh walks the workspace and re-parses only files whose size or mtime changed. The walk is
// cheap and the parse is not: re-reading every file on every turn is the failure mode incremental
// parsing exists to prevent.
func (ix *Index) Refresh(root string) (parsed, reused, removed int) {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	ix.Root = filepath.Clean(root)
	seen := map[string]struct{}{}
	var walked int

	_ = filepath.WalkDir(ix.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		rel, rerr := filepath.Rel(ix.Root, path)
		if rerr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if codectx.IsIgnored(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if walked >= ix.Limits.MaxFiles {
			ix.truncated = true
			return filepath.SkipDir
		}
		info, ierr := d.Info()
		if ierr != nil || info == nil {
			return nil
		}
		if info.Size() > int64(ix.Limits.MaxFileBytes) {
			ix.skipped++
			return nil
		}
		language := codectx.LanguageOf(strings.ToLower(filepath.Ext(rel)))
		if language == "" {
			return nil
		}
		walked++
		seen[rel] = struct{}{}

		hash := strconv.FormatInt(info.Size(), 10) + "@" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
		// A file the agent edited is re-parsed even when the hash matches: a same-size edit with a
		// preserved mtime is possible, and a graph built from the pre-edit bytes is not the graph.
		if old, ok := ix.files[rel]; ok && old.Hash == hash {
			if _, marked := ix.dirty[rel]; !marked {
				reused++
				return nil
			}
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		node, syms, calls := parseFile(rel, language, string(b), ix.Limits.MaxSymbolsPerFile)
		node.Hash = hash
		node.Bytes = len(b)
		ix.files[rel] = node
		ix.raws[rel] = syms
		ix.pending[rel] = calls
		parsed++
		return nil
	})

	for rel := range ix.files {
		if _, ok := seen[rel]; !ok {
			delete(ix.files, rel)
			delete(ix.pending, rel)
			delete(ix.raws, rel)
			removed++
		}
	}
	for rel := range ix.dirty {
		if _, ok := seen[rel]; !ok {
			delete(ix.dirty, rel)
		}
	}

	ix.build()
	ix.builtAt = time.Now()
	ix.parsed = parsed
	ix.reused = reused
	ix.dirty = map[string]struct{}{}
	return parsed, reused, removed
}

// build rebuilds the lookup tables and adjacency from the parsed files: cross-file references can
// only be resolved once every package is known.
func (ix *Index) build() {
	ix.edges = nil
	ix.callers = map[string][]string{}
	ix.callees = map[string][]string{}
	ix.importedBy = map[string][]string{}
	ix.byName = map[string][]string{}
	ix.byPackage = map[string][]string{}
	ix.symbols = map[string]*Symbol{}

	for rel, syms := range ix.raws {
		for _, s := range syms {
			ix.symbols[s.ID] = &Symbol{
				ID: s.ID, Name: s.Name, Kind: s.Kind, File: rel, Line: s.Line,
				Package: ix.files[rel].Package,
			}
		}
	}

	for rel, node := range ix.files {
		ix.byPackage[node.Package] = append(ix.byPackage[node.Package], rel)
		for _, id := range node.Symbols {
			sym := ix.symbols[id]
			if sym == nil {
				continue
			}
			ix.byName[strings.ToLower(sym.Name)] = append(ix.byName[strings.ToLower(sym.Name)], id)
		}
	}

	// Resolve pending references now that every package is known.
	for rel, node := range ix.files {
		ownerByName := map[string]string{}
		for _, id := range node.Symbols {
			sym := ix.symbols[id]
			if sym == nil {
				continue
			}
			if _, ok := ownerByName[sym.Name]; !ok {
				ownerByName[sym.Name] = id
			}
		}
		for _, call := range ix.pending[rel] {
			target := ix.resolve(node, call, ownerByName)
			if target == "" || target == call.Owner {
				continue
			}
			from := call.Owner
			if from == "" {
				from = rel
			}
			ix.edges = append(ix.edges, Edge{From: from, To: target, Kind: "call"})
			ix.callers[target] = append(ix.callers[target], from)
			ix.callees[from] = append(ix.callees[from], target)
		}
		for _, imp := range node.Imports {
			ix.edges = append(ix.edges, Edge{From: rel, To: imp, Kind: "import"})
			ix.importedBy[imp] = append(ix.importedBy[imp], rel)
		}
	}

	maxFanIn := 0
	for _, list := range ix.callers {
		if len(list) > maxFanIn {
			maxFanIn = len(list)
		}
	}
	ix.shape = codectx.GraphShape{
		Files:    len(ix.files),
		Symbols:  len(ix.symbols),
		Edges:    len(ix.edges),
		Modules:  len(ix.byPackage),
		MaxFanIn: maxFanIn,
		Cycles:   len(ix.packageCyclesLocked(20)),
	}
}

// resolve maps one reference to a symbol id: same file first, then a package the file imports,
// then a unique global match. An ambiguous name is not guessed, because a wrong edge in an impact
// analysis is worse than a missing one.
func (ix *Index) resolve(node *FileNode, call PendingCall, ownerByName map[string]string) string {
	key := strings.ToLower(call.Name)
	if call.Qualifier == "" {
		if id, ok := ownerByName[call.Name]; ok {
			return id
		}
		matches := ix.byName[key]
		if len(matches) == 1 {
			return matches[0]
		}
		return ""
	}
	for _, imp := range node.Imports {
		if lastSegment(imp) != strings.ToLower(call.Qualifier) {
			continue
		}
		for _, rel := range ix.filesForPackage(lastSegment(imp)) {
			for _, id := range ix.files[rel].Symbols {
				if strings.ToLower(ix.symbols[id].Name) == key {
					return id
				}
			}
		}
	}
	return ""
}

// filesForPackage maps a package name to the files that declare it. The import path is not the
// package name, so the mapping follows the Go convention (last path segment) rather than assuming
// the two are the same string.
func (ix *Index) filesForPackage(pkg string) []string {
	return ix.byPackage[strings.ToLower(strings.TrimSpace(pkg))]
}

func lastSegment(p string) string {
	p = strings.Trim(strings.TrimSpace(p), `"`)
	if p == "" {
		return ""
	}
	for _, sep := range []string{"/", "::", "."} {
		if i := strings.LastIndex(p, sep); i >= 0 {
			p = p[i+len(sep):]
		}
	}
	return p
}

// Stats reports what the last refresh cost, so a truncated index is visible instead of silent.
func (ix *Index) Stats() (parsed, reused, skipped int, truncated bool, builtAt time.Time) {
	if ix == nil {
		return 0, 0, 0, false, time.Time{}
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.parsed, ix.reused, ix.skipped, ix.truncated, ix.builtAt
}

// Shape returns the graph shape used by the complexity classifier.
func (ix *Index) Shape() codectx.GraphShape {
	if ix == nil {
		return codectx.GraphShape{}
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.shape
}

// SortedFiles returns parsed file paths in stable order.
func (ix *Index) SortedFiles() []string {
	if ix == nil {
		return nil
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	out := make([]string, 0, len(ix.files))
	for rel := range ix.files {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}
