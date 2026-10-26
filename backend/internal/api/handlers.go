package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mobtgzhang/clawmind/backend/internal/agent"
	"github.com/mobtgzhang/clawmind/backend/internal/clawmindcfg"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/evolution"
	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
	"github.com/mobtgzhang/clawmind/backend/internal/mcpclient"
	"github.com/mobtgzhang/clawmind/backend/internal/memory"
	"github.com/mobtgzhang/clawmind/backend/internal/plugin"
	"github.com/mobtgzhang/clawmind/backend/internal/security"
	"github.com/mobtgzhang/clawmind/backend/internal/store"
	"github.com/mobtgzhang/clawmind/backend/internal/thread"
	"github.com/mobtgzhang/clawmind/backend/internal/tools"
)

// Server wires HTTP routes to store and LLM.
type Server struct {
	Store      *store.Store
	Cfg        *clawmindcfg.Manager
	LLM        *llm.Client
	Mem        memory.Store
	MCP        *mcpclient.Session
	ToolsPath  string // e.g. ./config/tools.json
	SkillsPath string // e.g. .clawmind/skills.json
	// HarnessRoot is the per-scope harness directory, so the editable surface exists even when the
	// evolution engine is off.
	HarnessRoot string
	// Harness is the self-evolution engine; nil disables the evolution endpoints.
	Harness *evolution.Engine
	// Plugins is the composed plugin set. nil disables the plugin endpoints.
	Plugins *plugin.Registry
	// Dispatcher fires plugin hooks for the agent loop.
	Dispatcher *plugin.Dispatcher
	// Assets is the memory asset registry (ownership, version, visibility, ACL, bindings).
	Assets *memory.AssetRegistry
	// Pipeline distils memory layers off the turn: L0 raw -> L1 atom -> L2 scenario -> L3 persona.
	Pipeline *memory.Pipeline
	// Ret applies the retrieval budget: item count, character budget, and timeout.
	Ret *memory.Retriever
	// Runtime is the patch layers resolved at boot. Only keys the host reads are represented, so a
	// seam that is declared but never consumed cannot look like a feature.
	Runtime plugin.Runtime
	// Cap is the per-layer memory limit the runtime actually enforces, after the memory layer has
	// lowered it.
	Cap memory.LayerCap

	mu               sync.Mutex
	streams          map[string]streamEntry
	streamID         uint64
	muApproval       sync.Mutex
	pendingApprovals map[string]chan bool
}

type streamEntry struct {
	gen    uint64
	cancel context.CancelFunc
}

func NewServer(st *store.Store, cfg *clawmindcfg.Manager, client *llm.Client, mem memory.Store, toolsPath, skillsPath string, mcp *mcpclient.Session) *Server {
	if mem == nil {
		mem = memory.NoopStore{}
	}
	if cfg == nil {
		cfg = clawmindcfg.NewManager(".clawmind")
	}
	return &Server{
		Store:            st,
		Cfg:              cfg,
		LLM:              client,
		Mem:              mem,
		MCP:              mcp,
		ToolsPath:        toolsPath,
		SkillsPath:       skillsPath,
		streams:          make(map[string]streamEntry),
		pendingApprovals: make(map[string]chan bool),
	}
}

// layerCap returns the per-layer limits the runtime actually enforces. The census compares counts
// against these caps, so reporting the defaults when a memory layer lowered a cap would hide the one
// layer that is already pushing the rest of the context out.
func (s *Server) layerCap() memory.LayerCap {
	c := s.Cap
	if c.L0 == 0 && c.L1 == 0 && c.L2 == 0 && c.L3 == 0 {
		return memory.DefaultLayerCap()
	}
	return c
}

