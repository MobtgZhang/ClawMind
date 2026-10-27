package codectx

import "strings"

// Class is the project-complexity tier the loop is shaped by. One fixed budget cannot serve a
// three-file script and a three-thousand-file monorepo: the same round cap that is generous for
// the first is a dead end for the second, and a search cap that is safe for the second starves
// the first into guessing.
type Class string

const (
	ClassSimple  Class = "simple"
	ClassComplex Class = "complex"
)

// GraphShape is the code-graph view the classifier needs. It is a plain struct so the graph
// package can feed the classifier without the classifier importing the graph back.
type GraphShape struct {
	Files    int `json:"files"`
	Symbols  int `json:"symbols"`
	Edges    int `json:"edges"`
	Modules  int `json:"modules"`
	MaxFanIn int `json:"maxFanIn"`
	Cycles   int `json:"cycles"`
}

// Budget is the loop shape derived from a class: node count, re-entry, fan-out and search caps.
// These are the numbers a single global setting cannot express, because they depend on the size
// of the surface being analysed rather than on operator preference.
type Budget struct {
	Class         Class    `json:"class"`
	Score         int      `json:"score"`
	Reasons       []string `json:"reasons"`
	Loop          string   `json:"loop"` // linear | graph
	MaxNodes      int      `json:"maxNodes"`
	MaxRounds     int      `json:"maxRounds"`
	Reentry       int      `json:"reentry"`
	Fanout        int      `json:"fanout"`
	SearchFiles   int      `json:"searchFiles"`
	SearchResults int      `json:"searchResults"`
	ContextTokens int      `json:"contextTokens"`
	PromptChars   int      `json:"promptChars"`
	Graph         bool     `json:"graph"`
	Verify        bool     `json:"verify"`
	Review        bool     `json:"review"`
}

// complexTaskWords are requests that cannot be answered inside one file: they name a change that
// crosses a boundary, so the loop has to look at dependants before it edits.
var complexTaskWords = []string{
	"重构", "架构", "跨模块", "依赖", "迁移", "性能", "并发", "接口", "契约", "批量", "全仓库", "调用点", "测试覆盖",
	"refactor", "architecture", "migrate", "across", "dependency", "concurrency", "contract",
	"schema", "pipeline", "blast radius", "call sites", "breaking change",
}

// simpleTaskWords are requests whose whole surface is one span of text: no dependant can break,
// so a graph pass would be pure cost.
var simpleTaskWords = []string{
	"改一行", "拼写", "错别字", "注释", "文案", "翻译", "格式", "重命名这个变量", "加个日志",
	"typo", "comment", "rename this variable", "one line", "formatting", "copy text",
}

