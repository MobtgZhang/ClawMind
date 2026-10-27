package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mobtgzhang/clawmind/backend/internal/codectx"
)

// codeRoutes exposes the code-engineering view. It is read-only: the agent changes the workspace
// through its tools, and this endpoint reports what the classifier saw. A class that changes the
// round cap, the search ceiling and the loop shape has to be inspectable, otherwise a wrong budget
// is invisible to the person whose turn it changed.
func (s *Server) codeRoutes(g *gin.RouterGroup) {
	if s.CodeCache == nil && s.CodeGraph == nil {
		return
	}
	g.GET("/code", s.getCodeWorkspace)
	g.POST("/code/refresh", s.refreshCodeWorkspace)
}

func (s *Server) getCodeWorkspace(c *gin.Context) {
	ws, ok := s.codeWorkspace(c.Query("path"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is outside the workspace root"})
		return
	}
	budget, profile, shape := s.classifyWorkspace(ws, c.Query("task"))
	graph := s.CodeGraph.Get(ws)
	parsed, reused, skipped, truncated, builtAt := graph.Stats()
	c.JSON(http.StatusOK, gin.H{
		"workspace": ws,
		"class":     budget.Class,
		"score":     budget.Score,
		"reasons":   budget.Reasons,
		"loop":      budget.Loop,
		"budget":    budget,
		"profile":   profile,
		"graph": gin.H{
			"enabled":   graph != nil,
			"shape":     shape,
			"parsed":    parsed,
			"reused":    reused,
			"skipped":   skipped,
			"dirty":     graph.Dirty(),
			"truncated": truncated,
			"builtAt":   builtAt,
		},
	})
}

// refreshCodeWorkspace rebuilds both views for one workspace. The two caches have different freshness
// windows, so a reader who wants the current state should not have to guess which one is stale.
func (s *Server) refreshCodeWorkspace(c *gin.Context) {
	ws, ok := s.codeWorkspace(c.Query("path"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is outside the workspace root"})
		return
	}
	s.CodeCache.Invalidate(ws)
	graph := s.CodeGraph.Refresh(ws)
	c.JSON(http.StatusOK, gin.H{"ok": true, "workspace": ws, "shape": graph.Shape()})
}

// codeWorkspace resolves the workspace root for a request. A query path may narrow the root but never
// widen it, which is the same rule the sandbox layer applies to the agent.
func (s *Server) codeWorkspace(want string) (string, bool) {
	base := s.defaultWorkspace()
	if strings.TrimSpace(want) == "" {
		return base, true
	}
	// A relative path is a subdirectory of the root, not a path relative to the process: the agent
	// tools read relative paths the same way, and an endpoint that resolved them differently would
	// report a shape for a directory the agent is not working in.
	if !filepath.IsAbs(want) {
		want = filepath.Join(base, want)
	}
	return narrowWorkspace(base, want)
}

// defaultWorkspace is the root a turn runs against: the environment value, narrowed by the sandbox
// layer. It is one function because the endpoint and the turn must agree on what "the workspace" is.
func (s *Server) defaultWorkspace() string {
	ws := strings.TrimSpace(os.Getenv("AGENT_WORKSPACE"))
	if ws == "" {
		if wd, err := os.Getwd(); err == nil {
			ws = wd
		}
	}
	if narrowed, applied := narrowWorkspace(ws, s.Runtime.Sandbox.Workspace); applied {
		return narrowed
	}
	return ws
}

// classifyWorkspace scores one workspace for one request without paying for a full walk: the profile
// comes from the cache and the shape from the incremental index. A classifier that re-reads the
// repository on every call is the cost it exists to avoid.
func (s *Server) classifyWorkspace(workspace, task string) (codectx.Budget, *codectx.Profile, codectx.GraphShape) {
	profile := s.repoProfile(workspace)
	graph := s.CodeGraph.Get(workspace)
	var shape codectx.GraphShape
	if graph != nil {
		shape = graph.Shape()
	}
	baseRounds, baseTokens := s.classifierBase()
	if forced := strings.ToLower(strings.TrimSpace(os.Getenv("CLAWMIND_PROJECT_CLASS"))); forced == "simple" || forced == "complex" {
		return codectx.FixedBudget(codectx.Class(forced), baseRounds, baseTokens), profile, shape
	}
	return codectx.Classify(profile, shape, task, baseRounds, baseTokens), profile, shape
}

// classifierBase returns the caps the classifier was given. Reporting a base different from the one
// the turn applies would make the reasons describe a decision the runtime never made.
func (s *Server) classifierBase() (int, int) {
	fileCfg, err := s.Cfg.Load()
	if err != nil {
		return envInt("CLAWMIND_MAX_AGENT_ROUNDS", 8), envInt("CLAWMIND_MAX_CONTEXT_TOKENS", 24000)
	}
	resolved := s.applyRuntimeLayer(fileCfg.Resolved())
	tokens := envInt("CLAWMIND_MAX_CONTEXT_TOKENS", 24000)
	if s.Runtime.Session.MaxContextTokens > 0 {
		tokens = s.Runtime.Session.MaxContextTokens
	}
	return resolved.MaxAgentRounds, tokens
}

// envInt reads an integer environment override. A value beyond a package ceiling is clamped by the
// package that owns the ceiling, not here, so one number is not trusted twice.
func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return time.Duration(n) * time.Second
}
