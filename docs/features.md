# ClawMind 特点与能力

## 产品定位

ClawMind 是面向开发者的 **本地优先** 代码项目 Agent：单仓库内 **Go 后端** + **Vue 3 前端**，支持流式回复、会话与项目管理，以及**代码工具型 Agent** 与**自进化 harness**。定位是代码任务，不做通用任务助手。

## 核心特性

### 对话与界面

- **类 ChatGPT 主列**：主内容区宽度约为 **侧栏以外区域的 4/5（80%）**（`w-4/5`），随窗口变宽；用户气泡不超过列宽约 **85%**。
- **Markdown 表格**：GFM 风格表格（`markdown-it-multimd-table`），带边框、表头底纹与斑马行，接近 ChatGPT 阅读体验。
- **用户 / 助手头像**：自定义 PNG 头像与 SVG Logo，侧栏与消息列表统一品牌感。
- **Cursor 风格 Agent 输出**：任务流程、思考过程、工具执行、最终回答 **分段展示**；任务与思考支持 **折叠 / 展开**（默认折叠，流式时在对应阶段自动展开）。
- **Markdown**：列表、代码高亮；代码块带 **一键复制**；行内/块级 LaTeX 公式由 **KaTeX** 渲染（`$...$`、`$$...$$`）。

### 配置

- **`.clawmind/config.json`**：模型地址、API Key、采样参数、`maxAgentRounds` 等；**打开设置** 与 **发起对话** 均读取该文件。
- 支持环境变量后备（如 `OPENAI_API_KEY`、`OPENAI_BASE_URL`）；**推荐用环境变量存放 API Key**，减少配置文件被工具误读的风险。

### 代码工具与自进化

- **非流式工具轮**：在流式最终回答前，可多次调用模型进行 **function calling**，执行后再继续。
- **内置原子工具**：`file_read`、`file_write`、`shell_exec`（Windows / macOS / Linux 差异由后端处理）、`web_fetch`、`task_plan`、`task_summary`。

**代码向工具**（`backend/internal/tools/code.go`）：`code_search`（有边界的字面量/正则搜索）、`repo_tree`（有界结构，跳过依赖与构建目录）、`file_edit`（精确替换，`old` 歧义即失败）、`run_tests`（跑推断出的验证命令）、`load_skill`（按需读取 harness 技能）、`delegate`（在独立上下文跑一个 subagent，只回传文本）。

**仓库画像**：`internal/codectx` 扫描语言、manifest、推断的 test/build/lint/format 命令与有界目录树，注入系统提示；不 shell out。

**改后必验证**：某一轮发生文件改动后自动跑推断的测试命令，输出回灌进对话，失败必须被读而不是被忽略（`CLAWMIND_AUTO_VERIFY`）。

**自进化 harness**：两级递归（任务级 Propose–Probe–Compose–Select–Reflect + 经验级经验库），可编辑面只有 `AGENTS.md`、`tool_guidance.md`、`lessons.md`、`skills/`、`scripts/`；写入前整树校验，证据不足时保留基线。侧栏 Harness 面板可查看与运行。详见 [自进化](self-evolution.md)。
- **工作目录**：`AGENT_WORKSPACE` 限制文件与 Shell 的相对路径根，降低误操作范围。
- **高危 Shell**：匹配启发式危险模式时，经 SSE 推送确认请求，用户通过 REST 批准或拒绝后再执行（非沙箱，仍需谨慎）。
- **web_fetch**：对目标 URL 做 SSRF 过滤，禁止访问内网与链路本地等地址。
- **技能扩展**：侧栏 **导入** JSON（OpenAI tools 形状）或 **新建** 技能，合并写入 `.clawmind/skills.json`，并与 `TOOLS_PATH` 指向的 JSON、插件贡献的工具及内置工具合并。
- **SKILL.md 导入**：`POST /api/skills/import-md` 接受 multipart 或 `{projectId, content}`，按社区 frontmatter 契约解析，写入前跑整树校验，缺触发行的技能由 `AutoTrigger()` 补齐。

### 多级记忆（L0–L3）

