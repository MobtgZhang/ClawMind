package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/mobtgzhang/clawmind/backend/internal/agent"
	"github.com/mobtgzhang/clawmind/backend/internal/clawmindcfg"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/llm"
	"github.com/mobtgzhang/clawmind/backend/internal/memory"
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
	ToolsPath  string // e.g. ./config/tools.json
	SkillsPath string // e.g. .clawmind/skills.json

	mu       sync.Mutex
	streams  map[string]streamEntry
	streamID uint64
}

type streamEntry struct {
	gen    uint64
	cancel context.CancelFunc
}

func NewServer(st *store.Store, cfg *clawmindcfg.Manager, client *llm.Client, mem memory.Store, toolsPath, skillsPath string) *Server {
	if mem == nil {
		mem = memory.NoopStore{}
	}
	if cfg == nil {
		cfg = clawmindcfg.NewManager(".clawmind")
	}
	return &Server{
		Store:      st,
		Cfg:        cfg,
		LLM:        client,
		Mem:        mem,
		ToolsPath:  toolsPath,
		SkillsPath: skillsPath,
		streams:    make(map[string]streamEntry),
	}
}

func (s *Server) toolDefinitions() []tools.Definition {
	fileReg, _ := tools.Load(s.ToolsPath)
	userReg, _ := tools.Load(s.SkillsPath)
	return tools.MergeDefinitions(tools.AtomicTools(), fileReg.Tools, userReg.Tools)
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
	}
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
	resolved := fileCfg.Resolved()
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
	toolsJSON, _ := json.Marshal(s.toolDefinitions())
	promptsDir := strings.TrimSpace(os.Getenv("AGENT_PROMPTS_DIR"))
	cfg := agent.RunConfig{
		AssistantMessageID: messageID,
		BaseURL:            resolved.BaseURL,
		APIKey:             resolved.APIKey,
		Model:              model,
		SystemPrompt:       resolved.SystemPrompt,
		PromptsDir:         promptsDir,
		Temperature:        resolved.Temperature,
		TopP:               resolved.TopP,
		TopK:               resolved.TopK,
		MaxAgentRounds:     resolved.MaxAgentRounds,
		Workspace:          workspace,
		ToolsJSON:          toolsJSON,
	}
	_ = agent.RunStream(genCtx, s.Store, s.Mem, s.LLM, cfg, send)
}
