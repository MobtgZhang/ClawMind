package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mobtgzhang/clawmind/backend/internal/clawmindcfg"
	"github.com/mobtgzhang/clawmind/backend/internal/plugin"
)

// settingsResponse is the file config plus the values the runtime actually uses. The settings page
// writes the file, but a plugin layer can override a key, so reporting only the file makes a saved
// value look effective when it is not.
type settingsResponse struct {
	clawmindcfg.Config
	Effective map[string]any    `json:"effective"`
	Source    map[string]string `json:"source,omitempty"`
}

// settingsKeyNames maps patch keys to the names the settings page uses, so the source map can be read
// next to the effective values instead of needing a second vocabulary.
var settingsKeyNames = map[string]string{
	"model":          "openaiModel",
	"baseUrl":        "openaiBaseUrl",
	"apiKey":         "openaiApiKey",
	"temperature":    "temperature",
	"topP":           "topP",
	"topK":           "topK",
	"maxAgentRounds": "maxAgentRounds",
	"systemPrompt":   "systemPrompt",
}

func (s *Server) getSettings(c *gin.Context) {
	cfg, err := s.Cfg.Load()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	eff := s.applyRuntimeLayer(cfg.Resolved())
	resp := settingsResponse{Config: cfg, Effective: map[string]any{
		"openaiBaseUrl":  eff.BaseURL,
		"openaiApiKey":   eff.APIKey,
		"openaiModel":    eff.Model,
		"systemPrompt":   eff.SystemPrompt,
		"temperature":    eff.Temperature,
		"topP":           eff.TopP,
		"topK":           eff.TopK,
		"maxAgentRounds": eff.MaxAgentRounds,
	}, Source: s.settingsSource()}
	c.JSON(http.StatusOK, resp)
}

// settingsSource names the plugin that wins each key. A key with no entry comes from the file or the
// environment, which is the only thing the settings page is allowed to change.
func (s *Server) settingsSource() map[string]string {
	if s.Plugins == nil {
		return nil
	}
	out := map[string]string{}
	for _, seam := range []plugin.Seam{plugin.SeamLLM, plugin.SeamPrompt} {
		for key, id := range s.Plugins.Provenance(seam) {
			if name, ok := settingsKeyNames[key]; ok {
				out[name] = id
			}
		}
	}
	return out
}

func (s *Server) putSettings(c *gin.Context) {
	existing, err := s.Cfg.Load()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var body clawmindcfg.Config
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 设置页不再提交 systemPrompt 时，避免用空字符串覆盖配置文件中的值
	if strings.TrimSpace(body.SystemPrompt) == "" {
		body.SystemPrompt = existing.SystemPrompt
	}
	if err := s.Cfg.Save(body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