// Classify scores the workspace plus the request and returns the loop shape for this turn.
// The workspace alone is not enough: a 2000-file repo asked for a typo fix is a simple task, and
// a 40-file repo asked for a module boundary change is a complex one.
func Classify(p *Profile, shape GraphShape, task string, baseMaxRounds, baseContextTokens int) Budget {
	b := Budget{Class: ClassSimple, Loop: "linear"}
	var score int
	var reasons []string

	files := 0
	if p != nil {
		files = p.Scanned
	}
	if files >= 400 {
		score += 3
		reasons = append(reasons, "文件数 "+itoa(files)+"（大规模仓库）")
	} else if files >= 120 {
		score += 2
		reasons = append(reasons, "文件数 "+itoa(files))
	} else if files >= 40 {
		score += 1
		reasons = append(reasons, "文件数 "+itoa(files))
	}

	if shape.Symbols >= 600 {
		score += 3
		reasons = append(reasons, "符号数 "+itoa(shape.Symbols))
	} else if shape.Symbols >= 200 {
		score += 2
		reasons = append(reasons, "符号数 "+itoa(shape.Symbols))
	}
	if shape.Modules >= 10 {
		score += 2
		reasons = append(reasons, "模块数 "+itoa(shape.Modules))
	} else if shape.Modules >= 4 {
		score += 1
		reasons = append(reasons, "模块数 "+itoa(shape.Modules))
	}
	if shape.MaxFanIn >= 12 {
		score += 2
		reasons = append(reasons, "最大入度 "+itoa(shape.MaxFanIn)+"（共享符号）")
	} else if shape.MaxFanIn >= 5 {
		score += 1
		reasons = append(reasons, "最大入度 "+itoa(shape.MaxFanIn))
	}
	if shape.Cycles > 0 {
		score += 2
		reasons = append(reasons, "存在包级循环依赖")
	}
	if p != nil {
		if len(p.Languages) >= 3 {
			score += 1
			reasons = append(reasons, "多语言混合")
		}
		if len(p.Manifests) >= 3 {
			score += 1
			reasons = append(reasons, "多个清单文件（多包/多工作区）")
		}
		if p.Truncated {
			score += 1
			reasons = append(reasons, "画像被截断（仓库超出单次快照上限）")
		}
		if p.HasCI {
			score += 1
			reasons = append(reasons, "存在 CI 契约")
		}
	}

	lower := strings.ToLower(task)
	taskScore := 0
	for _, w := range complexTaskWords {
		if strings.Contains(lower, strings.ToLower(w)) {
			taskScore += 2
			reasons = append(reasons, "请求跨边界（命中："+w+"）")
			break
		}
	}
	taskSimple := false
	for _, w := range simpleTaskWords {
		if strings.Contains(lower, strings.ToLower(w)) {
			taskSimple = true
			reasons = append(reasons, "请求限于单处文本（命中："+w+"）")
			break
		}
	}
	score += taskScore

	if p != nil && p.TestFiles > 0 {
		reasons = append(reasons, "存在测试文件 "+itoa(p.TestFiles)+"：验证门可执行")
	}

	if score >= 5 && !taskSimple {
		b.Class = ClassComplex
	}
	b.Score = score
	if len(reasons) == 0 {
		reasons = append(reasons, "工作区与请求都小：单遍即可")
	}
	b.Reasons = reasons
	b.Graph = shape.Symbols > 0
	b.Verify = p != nil && p.Commands.Test != ""
	b.applyShape(baseMaxRounds, baseContextTokens)
	return b
}

// FixedBudget returns the shape for a class the operator forced. It has to produce the same numbers
// the classifier would have produced for that class, otherwise the flag changes the budgets in a way
// the operator cannot predict from the class it set.
func FixedBudget(class Class, baseMaxRounds, baseContextTokens int) Budget {
	b := Budget{Class: class, Reasons: []string{"分类由 CLAWMIND_PROJECT_CLASS 强制"}}
	b.applyShape(baseMaxRounds, baseContextTokens)
	return b
}

// applyShape fills the numbers for the class already chosen. Keeping them in one place is the point:
// the classifier and the override must not drift into two definitions of the same class.
func (b *Budget) applyShape(baseMaxRounds, baseContextTokens int) {

	if b.Class == ClassComplex {
		b.Loop = "graph"
		b.MaxNodes = 12
		b.MaxRounds = boundedInt(baseMaxRounds, 16, 32)
		b.Reentry = 3
		b.Fanout = 4
		b.SearchFiles = 8000
		b.SearchResults = 120
		b.ContextTokens = boundedInt(baseContextTokens, 24000, 60000)
		b.PromptChars = 6000
		b.Graph = true
		b.Verify = true
		b.Review = true
	} else {
		b.MaxNodes = 5
		b.MaxRounds = boundedInt(baseMaxRounds, 8, 16)
		b.Reentry = 1
		b.Fanout = 1
		b.SearchFiles = 1500
		b.SearchResults = 40
		b.ContextTokens = boundedInt(baseContextTokens, 8000, 24000)
		b.PromptChars = 2500
		b.Review = false
	}
}

func clampInt(v, min, max int) int {
	if v <= 0 {
		return min
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// boundedInt keeps an operator setting when it is usable and replaces it only when it is missing or
// beyond the class ceiling. A classifier that silently overrides a configured value is not reporting
// a decision, it is making one, so the floor applies only to the unset case.
func boundedInt(v, def, max int) int {
	if v <= 0 {
		return def
	}
	if v > max {
		return max
	}
	return v
}
