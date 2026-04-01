# Skills 与 OpenClaw 生态差异及路线图

## 当前实现（ClawMind）

- 技能为 **OpenAI Chat Completions 的 `tools` JSON** 形状（`type: function`、`function.name` / `description` / `parameters`）。
- 来源：侧栏导入、新建技能，合并写入 `.clawmind/skills.json`；可与环境变量 `TOOLS_PATH` 指向的 JSON 及内置原子工具合并。
- 运行时由后端 `internal/tools` 与 `internal/api/skills.go` 加载并注入 Agent。

## OpenClaw 生态（对照）

- 常见模式为基于 **`SKILL.md`** 的技能描述与目录约定，配合注册中心（如 ClawHub）发现、拉取技能。
- 工具与提示的组织方式与「单文件 JSON 列表」不同，无法直接互拷。

## 路线图（建议分阶段）

1. **文档与导出**：在 UI 或文档中提供「从 SKILL.md 手工迁移」的检查清单（名称、描述、参数 schema 映射）。
2. **可选解析器**：在后端或 CLI 增加对极简 `SKILL.md`  frontmatter 的解析，生成内部 tools JSON（不承诺覆盖全部 OpenClaw 变体）。
3. **远程注册（远期）**：若需对齐社区，再评估 HTTP 拉取与版本锁定，避免引入未审计代码执行面。

实现优先级低于安全与记忆正确性；以本文件为契约，避免与 [architecture-target.md](architecture-target.md) 重复展开实现细节。
