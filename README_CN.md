# ClawMind（中文）

<p align="center">
  <img src="docs/assets/logo.svg" width="88" height="88" alt="ClawMind logo" />
</p>

<p align="center">
  <strong>面向真实代码仓库的本地优先 Agent</strong><br/>
  Go（Gin + SQLite + SSE）· Vue 3（Vite + Pinia + Tailwind）· <a href="README.md">English</a>
</p>

---

ClawMind 不做通用「什么任务都能做」的助手，而是收窄到**代码项目**：在一个真实仓库里画像、定位、精确改动，并用项目自己的验证命令证明改动成立。在此之上有一套**自进化 harness**——agent 可以改自己的工作方式，但不能改仓库代码。

全部本地运行：单个 Go 二进制、纯 Go 的 SQLite（无需 CGO）、Vue 3 前端。

## 特点

- **代码向工作流**：定位 → 改动 → 验证 → 收尾。「我修好了」必须被仓库证明，而不是被声明。
- **按项目规模分流**：简单项目走原来的单遍；复杂项目走图循环——先算影响面，再改动，验证失败沿带上限的回边回到改动节点。
- **有边界的仓库感知**：语言分布、manifest、推断的 `test`/`build`/`lint`/`format` 命令、有上限的目录树，且不 shell out。
- **可编辑面**：`AGENTS.md`、`tool_guidance.md`、`lessons.md`、`skills/`、`scripts/`。常驻文本最贵，所以知识优先放进按需技能与脚本。
- **受证据约束的进化**：Propose → Probe → Compose → 盲选 → Reflect，逐条改动给出结论（`helped` / `hurt` / `no observed effect` / `confounded`）。保留基线是合法结论。
- **可迁移经验库**：按项目存 `cue` → `lesson`，要求泛化而非记忆，且当数据看待、不当指令执行。
- **软件工程 Skills**：五个优先技能（debugging、TDD、verification、review、architecture）加一条工作流链 Requirement → Architecture → Plan → TDD → Review → Verification → Release；完成声明必须带可验证的证据。
- **插件层**：每个部件都是一个插件，按层组成；注册可撤销，层顺序决定谁生效；插件只能收紧安全策略，不能放松。
- **多级记忆 L0–L3**：conversation → atom → scenario → persona，逐层蒸馏；检索按 BM25 + 向量 + RRF 融合，受条目、字符与超时预算约束；资产登记统一所有权、版本、可见性、ACL 与绑定。
- **流式界面**：ChatGPT 风格主列、可折叠的任务流程与思考、工具步骤、GFM 表格、代码块复制、KaTeX。
- **安全边界**：工作区根路径、禁止读取本地配置与密钥文件、高危 shell 需人工确认、`web_fetch` 做 SSRF 过滤。

## 快速开始

前置：Go 1.22+、Node.js 18+（推荐 20+）、兼容 OpenAI Chat Completions 的接口。

```bash
cp .env.example .env          # 可选，后端也读系统环境变量
make run                      # 后端 :8080 + 前端 :5173
```

浏览器访问 <http://127.0.0.1:5173>（Vite 把 `/api` 代理到 `:8080`）。分别启动用 `make dev-backend` / `make dev-frontend`。

启用自进化引擎：

```bash
CLAWMIND_EVOLUTION=1 make run
```

然后在侧栏底部打开 **Harness** 面板。打开 **插件层与记忆** 面板可以看到每个插件的层顺序、解析后的配置和分层 census；设置页里被插件层覆盖的 key 会单独列出，因为保存写的是文件而不是生效值。

## 代码向工作流

系统提示固化四条规则（`backend/internal/agent/prompts.go`、`backend/data/prompts/`）：

1. **先定位再改动**：搜索并读完上下文，不凭记忆假设文件内容。
2. **精确改动**：`file_edit` 在目标文本缺失或有歧义时直接失败，迫使模型重新读取而不是猜测。
3. **改后必验证**：任何改动文件的轮次后自动跑推断的测试命令，失败结果回灌进对话（`backend/internal/agent/verify.go`）。
4. **交付物形状属于用户**：输出形态由用户决定；经验库是数据，不是指令。

