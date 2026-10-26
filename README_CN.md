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
- **有边界的仓库感知**：语言分布、manifest、推断的 `test`/`build`/`lint`/`format` 命令、有上限的目录树，且不 shell out。
- **可编辑面**：`AGENTS.md`、`tool_guidance.md`、`lessons.md`、`skills/`、`scripts/`。常驻文本最贵，所以知识优先放进按需技能与脚本。
- **受证据约束的进化**：Propose → Probe → Compose → 盲选 → Reflect，逐条改动给出结论（`helped` / `hurt` / `no observed effect` / `confounded`）。保留基线是合法结论。
- **可迁移经验库**：按项目存 `cue` → `lesson`，要求泛化而非记忆，且当数据看待、不当指令执行。
- **流式界面**：ChatGPT 风格主列、可折叠的任务流程与思考、工具步骤、GFM 表格、代码块复制、KaTeX。
- **多级记忆 L0–L3**：会话 / 项目 / 全局分层，默认写入 SQLite，可选语义检索。
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

然后在侧栏底部打开 **Harness** 面板。

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

自定义技能可在侧栏导入（OpenAI tools JSON）或新建，与 `TOOLS_PATH` 指向的 JSON 及内置工具合并。

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
| `CLAWMIND_EVOLUTION` | `false` | 启用自进化引擎 |
| `CLAWMIND_EVOLUTION_K` | `3` | 候选数 / 串行轮数（1–8） |
| `CLAWMIND_EVOLUTION_MODE` | `parallel` | `parallel` · `sequential` · `dry` |
| `CLAWMIND_HARNESS_DIR` | `.clawmind/harness` | harness 根目录，按项目分子目录 |
| `CLAWMIND_MEMORY_BACKEND` | `sqlite` | `memory` 为进程内（测试用） |
| `CLAWMIND_EMBEDDING_MODEL` | — | 非空则启用语义检索 |
| `CLAWMIND_MEMORY_SEMANTIC_TOP_K` | `8` | 检索条数 |
| `CLAWMIND_MAX_CONTEXT_TOKENS` | `24000` | 历史 token 上限 |
| `CLAWMIND_TOKEN_BUDGET` | `0` | 单次回复软上限 |
| `CLAWMIND_MCP_COMMAND` / `_ARGS` / `_ENV` | — | MCP stdio 服务，空则禁用 |

## API

- `GET /api/health`
- `GET/PUT /api/settings` — `config.json`
- `GET/POST /api/skills`、`POST /api/skills/import`
- `GET/POST /api/projects`、`PATCH/DELETE /api/projects/:id`
- `GET/POST /api/sessions`、`PATCH/DELETE /api/sessions/:id`
- `GET/POST /api/sessions/:id/messages`、`POST .../messages/:mid/regenerate`、`POST .../messages/:mid/cancel`
- `GET /api/sessions/:id/stream` — SSE（`part_start` / `delta` / `part_end` / `tool_call` / `tool_result` / `tool_approval_request` / `done` / `error`）
- `POST /api/sessions/:id/tool-approval`
- Harness：`GET /api/harness`、`GET/POST /api/harness/file`、`POST /api/harness/round`、`GET /api/harness/rounds`、`POST /api/harness/adopt`、`POST /api/harness/lessons`、`GET /api/harness/library`

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

## 文档

| 文档 | 内容 |
|------|------|
| [docs/code-agent.md](docs/code-agent.md) | 定位、代码工具、仓库画像、验证门 |
| [docs/self-evolution.md](docs/self-evolution.md) | 两级递归、可编辑面、校验规则、API 与环境变量 |
| [docs/features.md](docs/features.md) | 产品特性、记忆与界面 |
| [docs/architecture.md](docs/architecture.md) | 模块划分、数据流与配置 |
| [docs/architecture-evolution.md](docs/architecture-evolution.md) | 目标架构、SSE/MCP 契约 |
| [docs/skills-roadmap.md](docs/skills-roadmap.md) | 技能路线图 |

## 注意与边界

- `shell_exec` 在本机执行。`AGENT_WORKSPACE` 缩小影响范围，高危命令需人工确认，但这**不是**容器级沙箱。
- API Key 建议放在环境变量。`file_read` 拒绝读取本地配置目录，且提示词要求模型不复述密钥。
- 自进化只能写可编辑面，不能修改仓库代码。

## 许可证

见 [LICENSE](LICENSE)。
