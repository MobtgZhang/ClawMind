package harness

import "strings"

// PrioritySkills are the five software-engineering skills the roadmap prioritises: they change
// how the agent works, not what it produces. Each one is on-demand, so its cost is paid only
// when its trigger fires.
var PrioritySkills = []Skill{
	{Name: "systematic-debugging", Description: "Bug 或测试失败需要定位根因时，系统化复现、缩小范围、验证假设", Path: "skills/systematic-debugging/SKILL.md"},
	{Name: "test-driven-development", Description: "要实现新行为且项目有测试框架时，先写失败测试再最小实现", Path: "skills/test-driven-development/SKILL.md"},
	{Name: "verification-before-completion", Description: "准备向用户宣布完成时，先跑测试、lint、build 并读完整输出", Path: "skills/verification-before-completion/SKILL.md"},
	{Name: "code-review", Description: "需要审查一段改动时，检查正确性、安全性、可维护性", Path: "skills/code-review/SKILL.md"},
	{Name: "architecture-design", Description: "涉及模块边界或技术选型时，先画接口与数据模型再决定改动", Path: "skills/architecture-design/SKILL.md"},
}

// WorkflowChain is the engineering order stated in always-on text: a completion claim is only
// accepted with evidence from the verification stage.
var WorkflowChain = []string{
	"Requirement", "Architecture", "Plan", "TDD", "Review", "Verification", "Release",
}

// SeedTree returns the seeded software-engineering surface: the workflow chain in always-on
// text, the five priority skills, and the deterministic helper they reference.
func SeedTree() Tree {
	spec := DefaultSpec()
	t := New()
	t[AgentsFile] = agentsSeed(spec)
	t[GuardrailsFile] = guardrailsSeed
	t[MemoryFile] = memorySeed
	t[GuidanceFile] = guidanceSeed
	t[HooksFile] = hooksSeed
	for _, s := range PrioritySkills {
		t[s.Path] = skillSeed(s.Name, s.Description)
	}
	t[ScriptDir+"/preflight.py"] = preflightScript
	return t
}

func agentsSeed(spec SurfaceSpec) string {
	var b strings.Builder
	b.WriteString("# 角色与流程\n\n")
	b.WriteString("按 " + strings.Join(WorkflowChain, " → ") + " 的顺序工作；每一步留下可核对的证据，写完代码不等于成功。\n\n")
	for _, step := range workflowRules {
		b.WriteString("- " + step + "\n")
	}
	b.WriteString("\n")
	for _, s := range PrioritySkills {
		b.WriteString(TriggerLine(spec, s.Name, firstClause(s.Description)) + "\n")
	}
	return b.String()
}

var workflowRules = []string{
	"需求：先写验收标准与边界条件，无法验证的判据先补齐。",
	"架构：接口、数据模型、模块边界；改公共 API 前先搜索全部调用点。",
	"计划：拆成可执行步骤，标注风险与回滚方式。",
	"实现：先写失败测试，再最小实现；一次成功的写调用不等于改动可用。",
	"评审：正确性、安全性、可维护性，不放过错误处理与边界。",
	"验证：跑测试、lint、build，读完整输出后才允许报告完成。",
	"交付：形状属于用户，不重命名、不重排、不加未要求的注释。",
}

const guardrailsSeed = `# 护栏

- 被安全策略拒绝的命令永不执行；高风险命令需要用户确认。
- 不读取存放密钥的文件，不在回复中复述密钥。
- 不在工作区之外写文件；路径先规范化。
- 不臆造结果：没有运行就没有证据，没有证据就不能宣布完成。
- 不为了让测试通过而修改测试断言。
- 提案校验不通过时丢弃整个提案，不做部分应用。
`

const memorySeed = `# 记忆

分层：L0 会话 → L1 原子事实 → L2 场景 → L3 长期画像。写入时只保留可泛化的流程，
不保留本次任务的具体事实；检索按 BM25 + 向量 + RRF 融合，并受条目与字符预算约束。
记忆是过去运行的数据，与当前任务冲突时以当前任务为准。
`