func (s *Server) toolDefinitions() []tools.Definition {
	toolsPath, skillsPath := s.ToolsPath, s.SkillsPath
	if s.Runtime.Tools.ToolsPath != "" {
		toolsPath = s.Runtime.Tools.ToolsPath
	}
	if s.Runtime.Tools.SkillsPath != "" {
		skillsPath = s.Runtime.Tools.SkillsPath
	}
	fileReg, _ := tools.Load(toolsPath)
	userReg, _ := tools.Load(skillsPath)
	out := tools.MergeDefinitions(tools.AtomicTools(), tools.CodeTools(), fileReg.Tools, userReg.Tools)
	if s.MCP != nil {
		out = tools.MergeDefinitions(out, s.MCP.Definitions())
	}
	// A mounted plugin replaces the whole definition of a tool it registers, which is what makes
	// the tool seam replaceable from config rather than hardcoded.
	if s.Plugins != nil {
		for _, src := range s.Plugins.Tools() {
			out = tools.MergeDefinitions(out, []tools.Definition{src.Definition})
		}
	}
	return out
}

func (s *Server) Register(r *gin.Engine) {
	r.GET("/api/health", s.health)
	api := r.Group("/api")
	{
		api.GET("/settings", s.getSettings)
		api.PUT("/settings", s.putSettings)
		api.GET("/skills", s.listSkills)
		api.POST("/skills", s.createSkill)
		api.POST("/skills/import", s.importSkills)
		api.POST("/skills/import-md", s.importSkillMarkdown)
		api.POST("/projects", s.createProject)
		api.GET("/projects", s.listProjects)
		api.DELETE("/projects/:id", s.deleteProject)
		api.PATCH("/projects/:id", s.patchProject)

		api.POST("/sessions", s.createSession)
		api.GET("/sessions", s.listSessions)
		api.GET("/sessions/:id", s.getSession)
		api.DELETE("/sessions/:id", s.deleteSession)
		api.PATCH("/sessions/:id", s.patchSession)
		api.GET("/sessions/:id/messages", s.listMessages)
		api.POST("/sessions/:id/messages", s.postMessage)
		api.POST("/sessions/:id/messages/:mid/regenerate", s.regenerateAssistant)
		api.GET("/sessions/:id/stream", s.stream)
		api.POST("/sessions/:id/messages/:mid/cancel", s.cancelStream)
		api.POST("/sessions/:id/tool-approval", s.postToolApproval)
		s.pluginRoutes(api)
		s.memoryRoutes(api)
		s.harnessRoutes(api)
	}
}

// pluginRoutes registers the plugin surface. Every part of the product is a plugin, so the routes
// expose the resolved composition rather than a static list of features.
func (s *Server) pluginRoutes(g *gin.RouterGroup) {
	if s.Plugins == nil {
		return
	}
	g.GET("/plugins", s.listPlugins)
	g.POST("/plugins", s.installPlugin)
	g.POST("/plugins/:id/enable", s.enablePlugin)
	g.POST("/plugins/:id/disable", s.disablePlugin)
	g.GET("/plugins/config", s.getPluginConfig)
}

// memoryRoutes registers the layered memory and the asset registry.
func (s *Server) memoryRoutes(g *gin.RouterGroup) {
	g.GET("/memory/layers", s.listMemoryLayers)
	g.GET("/memory/search", s.searchMemory)
	if s.Pipeline != nil {
		g.POST("/memory/distill", s.distillMemory)
	}
	if s.Assets == nil {
		return
	}
	g.GET("/memory/assets", s.listAssets)
	g.POST("/memory/assets", s.registerAsset)
	g.GET("/memory/assets/:id", s.getAsset)
	g.POST("/memory/assets/:id/share", s.shareAsset)
	g.POST("/memory/assets/:id/status", s.setAssetStatus)
	g.POST("/memory/assets/:id/acl", s.addAssetACL)
	g.POST("/memory/assets/:id/acl/revoke", s.revokeAssetACL)
	g.POST("/memory/assets/:id/bind", s.bindAsset)
	g.POST("/memory/unbind/:bindingId", s.unbindAsset)
	g.GET("/memory/loadout", s.getLoadout)
}

