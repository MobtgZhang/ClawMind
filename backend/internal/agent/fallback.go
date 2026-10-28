package agent

import "fmt"

// OfflinePlaceholderText is shown when the model API cannot be reached. The model endpoint is
// configured in the settings page, so the message points there first; the environment variables are
// the fallback for a deployment that has no UI.
const OfflinePlaceholderText = "当前无法连接语言模型服务。请在「设置」页确认 API Base URL、模型名称与 API Key，并确认该地址可访问；未配置时也可在后端环境变量中设置 OPENAI_API_KEY / OPENAI_BASE_URL。\n\n（本条为离线占位回复，不影响你继续浏览界面与删除会话。）"

func composeFailureReply(err error) string {
	if err == nil {
		return OfflinePlaceholderText
	}
	return fmt.Sprintf("%s\n\n> %s", OfflinePlaceholderText, err.Error())
}
