package agent

import "fmt"

// OfflinePlaceholderText is shown when the model API cannot be reached.
const OfflinePlaceholderText = "当前无法连接语言模型服务。请在运行后端的终端设置 OPENAI_API_KEY，并确认 OPENAI_BASE_URL 可访问。\n\n（本条为离线占位回复，不影响你继续浏览界面与删除会话。）"

func composeFailureReply(err error) string {
	if err == nil {
		return OfflinePlaceholderText
	}
	return fmt.Sprintf("%s\n\n> %s", OfflinePlaceholderText, err.Error())
}
