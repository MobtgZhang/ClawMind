package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mobtgzhang/clawmind/backend/internal/tools"
)

var errSkillExists = errors.New("skill name already exists")

type skillItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) listSkills(c *gin.Context) {
	var out []skillItem
	for _, t := range s.toolDefinitions() {
		out = append(out, skillItem{
			Name:        t.Function.Name,
			Description: t.Function.Description,
		})
	}
	c.JSON(http.StatusOK, out)
}

type createSkillReq struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

func (s *Server) createSkill(c *gin.Context) {
	var req createSkillReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}
	desc := strings.TrimSpace(req.Description)
	params := req.Parameters
	if params == nil {
		params = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	def := tools.Definition{
		Type: "function",
		Function: tools.FunctionSpec{
			Name:        name,
			Description: desc,
			Parameters:  params,
		},
	}
	if err := s.appendUserSkill(def); err != nil {
		if errors.Is(err, errSkillExists) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ok": true})
}

func (s *Server) importSkills(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}
	src, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer src.Close()
	var incoming tools.Registry
	if err := json.NewDecoder(src).Decode(&incoming); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json: " + err.Error()})
		return
	}
	existing, _ := tools.Load(s.SkillsPath)
	seen := map[string]struct{}{}
	for _, t := range existing.Tools {
		seen[t.Function.Name] = struct{}{}
	}
	for _, t := range incoming.Tools {
		if t.Function.Name == "" {
			continue
		}
		if _, ok := seen[t.Function.Name]; ok {
			continue
		}
		seen[t.Function.Name] = struct{}{}
		existing.Tools = append(existing.Tools, t)
	}
	if err := s.saveSkillsFile(existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "count": len(existing.Tools)})
}

func (s *Server) appendUserSkill(def tools.Definition) error {
	reg, _ := tools.Load(s.SkillsPath)
	for _, t := range reg.Tools {
		if t.Function.Name == def.Function.Name {
			return errSkillExists
		}
	}
	reg.Tools = append(reg.Tools, def)
	return s.saveSkillsFile(reg)
}

func (s *Server) saveSkillsFile(reg *tools.Registry) error {
	if s.SkillsPath == "" {
		return os.ErrInvalid
	}
	dir := filepath.Dir(s.SkillsPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.SkillsPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.SkillsPath)
}
