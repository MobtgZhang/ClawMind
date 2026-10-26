package api

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/plugin"
	"github.com/mobtgzhang/clawmind/backend/internal/tools"
)

// pluginView is one plugin as the registry actually resolves it: what it declares, what it
// contributed, and which layer won for each key.
type pluginView struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Description string         `json:"description"`
	Seams       []string       `json:"seams"`
	Enabled     bool           `json:"enabled"`
	Layer       int            `json:"layer"`
	Tools       []string       `json:"tools"`
	Skills      []string       `json:"skills"`
	Subagents   []string       `json:"subagents"`
	Hooks       map[string]int `json:"hooks"`
	Patch       []string       `json:"patch"`
	Dir         string         `json:"dir"`
	Problems    []string       `json:"problems,omitempty"`
}

// listPlugins returns every plugin directory, mounted or not. A plugin that fails validation is
// listed with its problems instead of being silently absent: a plugin that declares behaviour and
// never runs it is exactly the failure mode this design is meant to make visible.
func (s *Server) listPlugins(c *gin.Context) {
	if s.Plugins == nil {
		c.JSON(http.StatusOK, []pluginView{})
		return
	}
	entries, err := filepath.Glob(filepath.Join(s.Plugins.Home, "*"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var out []pluginView
	for _, entry := range entries {
		m, err := plugin.LoadManifest(entry)
		if err != nil {
			continue
		}
		view := pluginView{
			ID: m.ID, Name: m.Name, Version: m.Version, Description: m.Description,
			Enabled: m.Enabled, Dir: m.Dir,
		}
		for _, seam := range m.Seams {
			view.Seams = append(view.Seams, string(seam))
		}
		sort.Strings(view.Seams)
		for _, t := range m.Tools {
			view.Tools = append(view.Tools, t.Function.Name)
		}
		view.Skills = append(view.Skills, m.Skills...)
		view.Subagents = append(view.Subagents, m.Subagents...)
		view.Hooks = map[string]int{}
		for event, hooks := range m.Hooks {
			view.Hooks[event] = len(hooks)
		}
		for _, row := range m.Patch {
			view.Patch = append(view.Patch, string(row.Seam)+":"+row.ID)
		}
		view.Problems = m.Validate()
		view.Layer = s.Plugins.LayerIndex(m.ID)
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	c.JSON(http.StatusOK, out)
}

type installPluginReq struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Version     string                   `json:"version"`
	Description string                   `json:"description"`
	Seams       []string                 `json:"seams"`
	Tools       []tools.Definition       `json:"tools"`
	Skills      []string                 `json:"skills"`
	Subagents   []string                 `json:"subagents"`
	Hooks       map[string][]plugin.Hook `json:"hooks"`
	Patch       []plugin.PatchRow        `json:"patch"`
	Enabled     *bool                    `json:"enabled"`
	// Files writes component files inside the plugin directory so a declared skill or subagent is
	// readable at mount time instead of being a path that resolves to nothing.
	Files map[string]string `json:"files"`
}

// installPlugin writes plugin.json under the plugin home and mounts it when it validates. A plugin
// that fails validation is written but not mounted: the manifest is data the operator can fix, and
// mounting a broken plugin would make the failure invisible.
func (s *Server) installPlugin(c *gin.Context) {
	if s.Plugins == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plugin registry is not configured"})
		return
	}
	var req installPluginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id required"})
		return
	}
	m := &plugin.Manifest{
		ID: id, Name: strings.TrimSpace(req.Name), Version: strings.TrimSpace(req.Version),
		Description: strings.TrimSpace(req.Description), Enabled: req.Enabled == nil || *req.Enabled,
		Tools: req.Tools, Skills: req.Skills, Subagents: req.Subagents, Hooks: req.Hooks, Patch: req.Patch,
	}
	for _, seam := range req.Seams {
		m.Seams = append(m.Seams, plugin.Seam(strings.TrimSpace(seam)))
	}
	if len(m.Seams) == 0 {
		m.Seams = []plugin.Seam{plugin.SeamTools}
	}
	if problems := m.Validate(); len(problems) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": strings.Join(problems, "; "), "problems": problems})
		return
	}
	m.Dir = filepath.Join(s.Plugins.Home, id)
	if err := m.SaveManifest(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for rel, content := range req.Files {
		rel, err := harness.NormalizeRel(rel)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		full := filepath.Join(m.Dir, filepath.FromSlash(rel))
		if !strings.HasPrefix(full, m.Dir+string(os.PathSeparator)) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file escapes the plugin directory: " + rel})
			return
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if m.Enabled {
		if err := s.Plugins.Mount(m); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": true, "id": m.ID, "warning": err.Error()})
			return
		}
	}
	c.JSON(http.StatusCreated, gin.H{"ok": true, "id": m.ID, "enabled": m.Enabled, "dir": m.Dir})
}

func (s *Server) enablePlugin(c *gin.Context) {
	if s.Plugins == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plugin registry is not configured"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	m, err := plugin.LoadManifest(filepath.Join(s.Plugins.Home, id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if problems := m.Validate(); len(problems) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": strings.Join(problems, "; ")})
		return
	}
	m.Enabled = true
	if err := m.SaveManifest(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := s.Plugins.Mount(m); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": m.ID})
}

func (s *Server) disablePlugin(c *gin.Context) {
	if s.Plugins == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plugin registry is not configured"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if err := s.Plugins.Unmount(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if m, err := plugin.LoadManifest(filepath.Join(s.Plugins.Home, id)); err == nil {
		m.Enabled = false
		_ = m.SaveManifest()
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": id})
}

// getPluginConfig resolves the patch layers against an empty entry list, which is the order the
// runtime uses. Reading the resolved config is the only way to see which layer actually wins.
func (s *Server) getPluginConfig(c *gin.Context) {
	if s.Plugins == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "plugin registry is not configured"})
		return
	}
	resolved := s.Plugins.Config()
	out := map[string]any{}
	for _, seam := range plugin.Seams() {
		out[string(seam)] = resolved[seam]
	}
	c.JSON(http.StatusOK, gin.H{"home": s.Plugins.Home, "layers": len(s.Plugins.Layers()), "config": out})
}
