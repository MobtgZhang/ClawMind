package codectx

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// IgnoredDirs are skipped when profiling a workspace: build output, dependency trees, VCS metadata.
var IgnoredDirs = map[string]struct{}{
	".git": {}, ".svn": {}, "node_modules": {}, ".venv": {}, "venv": {}, "env": {},
	"dist": {}, "build": {}, "out": {}, "target": {}, "vendor": {}, ".next": {},
	"__pycache__": {}, ".pytest_cache": {}, ".mypy_cache": {}, ".ruff_cache": {},
	"coverage": {}, ".gradle": {}, ".clawmind": {},
}

// LanguageStat is one detected language with its file count and approximate size.
type LanguageStat struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Bytes int    `json:"bytes"`
}

// Commands are the inferred per-project commands (test / build / lint / format).
type Commands struct {
	Test   string `json:"test,omitempty"`
	Build  string `json:"build,omitempty"`
	Lint   string `json:"lint,omitempty"`
	Format string `json:"format,omitempty"`
}

// Profile is a bounded snapshot of a code workspace: languages, manifests, commands, tree.
type Profile struct {
	Root      string         `json:"root"`
	IsGit     bool           `json:"isGit"`
	Branch    string         `json:"branch,omitempty"`
	Languages []LanguageStat `json:"languages"`
	Manifests []string       `json:"manifests"`
	Commands  Commands       `json:"commands"`
	Tree      []string       `json:"tree"`
	Scanned   int            `json:"scanned"`
	Truncated bool           `json:"truncated"`
	// TestFiles counts files whose name looks like a test: the verification gate is only worth
	// running when something can actually be executed.
	TestFiles int `json:"testFiles"`
	// HasCI marks a CI config, which is a contract the change has to keep.
	HasCI bool `json:"hasCI"`
}

// MaxTreeEntries bounds the file listing injected into a prompt.
const MaxTreeEntries = 240

// Detect profiles a workspace root. It never shells out; git info is read from .git/HEAD.
func Detect(root string) *Profile {
	p := &Profile{Root: filepath.Clean(root)}
	if p.Root == "" || p.Root == "." {
		return p
	}
	p.Branch = gitBranch(p.Root)
	p.IsGit = p.Branch != "" || dirExists(filepath.Join(p.Root, ".git"))

	counts := map[string]*LanguageStat{}
	var scanned int
	var truncated bool
	maxEntries := 4000

	_ = filepath.Walk(p.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		rel, rerr := filepath.Rel(p.Root, path)
		if rerr != nil || rel == "." {
			return nil
		}
		if info.IsDir() {
			if IsIgnored(info.Name()) {
				return filepath.SkipDir
			}
			if depth(rel) > 4 {
				return filepath.SkipDir
			}
			if len(p.Tree) < MaxTreeEntries {
				p.Tree = append(p.Tree, rel+"/")
			} else {
				truncated = true
			}
			return nil
		}
		if scanned >= maxEntries {
			truncated = true
			return filepath.SkipDir
		}
		scanned++
		if len(p.Tree) < MaxTreeEntries {
			p.Tree = append(p.Tree, rel)
		} else {
			truncated = true
		}
		name := filepath.Base(rel)
		if isManifest(name) {
			p.Manifests = append(p.Manifests, rel)
		}
		if isTestName(name) {
			p.TestFiles++
		}
		if strings.HasPrefix(rel, ".github/workflows/") || strings.HasPrefix(rel, ".gitlab-ci.yml") {
			p.HasCI = true
		}
		lang := languageOf(strings.ToLower(filepath.Ext(rel)))
		if lang == "" {
			return nil
		}
		st := counts[lang]
		if st == nil {
			st = &LanguageStat{Name: lang}
			counts[lang] = st
		}
		st.Files++
		st.Bytes += int(info.Size())
		return nil
	})
	p.Scanned = scanned
	p.Truncated = truncated

	for _, st := range counts {
		p.Languages = append(p.Languages, *st)
	}
	sort.Slice(p.Languages, func(i, j int) bool {
		if p.Languages[i].Files != p.Languages[j].Files {
			return p.Languages[i].Files > p.Languages[j].Files
		}
		return p.Languages[i].Name < p.Languages[j].Name
	})
	if len(p.Languages) > 8 {
		p.Languages = p.Languages[:8]
	}
	if len(p.Manifests) > 20 {
		p.Manifests = p.Manifests[:20]
	}
	p.Commands = inferCommands(p.Root, p.Manifests)
	sort.Strings(p.Tree)
	return p
}

