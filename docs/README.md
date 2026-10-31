# ClawMind 文档

本目录收录项目说明，与仓库根目录 [README.md](../README.md)（英文）或 [README_CN.md](../README_CN.md)（中文）配合阅读。

| 文档 | 说明 |
|------|------|
| [代码项目助手](code-agent.md) | 定位收窄：代码工具、仓库画像、改后必验证 |
| [自进化](self-evolution.md) | Harness 两级递归、可编辑面、校验规则、API 与环境变量 |
| [特点与能力](features.md) | 产品特性、Agent、工具、记忆与界面 |
| [架构说明](architecture.md) | 技术栈、模块划分、数据流与配置 |
| [演进架构与路线图](architecture-evolution.md) | 相对 content1 的差距、目标架构、SSE/MCP 契约与环境变量 |
| [目标架构与安全边界](architecture-target.md) | 安全边界、数据层、LLM 适配、SSE 生命周期 |
| [Skills 路线图](skills-roadmap.md) | 软件工程 Skills、触发行规则、工作流链、SKILL.md 兼容 |
| [插件层](plugins.md) | 层式注册、seam 契约、hook 事件与否决、运行时读取的 key |

## 品牌与界面资源

位于 [assets/](assets/) 与 `frontend/public/`：

- `logo.svg` — 项目 Logo（SVG）
- `sprite.svg` — 图标 sprite（Lucide v1.50.0，ISC 许可，见 `frontend/public/LICENSE.lucide.txt`）
- `fonts/` — Outfit 可变字体（SIL OFL 1.1，见 `frontend/public/fonts/OFL.txt`），CJK 回退系统字体

界面视觉语言（中性灰 + 单一紫罗兰 accent、玻璃拟态侧栏、Outfit 字体、恒暗代码块）由
`frontend/src/style.css` 的设计令牌驱动；深色为默认主题，首帧前由 `index.html` 的内联脚本
按 `localStorage["clawmind_ui"]` 设置，避免闪烁。

### 前端主要入口

- **侧栏**：新对话、搜索聊天、设置、插件设置、自进化 Harness、导入技能、代码工作区
- **聊天页左上角**：当前模型配置（模型名 + 采样档：精确 / 平衡 / 创意），点击展开模型卡
- **聊天页右上角**：图循环入口，按工作区形状显示「图循环」或「单遍」
- **代码工作区**（`CodePanel.vue`）：仓库画像、复杂度分级、符号图统计、预算、推断的验证命令、
  语言分布、目录树；数据来自 `GET /api/code`（`backend/internal/api/code.go`），前端只读
- **设置页**：左侧分区导航（模型与采样设置 / Agent 行为 / 外观），底部统一保存或取消
- **搜索聊天**：无后端端点，按已加载会话的标题与项目名做前端过滤