记忆不是扁平记录，而是逐层蒸馏，每层回答不同的问题（参考 [TencentDB-Agent-Memory](https://github.com/TencentCloud/TencentDB-Agent-Memory)）：

- **L0 conversation**：原始轮次——精确措辞、时间戳、来源。
- **L1 atom**：事实、偏好、约束、事件——精确召回。
- **L2 scenario**：围绕项目组织的知识块——快速恢复上下文。
- **L3 persona**：长期画像与稳定模式——快速进入上下文。

写入时只保留可泛化的流程，不保留本次任务的具体事实；L3 是合并而非追加。默认写入 **SQLite**（与主库同文件），
**重启后保留**；`CLAWMIND_MEMORY_BACKEND=memory` 为进程内实现（多用于测试）。

检索分两条路径：**bootstrap 层**（L2、L3）按新近顺序直接给出快速上下文，**fallback 层**（L0、L1）用
BM25 + 向量 + RRF 融合召回具体事实。BM25 分词把 CJK 单字与 ASCII 词分开——中文查询没有空格，只按空白切词的
词表永远匹配不到。融合用排名而非分数相加，因为 BM25 与余弦相似度不在同一量纲。

检索在「会把记忆挤进上下文之外」的那一点上被预算约束：条目数（默认 24）、字符数（默认 2000）、超时（默认 3s）。
嵌入失败或超时降级为 BM25，而不是返回空。每层另有条目上限（L0 400 / L1 150 / L2 100 / L3 200）：一个无上限的
层最终会把其余上下文挤出去。

### 资产登记与访问控制

聊天记忆、技能、wiki 页、代码图都是**记忆资产**，用同一套登记：所有权、版本、可见性、ACL、绑定。共享是显式
动作，不是默认泄露——`private` / `team` / `restricted` / `agent` 四种可见性，`restricted` 需要逐条 ACL。
`draft` / `ready` / `archived` 记录异步构建状态：wiki 与代码图在 `ready` 前不可用。绑定到 agent 的资产构成
它的 loadout，系统提示里的装载块只呈现访问控制已经判定为可见的部分。

### 插件层与 seam

每个部件都是一个插件：模型适配、工具注册表、会话存储、记忆存储、agent 循环、提示构造、中间件。没有可被补丁的
特权核心——行为靠挂载插件产生，每条注册都是一个在卸载时撤销的 effect。层顺序决定谁生效；校验不通过的插件被
跳过并报告，而不是被静默丢弃。详见 [插件层](plugins.md)。

- 插件贡献的 skill 与 subagent 进入同一个可编辑面，`load_skill` 与 `delegate` 读取解析后的有效集合。
- 插件层只能收紧安全策略：可以添加 deny / high-risk 模式，不能移除内置地板；只能降低 shell 超时与常驻文本预算；
  工作区根只能收窄，超出进程已有根路径的请求被拒绝。
- 声明了运行时不读取的 patch key 会被校验拒绝——一个声明了行为却从不执行的插件，正是这套设计要暴露的失败模式。嵌套值同样要能被读取：`caps` 只接受 `l0`–`l3`（大小写不敏感），写错会被报告而不是静默丢弃。
- 侧栏 **插件层与记忆** 面板列出层顺序、解析后的配置与分层 census；`GET /api/settings` 返回文件配置加 `effective` 与 `source`，因为插件层赢的时候文件值并不生效。记忆 cap 不只是被报告，存储层裁剪时实际执行它（内存后端也一样）。

### Hooks

`SessionStart`、`UserPromptSubmit`、`PreToolUse`、`PostToolUse`、`PostCompact`、`Stop`、`SubagentStop`。
退出码 `2` 是文档化的否决：调用方停止这次工具调用或本轮；其它非零退出码记录为失败但不阻塞。hook 的 stdout 作为
指导回灌进工具结果；声明但被跳过的 hook 在 trace 里记为 `skipped`，而不是消失。`PostCompact` 只在上下文真的被
裁剪时触发。

### 委派

`delegate` 工具在独立上下文里跑一个 harness subagent，只回传文本，上限 4000 字符。委派只在「输入大、回答小」
时便宜：无上限的子代理只是把成本从工具调用挪到 transcript。

### 软件工程 Skills

五个优先技能（`systematic-debugging`、`test-driven-development`、`verification-before-completion`、
`code-review`、`architecture-design`）加一条工作流链：Requirement → Architecture → Plan → TDD → Review →
Verification → Release。核心约束是**完成声明必须带可验证的工程证据**，`scripts/preflight.py` 在报告完成前收集
测试与 lint 证据。详见 [Skills 路线图](skills-roadmap.md)。

### 数据持久化

- **SQLite**：会话、消息、项目、默认模式下的 L1–L3 记忆行与可选向量等。
- **配置与技能**：`.clawmind/` 下 `config.json`、`skills.json`。

## 流式与成本

- **停止生成**：前端可调用取消接口或断开 SSE，后端会取消当次生成上下文。
- **Token 预算**：环境变量 `CLAWMIND_TOKEN_BUDGET` 可限制单次回复链路中累计的 completion token（0 不限制）。
- **工具轮次**：`maxAgentRounds` 限制 function calling 轮数，避免无限循环消耗。

## 安全提示

- `shell_exec` 会在本机执行命令，请仅在可信环境使用，并合理设置 `AGENT_WORKSPACE`；高危命令需人工确认，仍**不是**容器级沙箱。
- API Key 存放在本地配置文件时，勿将 `.clawmind/` 提交到公开仓库（仓库已默认 `.gitignore`）；`file_read` 禁止访问配置目录中的敏感文件。

## 主题

- 支持 **浅色 / 深色** 切换（本地偏好存储），与系统偏好可联动。

## 连接恢复

- SSE 在网络异常时可 **自动重连**；服务端在流式过程中尽量幂等更新同一条助手消息，减少重复段落（完整断点续传见路线图）。
