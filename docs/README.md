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

## 品牌资源

位于 [assets/](assets/)：

- `logo.svg` — 项目 Logo（SVG）
- `clawmind-assistant.png` — AI 助手头像
- `clawmind-user.png` — 用户头像

前端构建时从 `frontend/public/` 引用同名文件（与 `assets` 中 PNG 保持一致）。