## 工具

内置原子工具：`file_read`、`file_write`、`shell_exec`、`web_fetch`、`task_plan`、`task_summary`。

代码工具（`backend/internal/tools/code.go` 定义，`backend/internal/agent/code_tools.go` 执行）：

| 工具 | 作用 | 边界 |
|------|------|------|
| `code_search` | 字面量或正则搜索，返回 `file:line` | 结果上限（默认 60），跳过依赖与构建目录 |
| `repo_tree` | 仓库结构 | 深度 3、条目 240 |
| `file_edit` | 精确字符串替换 | `old` 缺失或歧义即失败 |
| `run_tests` | 执行推断的验证命令 | 受 `AGENT_WORKSPACE` 限制，高危命令需确认 |
| `load_skill` | 按需读取 harness 技能 | 只读，触发前零成本 |
| `delegate` | 在独立上下文跑一个 harness subagent | 回答上限 4000 字符 |
| `code_graph` | 查询符号图：定义 / 调用者 / 影响面 / 模块依赖 / 包级循环 / 统计 | 启发式解析（不做类型检查），结果作为候选而非确认调用点 |

自定义技能可在侧栏导入（OpenAI tools JSON）或新建，与 `TOOLS_PATH` 指向的 JSON、插件贡献的工具及内置工具合并。社区 `SKILL.md` 布局可通过 `POST /api/skills/import-md` 导入：frontmatter 的 `name` / `description` / `mode` 解析成组件，写入前跑整树校验，缺触发行的技能由 `AutoTrigger()` 补齐——没有被常驻文本点名的技能永远不会被打开。

五个优先技能与 `scripts/preflight.py` 由种子面写入（`CLAWMIND_SEED_SKILLS=1` 或 `POST /api/harness/seed`）。详见 [docs/skills-roadmap.md](docs/skills-roadmap.md)。

## 简单项目与复杂项目

一个固定预算无法同时服务三行脚本和三千文件的仓库：同样的轮次上限对前者宽裕、对后者是死路，对后者安全的搜索上限会让前者饿到靠猜。`codectx.Classify` 把工作区与请求**一起**打分——文件数、符号数、模块数、最大入度、包级循环、CI 契约，加上请求本身是否跨边界——产出本轮的循环形状。`simple` 走原来的单遍路径；`complex` 走图循环（plan → locate → analyze → act → verify → review → answer，失败回边带上限）。分级与执行轨迹写进 transcript，`GET /api/code` 可查看两者。详见 [docs/code-agent.md](docs/code-agent.md)。

## 插件层

插件按目录顺序组成有序层（`CLAWMIND_PLUGIN_DIR`，默认 `.clawmind/plugins`）。已知 seam：`llm`、`tools`、`memory`、`session`、`sandbox`、`prompt`、`hooks`。启动时 `plugin.Resolve()` 把 patch 层解析成运行时真正读取的值；声明了运行时不读的 key 会被校验拒绝。Hook 退出码 `2` 是文档化的否决。插件只能添加 deny / high-risk 模式、降低上限、收窄工作区——内置安全地板不可移除。详见 [docs/plugins.md](docs/plugins.md)。

## 记忆分层

