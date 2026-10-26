# 软件工程 Skills 与 SKILL.md 生态

## 当前实现（ClawMind）

一个 Skill 是一个可复用的工程工作流，而不是一段提示词：`SKILL.md` frontmatter 给出触发条件，正文给出
可核对的步骤，脚本给出证据。

- **可编辑面**：`AGENTS.md`、`tool_guidance.md`、`guardrails.md`、`memory.md`、`hooks.json`、
  `skills/<name>/SKILL.md`、`sub_agents/<name>/AGENT.md`、`scripts/<snake_case>.py`。
- **按需与常驻分开计价**：常驻文本每次运行都付费（默认预算 4000 字符），技能在触发前零成本。因此知识
  优先放进按需技能，常驻文本只留触发行。
- **触发行是组件的一部分**：技能必须被常驻文本点名，否则永远不会被打开。`AutoTrigger()` 补齐缺失的触发行，
  `Validate()` 拒绝没有 description 的技能。
- **插件贡献同一面**：`Registry.Skills()` / `Subagents()` 解析后进入 harness 视图，`load_skill` 与
  `delegate` 读取的是解析后的有效集合。

## 种子技能（`internal/harness/seed.go`）

`POST /api/harness/seed` 或 `CLAWMIND_SEED_SKILLS=1` 写入的五个优先技能，改变的是 agent 如何工作，而不是
它产出什么：

| Skill | 触发条件 | 核心要求 |
|-------|----------|----------|
| `systematic-debugging` | Bug 或测试失败需要定位根因 | 复现 → 缩小 → 证伪假设 → 修根因 → 留下失败过的测试 |
| `test-driven-development` | 要实现新行为且项目有测试框架 | 先写会失败的测试，断言验收标准而非实现细节 |
| `verification-before-completion` | 准备宣布完成 | 跑测试、lint、build，读完整输出；用 `scripts/preflight.py` 收集证据 |
| `code-review` | 需要审查一段改动 | 正确性、安全性、可维护性、范围；结论分条且可执行 |
| `architecture-design` | 涉及模块边界或技术选型 | 先写接口与数据模型；选型给两个候选与取舍；写成 ADR |

工作流链写在常驻文本里：

`Requirement → Architecture → Plan → TDD → Review → Verification → Release`

关键约束是：**写完代码不等于成功，完成声明必须带可验证的工程证据**。`scripts/preflight.py` 按项目类型
推断验证命令（`go.mod` → `go test ./...` + `go vet ./...`；`package.json` → `npm test` + `npm run lint`；
`pytest`），没有推断出命令时明确报告「未收集证据」而不是假装通过。

## 值得纳入的十类（按 `reports/plan-2.md`）

TDD、Systematic Debugging、Code Review、Verification Before Completion、Architecture Design、
Planning/Task Decomposition、Refactoring、Security Review、E2E Testing、Documentation/ADR。

优先级建议：先做上面五个，它们的组合比堆积几十个零散的框架 Skill 更有价值。其余五类作为按需技能加入，
每个都必须有触发行。

## SKILL.md 生态兼容

- `plugin.ImportSkillDir(dir)` 读取社区布局（Claude / OpenClaw 的 `skills/<name>/SKILL.md`）并映射到可编辑
  面：frontmatter `name` / `description` / `mode` → 组件，脚本保留 snake_case，不在面上的文件跳过并记录。
- `POST /api/skills/import-md` 接受 multipart 或 `{projectId, content}`，写入前跑完整 `Validate()`。
- 不承诺覆盖全部社区变体：社区 frontmatter 字段（`license`、`allowed-tools` 等）只读取运行时真正消费的
  `name` / `description` / `mode`。

## 路线图

1. **远程注册中心**：HTTP 拉取与版本锁定（ClawHub 一类）。当前不引入未审计的代码执行面，因此排在安全与
   记忆正确性之后。
2. **导出检查清单**：把内部 tools JSON 与 SKILL.md 的映射（名称、描述、参数 schema）在 UI 或文档中给出。
3. **技能效果度量**：把 `delegate` 与 `load_skill` 的调用与结果计入 harness 的证据，使「这条技能是否有效」
   可被 `Reflect` 判定，而不是靠印象。

以本文件为契约，避免与 [architecture-target.md](architecture-target.md) 重复展开实现细节；插件契约见
[插件层](plugins.md)。
