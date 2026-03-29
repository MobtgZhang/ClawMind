package api

import (
	"net/http"

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
	var body clawmindcfg.Config
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.Cfg.Save(body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
