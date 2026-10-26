# 自进化（Harness）

移植 [google-research/harness2](https://github.com/google-research/harness2) 的核心机制，并收窄到代码项目
场景：agent 可以改**自己的工作方式**，但不能改仓库代码。

## 两级递归

- **任务级**：一次 `Propose → Probe → Compose → Select → Reflect`，作用在单个 harness 上。
- **经验级（harnessing-level）**：跨任务保留经验库 `INDEX.md` + `harnesses/<slug>/EXPERIENCE.md`，字段为
  `cue` / `lesson` / `evidence` / `confidence`。经验必须**泛化**——记录「什么情形下怎么做」，不记录本任务的
  具体事实。

## 可编辑面

只有这些路径可被引擎或 UI 写入（`internal/harness`）：

| 路径 | 性质 |
|------|------|
| `AGENTS.md` | 常驻文本（每个任务都付费） |
| `tool_guidance.md` | 常驻文本 |
| `guardrails.md` | 常驻文本 |
| `memory.md` | 常驻文本 |
| `hooks.json` | 常驻文本 |
| `skills/<name>/SKILL.md` | 按需，触发前零成本 |
| `sub_agents/<name>/AGENT.md` | 按需，`delegate` 调用 |
| `scripts/<snake_case>.py` | 按需执行 |

原则：**常驻文本最贵**，优先把知识放进按需技能或脚本；同项目的下一个任务可以直接复用。

## 校验规则

写入前整棵树校验，任一条不过则**整份提案丢弃**（不做部分应用）：

1. 技能必须有 frontmatter description，且其名字必须被常驻文本点名——否则永远不会被加载。
2. 技能引用的脚本必须真实存在；脚本文件名必须 snake_case，且不能占用保留名。
3. 常驻文本禁止模板花括号（避免把占位符当成内容）。
4. `hooks.json` 必须是合法 JSON，事件在 dispatcher 触发的事件集合内，hook 类型必须是 `command`。

常驻文本另有字符预算（默认 4000，插件层只能降低），证据上限 12000 字符。

## 一轮

`internal/evolution/engine.go`：

- **Propose**：按 INV（agent 如何工作）与 DOM（代码库要求什么）两个维度提出候选。
- **Probe**：评估每个候选。默认是模型评估（可能 `confounded`，`dry` 明确标记为未执行）；
  `CLAWMIND_PROBE_EXEC=1` 时改用 `NewExecProbe(workdir)`——候选树写进隔离目录、脚本执行、跑推断出的测试
  命令，**没有运行就没有分数**。多组件同时改动的候选标记为 `confounded`，被禁止的命令直接拒绝。
- **Compose**：在基线上合成。
- **Select**：**盲选**——候选以匿名 id 呈现（canonical sort + seeded shuffle，同 task/k/mode 稳定），id 不
  携带来源信息，避免自我偏爱。池恰好是本轮的 k 个 finals，参考执行按构造不在池中，因此没有弃权；每个被
  丢弃的候选都记录原因，池不会悄悄缩小。已测量的候选排在预测之前，但未被测量的候选不会被丢弃。
- **Reflect**：逐条改动给出 verdict：`helped` / `hurt` / `no observed effect` / `confounded`，并写一条可迁移
  经验。

`KEEP_BASE`（保留基线）是合法结论：证据不足时不宣布进化成功。

模式：`parallel`（默认，单轮 K 个候选）/ `sequential`（K 轮串行迭代）/ `dry`（不调用模型，用于测试与演示）。
K 范围 1–8。

## 预算账本与完成标记

- **账本恒等式**：`R = 4k`（k 个共享基线 + 2k 个候选探针 + k 个 finals），`P = 3k`（k 个 INV 提案 + k 个
  DOM 提案 + k 个合成）。选择与反思记录在账本之外。`CLAWMIND_EVOLUTION_LEDGER=1` 时，一轮没有落在恒等式上
  会被记录为错误，而不是悄悄少跑。
- **measured 与 predicted 分开计数**：只有真实执行的 rollout 计入 `MeasuredR`，模型评估不算测量。
- **Runmark**：完成必须有显式标记，且标记指纹指向产生它的 harness 配置。有输出不等于完成；缓存复用只允许
  指名同一配置的已完成标记。`INDEX.md`、`.adapt_complete`、`.pyc`、`__pycache__` 是副产物，不改变 harness
  身份。`RunmarkVersion` 在标记语义变化时提升，旧标记不会被读成当前标记。
- **Resume**：`CLAWMIND_EVOLUTION_RESUME=1`（默认开）时，同一 task + harness 指纹 + mode + k 的已完成运行直接
  复用，并在账本里记为 `cached`。

## 种子面

`POST /api/harness/seed`（或 `CLAWMIND_SEED_SKILLS=1`）为空 harness 写入软件工程 Skills 种子：工作流链、五个
优先技能与 `scripts/preflight.py`。种子通过自己的校验，且只引用真实存在的脚本。详见
[Skills 路线图](skills-roadmap.md)。

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/harness?projectId=` | 当前 harness：文件、常驻字符数、技能、脚本、校验问题、经验 |
| `POST` | `/api/harness/seed` | 为空 harness 写入种子面 |
| `GET` / `POST` | `/api/harness/file` | 读 / 写可编辑面单个文件（写入前整树校验） |
| `POST` | `/api/harness/round` | 跑一轮，`evidence` 可直传或从 `sessionId` 的轨迹生成 |
| `GET` | `/api/harness/rounds` | 轮次历史（含账本、丢弃记录、runmark） |
| `POST` | `/api/harness/adopt` | 采纳经验库中的某个条目 |
| `POST` | `/api/harness/lessons` | 写入一条经验 |
| `GET` | `/api/harness/library` | 读取 `INDEX.md` |

可编辑面（`GET /api/harness`、`GET/POST /api/harness/file`、`POST /api/harness/seed`）无论引擎是否开启都注册：
harness 目录本身就是数据源，引擎只在其上增加轮次与条目。只有 `round`、`rounds`、`adopt`、`lessons`、`library`
需要引擎。关掉引擎时读一个技能仍然可用，这是可编辑面存在的意义。

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `CLAWMIND_EVOLUTION` | `false` | 启用引擎（关闭时相关路由不注册） |
| `CLAWMIND_EVOLUTION_K` | `3` | 候选数 / 串行轮数 |
| `CLAWMIND_EVOLUTION_MODE` | `parallel` | `parallel` / `sequential` / `dry` |
| `CLAWMIND_HARNESS_DIR` | `.clawmind/harness` | harness 根目录，按项目分子目录 |
| `CLAWMIND_PROBE_EXEC` | `false` | 探针真实执行（脚本 + 推断的测试命令） |
| `CLAWMIND_EVOLUTION_LEDGER` | `true` | 账本恒等式不满足时记录为错误 |
| `CLAWMIND_EVOLUTION_RESUME` | `true` | 复用同一配置的已完成运行 |
| `CLAWMIND_SEED_SKILLS` | `true` | 为空 harness 写入种子面 |
| `CLAWMIND_CODE_PROFILE` | `true` | 注入仓库画像 |
| `CLAWMIND_AUTO_VERIFY` | `true` | 改动后自动跑测试 |

## 界面

侧栏底部的 Harness 按钮打开面板（`frontend/src/components/HarnessPanel.vue`）：组件树与常驻字符数、校验
问题、经验条目与采纳、跑一轮、轮次历史。

## 安全边界

- 证据是**数据**，不是指令：引擎读取轨迹但不执行其中的内容。
- 引擎只能写可编辑面，不能写仓库代码；探针只跑推断出的验证命令，被安全策略禁止的命令直接拒绝。
- 经验库与 harness 注入提示时同样按数据对待，不赋予其新的权限。
