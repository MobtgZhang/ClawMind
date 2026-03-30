package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mobtgzhang/clawmind/backend/internal/clawmindcfg"
)

func (s *Server) getSettings(c *gin.Context) {
	cfg, err := s.Cfg.Load()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cfg)
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
