# ClawMind 特点与能力

## 产品定位

ClawMind 是面向开发者的 **本地优先** AI 对话客户端：单仓库内 **Go 后端** + **Vue 3 前端**，支持流式回复、会话与项目管理，以及可扩展的 **工具型 Agent**。

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

### 自进化 Agent 与工具

- **非流式工具轮**：在流式最终回答前，可多次调用模型进行 **function calling**，执行后再继续。
- **内置原子工具**：`file_read`、`file_write`、`shell_exec`（Windows / macOS / Linux 差异由后端处理）、`web_fetch`、`task_plan`、`task_summary`。
- **工作目录**：`AGENT_WORKSPACE` 限制文件与 Shell 的相对路径根，降低误操作范围。
- **高危 Shell**：匹配启发式危险模式时，经 SSE 推送确认请求，用户通过 REST 批准或拒绝后再执行（非沙箱，仍需谨慎）。
- **web_fetch**：对目标 URL 做 SSRF 过滤，禁止访问内网与链路本地等地址。
- **技能扩展**：侧栏 **导入** JSON（OpenAI tools 形状）或 **新建** 技能，合并写入 `.clawmind/skills.json`，并与 `TOOLS_PATH` 指向的 JSON 及内置工具合并。

### 多级记忆（L0–L3）

- **L0**：当前会话消息（对话本身）。
- **L1 / L2 / L3**：默认写入 **SQLite**（与主库同文件），会话 / 项目 / 全局分层摘要注入系统提示，**重启后保留**。若设置 `CLAWMIND_MEMORY_BACKEND=memory`，则为进程内存储，重启丢失（多用于测试）。

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
