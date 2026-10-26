package evolution

import (
	"fmt"

	"github.com/mobtgzhang/clawmind/backend/internal/harness"
)

// Output format shared by every authoring stage: the proposal is the file tree itself.
// fence is a triple backtick: Go raw string literals cannot contain backticks, so prompt text
// that shows fenced blocks builds them by concatenation.
const fence = "```"

const outputFormat = "## 输出格式\n\n" +
	"先输出改动，再输出理由。改动写成 fenced 块，块的第一行是 harness 内的相对路径：\n\n" +
	"<proposal>\n" + fence + "path\nskills/<name>/SKILL.md\n---\nname: <name>\n" +
	"description: <一句话描述与触发条件>\n---\n<技能正文>\n" + fence + "\n</proposal>\n\n" +
	"删除一个组件：\n\n<delete>\n<相对路径>\n</delete>\n\n然后：\n\n" +
	"<notes>\n对每一处改动，指出轨迹中促使它的具体时刻。没有依据就不要改动。\n</notes>\n\n" +
	"如果证据不支持任何改动，输出空的 <proposal> 并在 <notes> 中写 KEEP_BASE。"

// ProposePrompt builds one editor mission for a dimension (INV = how the agent works,
// DOM = what the codebase requires).
func ProposePrompt(dim string, k, index int, evidence string) string {
	dimension := harness.InvDimension
	if dim == "DOM" {
		dimension = harness.DomDimension
	}
	who := "INVARIANT"
	if dim == "DOM" {
		who = "DOMAIN"
	}
	which := "You are the only editor on this dimension; a later stage compares and merges."
	if k > 1 {
		which = fmt.Sprintf("你是该维度 %d 个候选中的第 %d 个，独立从同一证据出发，不会看到其他候选。"+
			"不要向平均值靠拢或试图覆盖所有情形——选定你这个维度上最站得住脚的单一版本。后续阶段会比较并合并。", k, index)
	}
	return fmt.Sprintf(`你是代码项目 harness 的 %s 编辑器，只针对这一个任务。
你拥有：当前真实任务、在现行（base）harness 下该任务的完整执行轨迹、现行 harness 文件、以及本项目库中此前被采纳的 harness。

%s

%s

%s

%s

%s

%s`, who, dimension, which, harness.EvidenceBlock(), harness.GeneralizeBlock(), harness.SurfaceBlock(), outputFormat)
}

// ProbePrompt asks what a candidate harness would have changed, given the recorded trajectory.
func ProbePrompt(candidateLabel, evidence string) string {
	return fmt.Sprintf(`你在评估一个候选 harness 对同一个任务的影响。

证据是**已经发生**的执行轨迹（base harness 下的真实运行）。候选尚未真实执行，因此你的结论是**预测**，
必须明确标注为预测，并且只能基于轨迹中已经出现的行为差异。

%s

对每个候选回答：
- 该候选会改变哪一步行为（引用轨迹中的具体时刻）
- 该改变是否可观察、是否可能复现到同项目的其他任务
- 分数 0-100：可观察改善 × 可迁移性 × 常驻成本（常驻文本越多分数越低）

## 输出格式

<probe>
label: %s
score: <0-100>
observed: <引用轨迹时刻的行为差异，或 "no observable difference">
confounded: <true|false，多个组件同时改变且无执行隔离时为 true>
</probe>`, harness.EvidenceBlock(), candidateLabel)
}

// ComposePrompt builds the merge stage: compose the final harness from all candidates.
func ComposePrompt(evidence string) string {
	return fmt.Sprintf(`你在组装最终 harness。以下都是合法选择：

- 跨候选合并——包括此前被拒绝的候选，如果现在的证据支持它
- 原样复现某个更早的状态，如果那本来就是最好的
- 采纳最佳候选
- KEEP_BASE

%s

%s

%s`, harness.GeneralizeBlock(), harness.EvidenceBlock(), harness.SurfaceBlock())
}

// ReflectPrompt builds the experience stage: per-edit verdicts plus one transferable lesson.
func ReflectPrompt(evidence string) string {
	return fmt.Sprintf(`你在阅读一次完整改进记录：base harness、针对它提出的改动、这些改动产生的执行差异、
以及最终采纳的是哪一个。为后续任务会读取的库写两样东西。

采纳的候选不是唯一证据。选择比较的是结果，改动本身可能被混淆：只在被拒绝候选中出现的改动，
也可能正是改变行为的那一个。阅读轨迹，而不是结论。

对每个被改动的组件写一行：组件、改动实际改变了什么、观察到的执行差异、判定。
判定取 helped / hurt / no observed effect / confounded；当一个候选同时改动多个组件且没有执行隔离，
或只有一次执行支持该差异时，优先使用 confounded。混淆的记录仍然有价值；编造的归因会污染整个库。

然后最多写一条可迁移的经验。`+"`cue`"+` 描述这条经验适用的**情形形状**（不是本任务的事实），
`+"`lesson`"+` 用可执行、可检查的方式陈述操作，并点名它涉及的组件。太泛（"要严谨"）无用，
太具体（"第 42 行的变量叫 cfg"）不会再触发——取中间。若记录中没有具体可复用的执行对比，
写 NO_ENTRY。

%s

%s`, harness.EvidenceBlock(), reflectFormat)
}

const reflectFormat = `## 输出格式

<edits>
component: <harness 树中的路径>
candidates: <携带该改动的候选标签>
change: <改动做了什么>
observed: <观察到的差异，以及你在哪里看到的>
verdict: <helped|hurt|no observed effect|confounded>
（对每个改动的组件重复这五行）
</edits>

<experience>
cue: <这条经验适用的情形形状>
lesson: <可迁移的操作，点名组件>
evidence: <经验所依据的 edit 行与执行对比>
confidence: <high|medium|low, 以及原因>
</experience>`

// SelectPrompt builds blind selection over anonymized candidates.
func SelectPrompt(n int) string {
	return fmt.Sprintf(`你要从 %d 个候选中选出应当保留的那个。

候选 id 是任意且打乱的，不说明任何来源。没有"最终"候选值得偏袒，也没有对更复杂候选的奖励。
多个候选可能看起来几乎一样——这时你依据真正有差别的地方决定。未改动的 base 是完全合法且常见的选择。

%s

## 输出格式

先给推理，最后给出选择：

<why>
最多 6 行。点名决定性的判据，以及最接近的落选者与它输在哪里。
</why>

`+fence+`choice
<id>
`+fence, n, harness.EvidenceBlock())
}
