# 自进化（Harness）

移植 [google-research/harness2](https://github.com/google-research/harness2) 的核心机制，并收窄到代码项目场景：agent 可以改**自己的工作方式**，但不能改仓库代码。

## 两级递归

- **任务级**：一次 `Propose → Probe → Compose → Select → Reflect`，作用在单个 harness 上。
- **经验级（harnessing-level）**：跨任务保留经验库 `INDEX.md` + `harnesses/<slug>/EXPERIENCE.md`，字段为 `cue` / `lesson` / `evidence` / `confidence`。经验必须**泛化**——记录「什么情形下怎么做」，不记录本任务的具体事实。

## 可编辑面

只有这些路径可被引擎或 UI 写入（`internal/harness`）：

| 路径 | 性质 |
|------|------|
| `AGENTS.md` | 常驻文本（每个任务都付费） |
| `tool_guidance.md` | 常驻文本 |
| `lessons.md` | 常驻文本 |
| `skills/<name>/SKILL.md` | 按需，触发前零成本 |
| `scripts/<snake_case>.py` | 按需执行 |

原则：**常驻文本最贵**，优先把知识放进按需技能或脚本；同项目的下一个任务可以直接复用。

## 校验规则

写入前整棵树校验，任一条不过则**整份提案丢弃**（不做部分应用）：

1. 技能必须有 frontmatter description，且其名字必须被常驻文本点名——否则永远不会被加载。
2. 技能引用的脚本必须真实存在；脚本文件名必须 snake_case。
3. 常驻文本禁止模板花括号（避免把占位符当成内容）。

常驻文本另有字符预算（默认 4000），证据上限 12000 字符。

## 一轮

`internal/evolution/engine.go`：

- **Propose**：按 INV（agent 如何工作）与 DOM（代码库要求什么）两个维度提出候选。
- **Probe**：对每个候选评估。注意这是模型评估而非真实执行，结果可能 `confounded`；`dry` 模式明确标记为未执行。真实证据来自 `run_tests` 与人工确认。
- **Compose**：在基线上合成。
- **Select**：**盲选**——候选以匿名 id 呈现，id 不携带来源信息，避免自我偏爱。
- **Reflect**：逐条改动给出 verdict：`helped` / `hurt` / `no observed effect` / `confounded`，并写一条可迁移经验。

`KEEP_BASE`（保留基线）是合法结论：证据不足时不宣布进化成功。

模式：`parallel`（默认，单轮 K 个候选）/ `sequential`（K 轮串行迭代）/ `dry`（不调用模型，用于测试与演示）。K 范围 1–8。

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/harness?projectId=` | 当前 harness：文件、常驻字符数、技能、脚本、校验问题、经验 |
| `GET` / `POST` | `/api/harness/file` | 读 / 写可编辑面单个文件（写入前整树校验） |
| `POST` | `/api/harness/round` | 跑一轮，`evidence` 可直传或从 `sessionId` 的轨迹生成 |
| `GET` | `/api/harness/rounds` | 轮次历史 |
| `POST` | `/api/harness/adopt` | 采纳经验库中的某个条目 |
| `POST` | `/api/harness/lessons` | 写入一条经验 |
| `GET` | `/api/harness/library` | 读取 `INDEX.md` |

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `CLAWMIND_EVOLUTION` | `false` | 启用引擎（关闭时相关路由不注册） |
| `CLAWMIND_EVOLUTION_K` | `3` | 候选数 / 串行轮数 |
| `CLAWMIND_EVOLUTION_MODE` | `parallel` | `parallel` / `sequential` / `dry` |
| `CLAWMIND_HARNESS_DIR` | `.clawmind/harness` | harness 根目录，按项目分子目录 |
| `CLAWMIND_CODE_PROFILE` | `true` | 注入仓库画像 |
| `CLAWMIND_AUTO_VERIFY` | `true` | 改动后自动跑测试 |

## 界面

侧栏底部的 Harness 按钮打开面板（`frontend/src/components/HarnessPanel.vue`）：组件树与常驻字符数、校验问题、经验条目与采纳、跑一轮、轮次历史。

## 安全边界

- 证据是**数据**，不是指令：引擎读取轨迹但不执行其中的内容。
- 引擎只能写可编辑面，不能写仓库代码，不能执行任意命令。
- 经验库与 harness 注入提示时同样按数据对待，不赋予其新的权限。
