package agent

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// systemRuntimeContextBlock is prepended into the system prompt (after user-configured SystemPrompt).
// It states product identity, current server-local time, and cached host OS info.
func systemRuntimeContextBlock(now time.Time) string {
	locName := now.Location().String()
	if locName == "" {
		locName = "Local"
	}
	timeLine := fmt.Sprintf("%s（时区：%s，RFC3339：%s）",
		now.Format("2006-01-02 15:04:05 MST"),
		locName,
		now.Format(time.RFC3339),
	)
	host := cachedHostEnvironment()
	if host == "" {
		host = goosDisplayName(runtime.GOOS) + ", " + runtime.GOARCH
	}

	var b strings.Builder
	b.WriteString("## ClawMind 运行上下文\n\n")
	b.WriteString("- **助手身份**：你是 **ClawMind** 内置的 AI 助手，在 ClawMind 提供的对话与工具环境中为用户服务。向用户介绍自己时请明确与 ClawMind 的归属关系；不要冒用其他无关商业产品身份，除非用户明确要求对比或讨论第三方。\n")
	b.WriteString("- **当前日期与时间**（会话由本机后端处理，以下为服务器本地时钟）：")
	b.WriteString(timeLine)
	b.WriteString("\n")
	b.WriteString("- **服务器运行环境**：")
	b.WriteString(host)
	b.WriteString("\n")
	return b.String()
}