// applyRuntimeLayer puts the resolved patch layers on top of the file and environment config. The
// layer is operator config, so it wins over the file; a per-session model still wins over the layer
// because that is the user's choice for this conversation.
func (s *Server) applyRuntimeLayer(base clawmindcfg.Resolved) clawmindcfg.Resolved {
	out := base
	rt := s.Runtime
	if rt.LLM.Model != "" {
		out.Model = rt.LLM.Model
	}
	if rt.LLM.BaseURL != "" {
		out.BaseURL = rt.LLM.BaseURL
	}
	if rt.LLM.APIKey != "" {
		out.APIKey = rt.LLM.APIKey
	}
	if rt.LLM.Temperature != nil {
		out.Temperature = *rt.LLM.Temperature
	}
	if rt.LLM.TopP != nil {
		out.TopP = *rt.LLM.TopP
	}
	if rt.LLM.TopK != nil {
		out.TopK = rt.LLM.TopK
	}
	if rt.LLM.MaxAgentRounds > 0 {
		out.MaxAgentRounds = rt.LLM.MaxAgentRounds
	}
	if rt.Prompt.SystemPrompt != "" {
		out.SystemPrompt = rt.Prompt.SystemPrompt
	}
	return out
}

// narrowWorkspace applies the sandbox layer to the workspace root. A plugin may narrow the root but
// never widen it: a layer that could escape the root the process was given would make the root
// meaningless, so a widening request is refused rather than trusted.
func narrowWorkspace(base, want string) (string, bool) {
	want = strings.TrimSpace(want)
	if want == "" {
		return base, false
	}
	absBase, err := filepath.Abs(filepath.Clean(base))
	if err != nil {
		return base, false
	}
	abs, err := filepath.Abs(filepath.Clean(want))
	if err != nil {
		return base, false
	}
	if abs == absBase {
		return abs, true
	}
	if !strings.HasPrefix(abs, absBase+string(os.PathSeparator)) {
		return base, false
	}
	return abs, true
}

func (s *Server) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type createSessionReq struct {
	Model     string  `json:"model"`
	ProjectID *string `json:"projectId"`
}

func (s *Server) createSession(c *gin.Context) {
	ctx := c.Request.Context()
	var req createSessionReq
	_ = c.ShouldBindJSON(&req)
	fileCfg, err := s.Cfg.Load()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resolved := fileCfg.Resolved()
	model := req.Model
	if model == "" {
		model = resolved.Model
	}
	var pid *string
	if req.ProjectID != nil && *req.ProjectID != "" {
		ok, err := s.Store.ProjectExists(ctx, *req.ProjectID)
		if err != nil || !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid projectId"})
			return
		}
		pid = req.ProjectID
	}
	sess, err := s.Store.CreateSession(ctx, model, pid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, sess)
}