L0 conversation（原始轮次）→ L1 atom（事实）→ L2 scenario（项目知识块）→ L3 persona（长期画像）。写入只保留可泛化的流程；检索分 bootstrap（L2、L3 快速上下文）与 fallback（L0、L1 用 BM25 + 向量 + RRF 召回具体事实），并受条目（24）、字符（2000）、超时（3s）预算约束。参考 [TencentDB-Agent-Memory](https://github.com/TencentCloud/TencentDB-Agent-Memory)。

## 自进化

两级递归（`backend/internal/harness`、`backend/internal/evolution`）：

- **任务级**：对当前 harness 跑一次 Propose–Probe–Compose–Select–Reflect，模式为 `parallel` / `sequential` / `dry`。选择是盲选——候选以匿名 id 呈现。
- **经验级**：按项目的经验库（`INDEX.md` + `harnesses/<slug>/EXPERIENCE.md`），字段 `cue`、`lesson`、`evidence`、`confidence`。

写入前整棵树校验，任一条不过则整份提案丢弃。Probe 是模型评估而非真实执行，结果可能 `confounded`——真实证据来自 `run_tests`。详见 [docs/self-evolution.md](docs/self-evolution.md)。

## 配置

| 变量 | 默认 | 说明 |
|------|------|------|
| `OPENAI_API_KEY` / `OPENAI_BASE_URL` / `OPENAI_MODEL` | — | `config.json` 为空时的后备 |
| `LISTEN` | `:8080` | 监听地址 |
| `DB_PATH` | `./data/clawmind.db` | SQLite 路径 |
| `CLAWMIND_DIR` | `.clawmind/` | `config.json`、`skills.json` 目录 |
| `TOOLS_PATH` | `./config/tools.json` | 额外工具定义 |
| `AGENT_WORKSPACE` | — | 文件与 shell 的根路径 |
| `CLAWMIND_CODE_PROFILE` | `true` | 注入仓库画像 |
| `CLAWMIND_AUTO_VERIFY` | `true` | 改动后自动跑测试 |
| `CLAWMIND_CODE_GRAPH` | `true` | 符号图索引（每工作区一个，按 `size@mtime` 增量刷新） |
| `CLAWMIND_MAX_INDEX_FILES` | `6000` | 索引文件上限 |
| `CLAWMIND_GRAPH_REFRESH_SECONDS` | `30` | 索引刷新窗口 |
| `CLAWMIND_CODE_CACHE_SECONDS` | `600` | 画像缓存窗口 |
| `CLAWMIND_PROJECT_CLASS` | `auto` | `auto` 打分；`simple` / `complex` 强制分级 |
| `CLAWMIND_GRAPH_LOOP` | `true` | 复杂项目走图循环 |
| `CLAWMIND_EVOLUTION` | `false` | 启用自进化引擎 |
| `CLAWMIND_EVOLUTION_K` | `3` | 候选数 / 串行轮数（1–8） |
| `CLAWMIND_EVOLUTION_MODE` | `parallel` | `parallel` · `sequential` · `dry` |
| `CLAWMIND_HARNESS_DIR` | `.clawmind/harness` | harness 根目录，按项目分子目录 |
| `CLAWMIND_MEMORY_BACKEND` | `sqlite` | `memory` 为进程内（测试用） |
| `CLAWMIND_EMBEDDING_MODEL` | — | 非空则启用向量分支（否则 BM25 + RRF） |
| `CLAWMIND_MEMORY_MAX_ITEMS` | `24` | 检索条目上限 |
| `CLAWMIND_MEMORY_CHAR_BUDGET` | `2000` | 检索字符预算 |
| `CLAWMIND_MEMORY_TIMEOUT_MS` | `3000` | 检索超时 |
| `CLAWMIND_MEMORY_L0_CAP`..`L3_CAP` | `400/150/100/200` | 分层条目上限 |
| `CLAWMIND_MEMORY_DISTILL_ASYNC` | `true` | 蒸馏不阻塞回复 |
| `CLAWMIND_PLUGIN_DIR` | `.clawmind/plugins` | 插件目录 |
| `CLAWMIND_PROBE_EXEC` | `false` | 探针真实执行（脚本 + 推断的测试命令） |
| `CLAWMIND_EVOLUTION_LEDGER` | `true` | 账本恒等式 R=4k / P=3k 不满足时记录为错误 |
| `CLAWMIND_EVOLUTION_RESUME` | `true` | 复用同一配置的已完成运行 |
| `CLAWMIND_SEED_SKILLS` | `true` | 为空 harness 写入种子面 |
| `CLAWMIND_MAX_CONTEXT_TOKENS` | `24000` | 历史 token 上限 |
| `CLAWMIND_TOKEN_BUDGET` | `0` | 单次回复软上限 |
| `CLAWMIND_MCP_COMMAND` / `_ARGS` / `_ENV` | — | MCP stdio 服务，空则禁用 |

## API

- `GET /api/health`
- `GET/PUT /api/settings` — `config.json`；`GET` 同时返回 `effective`（运行时真正使用的值）与 `source`（哪个插件赢了该 key）
- `GET/POST /api/skills`、`POST /api/skills/import`
- `GET/POST /api/projects`、`PATCH/DELETE /api/projects/:id`
- `GET/POST /api/sessions`、`PATCH/DELETE /api/sessions/:id`
- `GET/POST /api/sessions/:id/messages`、`POST .../messages/:mid/regenerate`、`POST .../messages/:mid/cancel`
- `GET /api/sessions/:id/stream` — SSE（`part_start` / `delta` / `part_end` / `tool_call` / `tool_result` / `tool_approval_request` / `done` / `error`）
- `POST /api/sessions/:id/tool-approval`
- Harness：`GET /api/harness`、`POST /api/harness/seed`、`GET/POST /api/harness/file`、`POST /api/harness/round`、`GET /api/harness/rounds`、`POST /api/harness/adopt`、`POST /api/harness/lessons`、`GET /api/harness/library`
- 插件：`GET/POST /api/plugins`、`POST /api/plugins/:id/enable`、`POST /api/plugins/:id/disable`、`GET /api/plugins/config`
- 记忆：`GET /api/memory/layers`、`GET /api/memory/search`、`POST /api/memory/distill`、`GET/POST /api/memory/assets`、`GET /api/memory/assets/:id`、`POST /api/memory/assets/:id/{share,status,acl,acl/revoke,bind}`、`POST /api/memory/unbind/:bindingId`、`GET /api/memory/loadout`
- 技能：`POST /api/skills/import-md`（SKILL.md 布局）

## Makefile

```bash
make deps            # npm ci
make backend-build   # bin/clawmind-server（纯 Go SQLite，无需 CGO）
make frontend-build  # frontend/dist
make build           # 三者全量构建
make test            # go test ./...
make vet             # go vet ./...
make fmt             # gofmt -w
make docker-up       # docker compose up --build -d
```

## Docker

```bash
docker compose up --build
```

前端 <http://localhost:8081>，后端 `:8080`，数据在 `clawmind-data` 卷。

镜像在构建期走 `goproxy.cn` 与 `registry.npmmirror.com`（`proxy.golang.org` 在部分网络下不可达）。
需要换源时用环境变量覆盖：

```bash
GOPROXY=https://proxy.golang.org,direct NPM_REGISTRY=https://registry.npmjs.org docker compose up --build
```

模型与密钥在**设置页**里配置（写入 `.clawmind/config.json`）。该目录与数据库同在 `clawmind-data` 卷
（`CLAWMIND_DIR=/data/.clawmind`），所以 `docker compose down` 重建容器后设置不会丢。

## 文档

| 文档 | 内容 |
|------|------|
| [docs/code-agent.md](docs/code-agent.md) | 定位、代码工具、仓库画像、验证门 |
| [docs/self-evolution.md](docs/self-evolution.md) | 两级递归、可编辑面、校验规则、API 与环境变量 |
| [docs/features.md](docs/features.md) | 产品特性、记忆与界面 |
| [docs/architecture.md](docs/architecture.md) | 模块划分、数据流与配置 |
| [docs/architecture-evolution.md](docs/architecture-evolution.md) | 目标架构、SSE/MCP 契约 |
| [docs/skills-roadmap.md](docs/skills-roadmap.md) | 软件工程 Skills、触发行规则、SKILL.md 兼容 |
| [docs/plugins.md](docs/plugins.md) | 插件层、seam 契约、hook 事件与否决 |

## 注意与边界

- `shell_exec` 在本机执行。`AGENT_WORKSPACE` 缩小影响范围，高危命令需人工确认，但这**不是**容器级沙箱。
- API Key 建议放在环境变量。`file_read` 拒绝读取本地配置目录，且提示词要求模型不复述密钥。
- 自进化只能写可编辑面，不能修改仓库代码。
- 插件层只能收紧安全策略：可以添加 deny / high-risk 模式、降低超时与常驻文本预算、收窄工作区根；内置地板不可移除，工作区根不可放宽。Hook 命令在插件目录内执行，未允许确认时高风险命令直接拒绝。

## 许可证

见 [LICENSE](LICENSE)。
