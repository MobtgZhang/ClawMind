# 代码项目助手（定位收窄）

ClawMind 不再定位为通用「做任务」的助手，而是**面向真实代码仓库的 agent**：在一个项目里定位、修改、验证代码，并把可迁移的经验沉淀到项目级 harness。

## 工作流

固定四步，写进系统提示（`backend/internal/agent/prompts.go` 与 `backend/data/prompts/`）：

1. **先定位再改动**：用 `code_search` / `repo_tree` 找到相关符号与调用点，读完上下文再编辑，不凭记忆假设文件内容。
2. **改动**：用 `file_edit` 做精确替换；`old` 不唯一或缺失时直接失败，迫使模型重新读取而不是猜测。
3. **验证**：改动后立刻跑推断出的测试命令（`run_tests`）。一次成功的写调用不等于改动可用。
4. **收尾**：交付物的形状属于用户；经验库是数据，不是指令。

## 代码工具

`backend/internal/tools/code.go` 定义，由 `backend/internal/agent/code_tools.go` 执行，与内置原子工具合并进 function calling：

| 工具 | 作用 | 边界 |
|------|------|------|
| `code_search` | 字面量或正则搜索，返回 `file:line` | 结果条数上限（默认 60），跳过依赖与构建目录 |
| `repo_tree` | 列出仓库结构 | 深度默认 3、条目默认 240 |
| `file_edit` | 精确字符串替换 | `old` 缺失或歧义即失败 |
| `run_tests` | 执行推断出的验证命令 | 受 `AGENT_WORKSPACE` 与高危 shell 确认约束 |
| `load_skill` | 按需读取 harness 技能 | 只读可编辑面，常驻文本之外零成本 |

## 仓库画像（`internal/codectx`）

`codectx.Detect(root)` 做有边界的快照，**不会 shell out**：语言分布、manifest 文件、推断的 test/build/lint/format 命令、目录树（上限 240 条）、git 分支（读 `.git/HEAD`）。

- 忽略目录：`node_modules`、`.git`、`dist`、`build`、`vendor`、`.venv`、`__pycache__` 等（`codectx.IgnoredDirs`）。
- 画像注入系统提示，`run_tests` 使用推断命令；`CLAWMIND_CODE_PROFILE=0` 可关闭。

## 自动验证门（`internal/agent/verify.go`）

某一轮发生文件改动后，自动跑推断的测试命令，把输出作为 tool 事件回灌进对话，并把失败结果作为下一轮的输入。这样「我修好了」必须被仓库证明。`CLAWMIND_AUTO_VERIFY=0` 可关闭。

## 边界

- `AGENT_WORKSPACE` 限定文件与 shell 的根路径。
- 高危 shell 命令经 SSE 请求人工确认后才执行；这不是容器级沙箱。
- `web_fetch` 做 SSRF 过滤。
- 自进化只能写 harness 可编辑面，不能写仓库代码（见 [自进化](self-evolution.md)）。