const guidanceSeed = `# 工具指导

- file_edit 做精确替换，old 文本要带足够上下文以保证唯一。
- code_search 先搜索再读取，不凭记忆假设文件内容。
- run_tests 运行推断出的测试命令；失败输出要完整阅读，不盲目重试同一命令。
- load_skill 只在触发条件出现时调用，未被调用的技能零成本。
- delegate 用于输入大、回答小的 bounded 调查，结果只回传文本。
- preflight 脚本在报告完成前收集测试与 lint 证据。
`

const hooksSeed = `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "shell_exec",
        "hooks": [
          { "type": "command", "command": "printf '%s\n' \"$CLAWMIND_HOOK_EVENT\"" }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          { "type": "command", "command": "python3 scripts/preflight.py" }
        ]
      }
    ]
  }
}
`

func skillSeed(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n" + skillBody[name] + "\n"
}

var skillBody = map[string]string{
	"systematic-debugging": `1. 复现：先拿到一次可重复的失败，记录输入与命令。
2. 缩小：用二分或最小用例把范围压到一个函数或一条边界。
3. 假设：写下「因为 X 所以 Y」，用一条命令证伪它，而不是继续读代码猜。
4. 根因：修根因，不修症状；临时兜底要标注为兜底。
5. 回归：为这个根因留下一个失败过的测试。`,
	"test-driven-development": `1. 写一条会失败的测试，断言的是验收标准，不是实现细节。
2. 运行它，确认失败原因与预期一致。
3. 写最小实现让它通过，不做顺带的重构。
4. 再跑受影响包的测试，确认没有连带破坏。
5. 只有测试通过后才允许整理结构。`,
	"verification-before-completion": `1. 运行项目推断出的测试命令，读完整输出。
2. 运行 lint 与 build；跳过任何一步都要说明原因。
3. 对照验收标准逐条核对，不核对不能宣布完成。
4. 用 scripts/preflight.py 收集证据，输出与结论一起交付。
5. 任何失败先修复再报告，不把「应该没问题」当作结论。`,
	"code-review": `1. 正确性：边界、空值、错误处理、并发与幂等。
2. 安全性：路径越界、密钥泄露、注入与被拒绝的命令。
3. 可维护性：命名、重复、契约同步、测试是否覆盖改动。
4. 范围：是否做了请求之外的改动，形状是否被改变。
5. 结论分条给出，每条可执行，不写「建议优化」这类空话。`,
	"architecture-design": `1. 先写接口与数据模型，再决定模块边界。
2. 列出调用点与契约，判断改动会波及哪些包。
3. 选型给出两个候选方案与取舍理由，并指出不可逆的决定。
4. 优先可替换的边界：seam 上的实现能从配置替换，不硬编码。
5. 把结论写成 ADR 要点：背景、决定、后果、替代方案。`,
}

const preflightScript = `#!/usr/bin/env python3
"""Collect the evidence a completion claim needs: tests, lint, build, in that order."""
import os
import subprocess
import sys


def commands():
    inferred = os.environ.get("CLAWMIND_TEST_COMMAND", "").strip()
    if inferred:
        return [inferred]
    if os.path.exists("go.mod"):
        return ["go test ./...", "go vet ./..."]
    if os.path.exists("package.json"):
        return ["npm test", "npm run lint"]
    if os.path.isdir("tests") or os.path.exists("pytest.ini"):
        return ["pytest -q"]
    return []


def main() -> int:
    cmds = commands()
    if not cmds:
        print("no verification command inferred; state that no evidence was collected")
        return 1
    failed = False
    for cmd in cmds:
        result = subprocess.run(cmd, shell=True, capture_output=True, text=True)
        print("$ " + cmd + " -> exit " + str(result.returncode))
        out = (result.stdout + result.stderr).strip()
        if out:
            print(out)
        if result.returncode != 0:
            failed = True
    if failed:
        print("verification failed: completion cannot be reported")
        return 1
    print("verification passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
`
