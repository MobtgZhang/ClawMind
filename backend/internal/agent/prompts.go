package agent

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	promptFileSystemSuffix = "system_suffix.md"
	promptFileTaskFlow     = "task_flow_user.md"
	promptFileThinking     = "thinking_user.md"
)

const (
	defaultSystemSuffix = `在以上 **ClawMind 运行上下文**（身份、当前时间、服务器环境）前提下：你是带工具的智能助手，在确有必要时使用工具完成检索、执行或校验等操作。向用户输出时，使用与用户**最近一条消息**相同的语言（若同轮混用多种语言，则以当前问题主体所用语言为准）；表达清楚、结构合理，并直接对准用户诉求。`

	defaultTaskFlowUser = `请用 Markdown 写一段**任务拆解与计划**（本阶段**不要**调用工具，也**不要**写出给用户的最终答案）。

**篇幅**：按问题复杂度伸缩——简单需求可短，复杂、多目标或多步骤任务请写充分，不要为「条数少」而省略关键信息。

**建议结构**（按需取舍，可增删标题与小节）：

- **目标与完成标准**：用户要什么；怎样算做完、可验收
- **已知信息**：对话与上下文中已明确的事实
- **假设与待澄清**：不得不做的合理假设；需要用户补充的点（若有）
- **约束与风险**：时间、技术、合规、依赖等限制；主要风险
- **执行计划**：分阶段/分步骤做什么、先后顺序与理由
- **工具与外部资源**（仅规划）：是否预计使用工具、大致用途（**不要在本阶段执行**）

输出语言请与用户最新输入保持一致。

用户最新输入：

` + LastUserPlaceholder

	defaultThinkingUser = `请用 Markdown 写你的**推理与权衡过程**（本阶段**不要**写出给用户的最终答案，也**不要**粘贴或逐字复述工具返回的大段原文；如必须引用，用一两句概括要点即可）。

**篇幅**：随难度变化——简单推理可以紧凑；涉及多方案比较、多约束折中、长链条推导或需要自洽检查时，请写完整，**不要用固定句数限制自己**。

**可包含**（择要，不必面面俱到）：问题类型判断、候选思路及取舍理由、关键依据与薄弱环节、仍需验证的假设、下一步依赖的信息或动作等。

输出语言请与用户最新输入保持一致。

用户最新输入：

` + LastUserPlaceholder
)

// LastUserPlaceholder is replaced in task_flow_user.md / thinking_user.md templates.
const LastUserPlaceholder = "{{LAST_USER}}"

type promptBundle struct {
	SystemSuffix string
	TaskFlowUser string
	ThinkingUser string
}

func resolvePromptsDir(explicit string) string {
	d := strings.TrimSpace(explicit)
	if d != "" {
		return filepath.Clean(d)
	}
	wd, err := os.Getwd()
	if err != nil {
		return filepath.Clean(filepath.Join(".", "data", "prompts"))
	}
	// 与 DB 同目录：dev-backend 时 cwd 为 backend；在仓库根执行 go run 时 cwd 为根目录
	candidates := []string{
		filepath.Join(wd, "data", "prompts"),
		filepath.Join(wd, "backend", "data", "prompts"),
	}
	for _, c := range candidates {
		c = filepath.Clean(c)
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return filepath.Clean(filepath.Join(wd, "data", "prompts"))
}

func readPromptFile(dir, name, fallback string) string {
	path := filepath.Join(dir, name)
	b, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return fallback
	}
	return s
}

func loadPromptBundle(promptsDir string) promptBundle {
	dir := resolvePromptsDir(promptsDir)
	return promptBundle{
		SystemSuffix: readPromptFile(dir, promptFileSystemSuffix, defaultSystemSuffix),
		TaskFlowUser: readPromptFile(dir, promptFileTaskFlow, defaultTaskFlowUser),
		ThinkingUser: readPromptFile(dir, promptFileThinking, defaultThinkingUser),
	}
}

func expandLastUser(tpl, lastUser string) string {
	return strings.ReplaceAll(tpl, LastUserPlaceholder, lastUser)
}