// PromptBlock renders the profile for system-prompt injection, bounded by maxChars.
func (p *Profile) PromptBlock(maxChars int) string {
	if p == nil || p.Root == "" {
		return ""
	}
	if maxChars <= 0 {
		maxChars = 4000
	}
	var b strings.Builder
	b.WriteString("## 代码仓库画像\n\n")
	b.WriteString("- 工作区根：`" + p.Root + "`")
	if p.IsGit {
		branch := p.Branch
		if branch == "" {
			branch = "（分支未知）"
		}
		b.WriteString(" · Git 仓库 · 当前分支：`" + branch + "`")
	}
	b.WriteString("\n")
	if len(p.Languages) > 0 {
		parts := make([]string, 0, len(p.Languages))
		for _, l := range p.Languages {
			parts = append(parts, l.Name+" "+itoa(l.Files)+" 文件")
		}
		b.WriteString("- 语言分布：" + strings.Join(parts, " · ") + "\n")
	}
	if len(p.Manifests) > 0 {
		b.WriteString("- 清单文件：" + strings.Join(p.Manifests, ", ") + "\n")
	}
	c := p.Commands
	if c.Test != "" || c.Build != "" || c.Lint != "" || c.Format != "" {
		b.WriteString("- 推断命令（优先使用，不要臆造）：\n")
		if c.Test != "" {
			b.WriteString("  - 测试：`" + c.Test + "`\n")
		}
		if c.Build != "" {
			b.WriteString("  - 构建：`" + c.Build + "`\n")
		}
		if c.Lint != "" {
			b.WriteString("  - 检查：`" + c.Lint + "`\n")
		}
		if c.Format != "" {
			b.WriteString("  - 格式化：`" + c.Format + "`\n")
		}
	}
	if len(p.Tree) > 0 {
		b.WriteString("- 结构（最多 " + itoa(MaxTreeEntries) + " 项")
		if p.Truncated {
			b.WriteString("，已截断")
		}
		b.WriteString("）：\n")
		used := b.Len()
		for _, e := range p.Tree {
			if used+len(e)+2 > maxChars {
				b.WriteString("  …（超出注入上限，已省略）\n")
				break
			}
			b.WriteString("  " + e + "\n")
			used += len(e) + 2
		}
	}
	return strings.TrimSpace(b.String())
}

func gitBranch(root string) string {
	head := filepath.Join(root, ".git", "HEAD")
	b, err := os.ReadFile(head)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "ref:") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "ref:"))
		s = strings.TrimPrefix(s, "refs/heads/")
		return s
	}
	if len(s) >= 7 {
		return s[:8]
	}
	return ""
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// IsIgnored reports whether a directory name is skipped when profiling or searching a workspace.
func IsIgnored(name string) bool {
	_, ok := IgnoredDirs[name]
	return ok
}

func depth(rel string) int {
	n := 0
	for _, r := range rel {
		if r == '/' || r == '\\' {
			n++
		}
	}
	return n
}

func isManifest(name string) bool {
	switch name {
	case "go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock.yaml",
		"pyproject.toml", "requirements.txt", "setup.py", "Cargo.toml", "Makefile",
		"pom.xml", "build.gradle", "build.gradle.kts", "Dockerfile", ".env.example",
		"tsconfig.json", "pytest.ini", "setup.cfg", ".ruff.toml", "ruff.toml":
		return true
	}
	return false
}

var extLang = map[string]string{
	".go": "Go", ".py": "Python", ".ts": "TypeScript", ".tsx": "TypeScript",
	".js": "JavaScript", ".jsx": "JavaScript", ".mjs": "JavaScript", ".cjs": "JavaScript",
	".rs": "Rust", ".java": "Java", ".kt": "Kotlin", ".c": "C", ".h": "C",
	".cpp": "C++", ".hpp": "C++", ".cs": "C#", ".rb": "Ruby", ".php": "PHP",
	".sh": "Shell", ".vue": "Vue", ".svelte": "Svelte", ".sql": "SQL",
	".yaml": "YAML", ".yml": "YAML", ".toml": "TOML", ".md": "Markdown",
}

func languageOf(ext string) string {
	return extLang[ext]
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}

// isTestName reports whether a filename looks like a test file. The classifier uses this to decide
// whether a verification gate can actually be executed: a gate with nothing to run is theatre.
func isTestName(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, "_test.go") || strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".test.js") || strings.HasSuffix(lower, ".spec.ts") ||
		strings.HasSuffix(lower, ".spec.js") || strings.HasSuffix(lower, ".test.tsx") {
		return true
	}
	if strings.HasPrefix(lower, "test_") || strings.HasPrefix(lower, "test.") {
		return true
	}
	return strings.Contains(lower, "/test/") || strings.Contains(lower, "tests/")
}

// LanguageOf maps a lowercase extension to a language name. The graph index needs the same mapping
// as the profile, so it exists once rather than as a second table that can drift.
func LanguageOf(ext string) string {
	return languageOf(ext)
}
