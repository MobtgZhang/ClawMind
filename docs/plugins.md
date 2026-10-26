# 插件层（Plugin Registry）

ClawMind 的每个部件都是一个插件：模型适配、工具注册表、会话存储、记忆存储、agent 循环、提示构造、
中间件。没有可被补丁的特权核心——行为靠把插件挂载到其它层旁边产生，每条注册都是一个在卸载时撤销的
effect。

## 组成模型

- 插件目录：`CLAWMIND_PLUGIN_DIR`（默认 `.clawmind/plugins/<id>/plugin.json`）。
- 按目录名排序依次挂载；**层顺序决定谁生效**：同一个 key 上，后挂载的层替换先挂载的整条注册，卸载后
  恢复被覆盖的那条。
- 校验不通过的插件被跳过并报告，不会被挂载；`GET /api/plugins` 仍列出它和它的问题，因为「声明了行为
  却从不执行」正是这套设计要暴露的失败模式。

## plugin.json 契约

```json
{
  "id": "verify-before-done",
  "name": "Verify before completion",
  "version": "1.0.0",
  "description": "报告完成前必须收集测试与 lint 证据",
  "seams": ["hooks", "prompt"],
  "skills": ["skills/verify-before-completion/SKILL.md"],
  "subagents": ["sub_agents/reviewer/AGENT.md"],
  "hooks": {
    "Stop": [{ "type": "command", "command": "python3 scripts/preflight.py" }]
  },
  "patch": [
    { "id": "budget", "seam": "memory", "config": { "maxItems": 16, "charBudget": 1500 } }
  ],
  "enabled": true
}
```

校验规则（`internal/plugin/manifest.go`）：

1. `id` 必须是小写 slug；至少要声明一个 seam。
2. seam 必须在已知集合内：`llm`、`tools`、`memory`、`session`、`sandbox`、`prompt`、`hooks`。
3. 工具条目必须是 `type: function` 且有 `name` / `description` / `parameters`；同名重复即失败。
4. hook 事件必须在 dispatcher 实际触发的事件集合内，且 `type` 必须是 `command`、`command` 非空。
5. `skills` / `subagents` 路径不能逃出插件目录；组件必须有 frontmatter `description`，`AGENT.md` 必须有
   `mode: subagent`——没有 description 的组件永远不会被检索。
6. patch 的每个 key 必须是运行时真正读取的 key（`ConsumedKeys`）。声明了运行时不读的 key 会被拒绝，
   而不是被静默忽略。

## 运行时读取的 key

| seam | key | 作用 |
|------|-----|------|
| `llm` | `model` `baseUrl` `apiKey` `temperature` `topP` `topK` `maxAgentRounds` | 覆盖 `config.json` 与环境变量 |
| `memory` | `backend` `embeddingModel` `maxItems` `charBudget` `timeoutMs` `caps` `distillAsync` | 检索预算与分层上限 |
| `prompt` | `systemPrompt` `promptsDir` `alwaysOnMax` | 系统提示与常驻文本预算 |
| `sandbox` | `workspace` `deniedPatterns` `highRiskPatterns` `shellTimeoutMs` `allowApproval` | 安全策略 |
| `session` | `maxContextTokens` `tokenBudget` | 历史裁剪与单次回复上限 |
| `tools` | `toolsPath` `skillsPath` | 工具定义来源 |
| `hooks` | `allowShell` `maxOutputChars` `timeoutMs` | hook 执行边界 |

解析入口是 `plugin.Resolve(reg)`，在启动时执行一次；`Server.Runtime` 保存结果，请求路径按层覆盖文件与
环境配置。

**只能收紧，不能放松**：内置安全模式是地板，插件层只能添加 `deniedPatterns` / `highRiskPatterns`；
`shellTimeoutMs` 与 `alwaysOnMax` 只能降低；`workspace` 只能收窄——超出进程已有根路径的请求被拒绝而不是
被信任。

**嵌套 key 也要能被读取**：`caps` 只接受 `l0`–`l3`（大小写不敏感，因为文档与 census 用 L0–L3 标注层），
写成 `l9` 在挂载时被报告，而不是在启动时被静默丢弃。记忆层的 cap 不只是被报告：存储层裁剪时实际使用它，
内存后端同样如此。

## 生效值与来源

`GET /api/settings` 返回文件配置，同时返回 `effective`（运行时真正使用的值）和 `source`（每个被覆盖的 key
属于哪个插件）。设置页写入的是文件，而插件层赢的时候文件值不会生效——只返回文件配置会让一个保存值看起来
已经生效。设置页把被覆盖的 key 单独列出，不把它们混进输入框。

## Hooks

事件集合（`internal/harness.HookEvents`）：`SessionStart`、`UserPromptSubmit`、`PreToolUse`、
`PostToolUse`、`PostCompact`、`Stop`、`SubagentStop`。

- `matcher` 为空或 `*` 匹配所有工具，否则是精确名或正则。
- 同一事件的多个 hook 累加，不互相替换。
- 退出码 `2` 是文档化的**否决（veto）**：调用方停止这次工具调用或本轮。其它非零退出码记录为 hook 失败，
  但不阻塞——一个坏掉的 hook 脚本不应该把 agent 一起拖垮。
- hook 的 stdout 作为指导回灌进工具结果。
- 声明但被跳过（`allowShell: false`、非 command 类型）会在 trace 里记为 `skipped`，而不是消失。
- 环境变量 `CLAWMIND_HOOK_EVENT` 与 `CLAWMIND_HOOK_JSON` 把事件与 payload 交给 hook。

## 与 SKILL.md 生态的兼容

`plugin.ImportSkillDir(dir)` 读取社区布局（Claude / OpenClaw 的 `skills/<name>/SKILL.md`）并映射到可编辑
面：frontmatter 的 `name` / `description` / `mode` 解析成组件，脚本按 snake_case 保留，不在面上的文件被
跳过并记录。`POST /api/skills/import-md` 接受 multipart 或 `{projectId, content}`，写入前跑完整
`Validate()`，缺触发行的技能由 `AutoTrigger()` 补齐——没有被常驻文本点名的技能永远不会被打开。

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/api/plugins` | 每个插件目录：声明了什么、贡献了什么、哪个层赢了、以及问题 |
| `POST` | `/api/plugins` | 写入一个插件目录（校验通过才挂载） |
| `POST` | `/api/plugins/:id/enable` | 挂载 |
| `POST` | `/api/plugins/:id/disable` | 卸载并写回 `enabled: false` |
| `GET` | `/api/plugins/config` | 按层解析后的配置（这是看到哪层真正生效的唯一方式） |
| `GET` | `/api/settings` | 文件配置 + `effective`（运行时真正使用的值）+ `source`（哪个插件赢了该 key） |
| `GET` | `/api/memory/layers` | 分层 census：每层的条数对照**运行时实际执行的** cap |

## 运行

```bash
mkdir -p .clawmind/plugins/verify-before-done
cat > .clawmind/plugins/verify-before-done/plugin.json <<'JSON'
{ "id": "verify-before-done", "seams": ["hooks"], "enabled": true,
  "hooks": { "Stop": [{ "type": "command", "command": "python3 scripts/preflight.py" }] } }
JSON
```

启动时 `Registry.Load()` 会报告被跳过的插件；`delegate` 工具与 `load_skill` 读取的是解析后的有效集合，
所以插件贡献的 skill 与 subagent 和 harness 种子在同一面上。