func (s *Server) listSessions(c *gin.Context) {
	ctx := c.Request.Context()
	q := c.Query("projectId")
	var pf *string
	if q != "" {
		pf = &q
	}
	list, err := s.Store.ListSessions(ctx, pf)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

func (s *Server) getSession(c *gin.Context) {
	sess, err := s.Store.GetSession(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, sess)
}

type patchSessionReq struct {
	ProjectID     *string           `json:"projectId"`
	SiblingChoice map[string]string `json:"siblingChoice"`
}

func (s *Server) patchSession(c *gin.Context) {
	ctx := c.Request.Context()
	sid := c.Param("id")
	var req patchSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ProjectID != nil {
		if *req.ProjectID == "" {
			if err := s.Store.SetSessionProject(ctx, sid, nil); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		} else {
			if err := s.Store.SetSessionProject(ctx, sid, req.ProjectID); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}
	}
	if req.SiblingChoice != nil {
		raw, err := json.Marshal(req.SiblingChoice)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := s.Store.PatchSessionMetadata(ctx, sid, map[string]string{"siblingChoice": string(raw)}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	sess, err := s.Store.GetSession(ctx, sid)
	if err != nil || sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, sess)
}

func (s *Server) deleteSession(c *gin.Context) {
	if err := s.Store.DeleteSession(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) listMessages(c *gin.Context) {
	msgs, err := s.Store.ListMessages(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, msgs)
}

type postMessageReq struct {
	Content             string  `json:"content"`
	ParentMessageID     *string `json:"parentMessageId"`
	EditOfUserMessageID *string `json:"editOfUserMessageId"`
}

type postMessageResp struct {
	UserMessageID      string `json:"userMessageId"`
	AssistantMessageID string `json:"assistantMessageId"`
}

func (s *Server) postMessage(c *gin.Context) {
	sid := c.Param("id")
	var req postMessageReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content required"})
		return
	}
	ctx := c.Request.Context()
	sess, err := s.Store.GetSession(ctx, sid)
	if err != nil || sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	hist, err := s.Store.ListMessages(ctx, sid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var parentPtr *string
	if req.EditOfUserMessageID != nil && strings.TrimSpace(*req.EditOfUserMessageID) != "" {
		oid := strings.TrimSpace(*req.EditOfUserMessageID)
		orig, err := s.Store.GetMessage(ctx, oid)
		if err != nil || orig == nil || orig.SessionID != sid || orig.Role != domain.RoleUser {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid editOfUserMessageId"})
			return
		}
		parentPtr = orig.ParentMessageID
	} else {
		if req.ParentMessageID != nil && strings.TrimSpace(*req.ParentMessageID) != "" {
			p := strings.TrimSpace(*req.ParentMessageID)
			pm, err := s.Store.GetMessage(ctx, p)
			if err != nil || pm == nil || pm.SessionID != sid {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid parentMessageId"})
				return
			}
			parentPtr = &p
		} else if len(hist) > 0 {
			lid := hist[len(hist)-1].ID
			parentPtr = &lid
		}
	}
	now := time.Now().UTC()
	userMsg := &domain.Message{
		ID:              uuid.NewString(),
		SessionID:       sid,
		Role:            domain.RoleUser,
		CreatedAt:       now,
		Parts:           []domain.Part{{Type: domain.PartText, Text: req.Content}},
		ParentMessageID: parentPtr,
	}
	if req.EditOfUserMessageID != nil && strings.TrimSpace(*req.EditOfUserMessageID) != "" {
		orig, _ := s.Store.GetMessage(ctx, strings.TrimSpace(*req.EditOfUserMessageID))
		if orig != nil {
			br := orig.ID
			if orig.BranchID != nil && *orig.BranchID != "" {
				br = *orig.BranchID
			}
			userMsg.BranchID = &br
		}
	} else {
		b := userMsg.ID
		userMsg.BranchID = &b
	}
	if err := s.Store.InsertMessage(ctx, userMsg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	asst := agent.NewAssistantPlaceholder(sid, userMsg.ID)
	if err := s.Store.InsertMessage(ctx, asst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	choice := thread.ParseSiblingChoice(sess.Metadata)
	pk := thread.ParentKey(userMsg.ParentMessageID)
	choice[pk] = userMsg.ID
	rawChoice, _ := json.Marshal(choice)
	if err := s.Store.PatchSessionMetadata(ctx, sid, map[string]string{"siblingChoice": string(rawChoice)}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if req.EditOfUserMessageID == nil {
		title := sess.Title
		if title == "" || title == "新对话" {
			runes := []rune(req.Content)
			if len(runes) > 36 {
				title = string(runes[:36]) + "…"
			} else {
				title = req.Content
			}
			_ = s.Store.UpdateSessionTitle(ctx, sid, title)
		}
	}
	_ = s.Store.TouchSession(ctx, sid)
	c.JSON(http.StatusOK, postMessageResp{
		UserMessageID:      userMsg.ID,
		AssistantMessageID: asst.ID,
	})
}

func (s *Server) regenerateAssistant(c *gin.Context) {
	ctx := c.Request.Context()
	sid := c.Param("id")
	mid := c.Param("mid")
	msg, err := s.Store.GetMessage(ctx, mid)
	if err != nil || msg == nil || msg.SessionID != sid {
		c.JSON(http.StatusNotFound, gin.H{"error": "message not found"})
		return
	}
	if msg.Role != domain.RoleAssistant {
		c.JSON(http.StatusBadRequest, gin.H{"error": "not an assistant message"})
		return
	}
	if msg.ParentMessageID == nil || *msg.ParentMessageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "assistant message has no parent"})
		return
	}
	if err := s.Store.UpdateMessageParts(ctx, mid, []domain.Part{{Type: domain.PartText, Text: ""}}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = s.Store.TouchSession(ctx, sid)
	c.Status(http.StatusNoContent)
}

func (s *Server) cancelStream(c *gin.Context) {
	mid := c.Param("mid")
	s.mu.Lock()
	ent, ok := s.streams[mid]
	if ok {
		delete(s.streams, mid)
	}
	s.mu.Unlock()
	if ok && ent.cancel != nil {
		ent.cancel()
	}
	c.Status(http.StatusNoContent)
}

type toolApprovalReq struct {
	ApprovalID string `json:"approvalId"`
	Approve    bool   `json:"approve"`
}

func (s *Server) postToolApproval(c *gin.Context) {
	var body toolApprovalReq
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.ApprovalID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "approvalId required"})
		return
	}
	s.muApproval.Lock()
	ch, ok := s.pendingApprovals[body.ApprovalID]
	s.muApproval.Unlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown or expired approval"})
		return
	}
	select {
	case ch <- body.Approve:
	default:
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) stream(c *gin.Context) {
	sid := c.Param("id")
	messageID := c.Query("messageId")
	if messageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messageId required"})
		return
	}
	ctx := c.Request.Context()
	msg, err := s.Store.GetMessage(ctx, messageID)
	if err != nil || msg == nil || msg.SessionID != sid {
		c.JSON(http.StatusNotFound, gin.H{"error": "message not found"})
		return
	}
	if msg.Role != domain.RoleAssistant {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is not assistant"})
		return
	}

	gen := atomic.AddUint64(&s.streamID, 1)
	s.mu.Lock()
	if prev, ok := s.streams[messageID]; ok {
		prev.cancel()
		delete(s.streams, messageID)
	}
	genCtx, cancel := context.WithCancel(ctx)
	s.streams[messageID] = streamEntry{gen: gen, cancel: cancel}
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		if cur, ok := s.streams[messageID]; ok && cur.gen == gen {
			delete(s.streams, messageID)
		}
		s.mu.Unlock()
	}()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		slog.Error("response writer is not a flusher")
		return
	}

	send := func(ev domain.StreamEvent) error {
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		_, err = c.Writer.Write([]byte("data: " + string(b) + "\n\n"))
		if err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	fileCfg, err := s.Cfg.Load()
	if err != nil {
		_ = send(domain.StreamEvent{Type: domain.EventError, MessageID: messageID, Error: err.Error()})
		return
	}
	resolved := s.applyRuntimeLayer(fileCfg.Resolved())
	sess, _ := s.Store.GetSession(ctx, sid)
	model := resolved.Model
	if sess != nil && sess.Model != "" {
		model = sess.Model
	}
	workspace := strings.TrimSpace(os.Getenv("AGENT_WORKSPACE"))
	if workspace == "" {
		if wd, err := os.Getwd(); err == nil {
			workspace = wd
		}
	}
	// The sandbox layer may narrow the root, never widen it, so a plugin cannot reach outside the
	// workspace the process was given.
	if narrowed, applied := narrowWorkspace(workspace, s.Runtime.Sandbox.Workspace); applied {
		workspace = narrowed
	}
	toolsJSON, _ := json.Marshal(s.toolDefinitions())
	promptsDir := strings.TrimSpace(os.Getenv("AGENT_PROMPTS_DIR"))
	if s.Runtime.Prompt.PromptsDir != "" {
		promptsDir = s.Runtime.Prompt.PromptsDir
	}
	maxCtx := 24000
	if v := strings.TrimSpace(os.Getenv("CLAWMIND_MAX_CONTEXT_TOKENS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxCtx = n
		}
	}
	if s.Runtime.Session.MaxContextTokens > 0 {
		maxCtx = s.Runtime.Session.MaxContextTokens
	}
	tokBudget := 0
	if v := strings.TrimSpace(os.Getenv("CLAWMIND_TOKEN_BUDGET")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tokBudget = n
		}
	}
	if s.Runtime.Session.TokenBudget > 0 {
		tokBudget = s.Runtime.Session.TokenBudget
	}
	pid := ""
	if sess != nil && sess.ProjectID != nil {
		pid = *sess.ProjectID
	}
	var harnessTree harness.Tree
	var harnessLessons []domain.HarnessLesson
	if s.Harness != nil {
		harnessTree, _ = s.Harness.Load(pid)
		harnessLessons, _ = s.Store.ListHarnessLessons(ctx, pid, 20)
	}
	// Plugin contributions are materialised into the view the agent sees, so a skill or subagent
	// mounted by a plugin is reachable through load_skill and delegate on the same surface.
	if s.Plugins != nil && len(harnessTree) > 0 {
		harnessTree = harnessTree.WithContributions(s.Plugins.Skills(), s.Plugins.Subagents())
	}
	var loadout []memory.Asset
	if s.Assets != nil {
		loadout, _ = s.Assets.Loadout(ctx, memory.Viewer{AgentID: "clawmind", Role: "member"})
	}

	// The sandbox layer is only attached when it actually says something: an empty layer means the
	// built-in floor, not a policy the agent has to reason about.
	var shellPolicy *security.Policy
	if rt := s.Runtime.Sandbox; len(rt.Denied) > 0 || len(rt.HighRisk) > 0 || rt.ShellTimeout > 0 || rt.Workspace != "" || rt.AllowApproval {
		resolvedPolicy := rt.Policy()
		shellPolicy = &resolvedPolicy
	}

	cfg := agent.RunConfig{
		AssistantMessageID: messageID,
		SessionID:          sid,
		BaseURL:            resolved.BaseURL,
		APIKey:             resolved.APIKey,
		Model:              model,
		SystemPrompt:       resolved.SystemPrompt,
		PromptsDir:         promptsDir,
		Temperature:        resolved.Temperature,
		TopP:               resolved.TopP,
		TopK:               resolved.TopK,
		MaxAgentRounds:     resolved.MaxAgentRounds,
		MaxContextTokens:   maxCtx,
		TokenBudget:        tokBudget,
		Workspace:          workspace,
		ToolsJSON:          toolsJSON,
		Repo:               s.repoProfile(workspace),
		Harness:            harnessTree,
		HarnessLessons:     harnessLessons,
		AutoVerify:         envFlag("CLAWMIND_AUTO_VERIFY", true),
		ProjectID:          pid,
		ShellPolicy:        shellPolicy,
		Loadout:            loadout,
	}
	if s.MCP != nil {
		cfg.MCPCall = s.MCP.CallTool
	}
	if s.Dispatcher != nil {
		cfg.Hooks = s.Dispatcher
	}
	if s.Pipeline != nil {
		cfg.Observer = s.Pipeline
	}
	cfg.WaitShellApproval = func(ctx context.Context, req agent.ShellApprovalRequest) (bool, error) {
		id := uuid.NewString()
		ch := make(chan bool, 1)
		s.muApproval.Lock()
		s.pendingApprovals[id] = ch
		s.muApproval.Unlock()
		defer func() {
			s.muApproval.Lock()
			delete(s.pendingApprovals, id)
			s.muApproval.Unlock()
		}()
		if err := send(domain.StreamEvent{
			Type:       domain.EventToolApprovalReq,
			MessageID:  messageID,
			SessionID:  sid,
			ApprovalID: id,
			ToolCallID: req.ToolCallID,
			ToolName:   req.ToolName,
			Arguments:  req.CommandSummary,
		}); err != nil {
			return false, err
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case ok := <-ch:
			approved := ok
			_ = send(domain.StreamEvent{
				Type:       domain.EventToolApprovalRes,
				MessageID:  messageID,
				SessionID:  sid,
				ApprovalID: id,
				Approved:   &approved,
			})
			return ok, nil
		case <-time.After(5 * time.Minute):
			return false, fmt.Errorf("确认超时")
		}
	}
	_ = agent.RunStream(genCtx, s.Store, s.Mem, s.LLM, cfg, send)
}
