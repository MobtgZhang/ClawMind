package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mobtgzhang/clawmind/backend/internal/memory"
)

// memoryLister is the read side of the store. Not every memory backend can enumerate rows, so
// retrieval stays available even when the layer census is not.
func (s *Server) memoryLister() memory.Lister {
	if l, ok := s.Mem.(memory.Lister); ok {
		return l
	}
	return nil
}

func viewerFromRequest(c *gin.Context) memory.Viewer {
	return memory.Viewer{
		UserID:  strings.TrimSpace(c.Query("userId")),
		TeamID:  strings.TrimSpace(c.Query("teamId")),
		AgentID: strings.TrimSpace(c.Query("agentId")),
		Role:    strings.TrimSpace(c.Query("role")),
	}
}

// listMemoryLayers reports the layer census: how much each layer holds against its own cap.
// A layer that has reached its cap is pushing the rest of the context out, which is the failure
// mode the caps exist to prevent, so the census is the thing worth reading.
func (s *Server) listMemoryLayers(c *gin.Context) {
	lister := s.memoryLister()
	if lister == nil {
		c.JSON(http.StatusOK, gin.H{"layers": []any{}, "note": "memory backend does not enumerate rows"})
		return
	}
	rows, err := lister.Rows(c.Request.Context(), strings.TrimSpace(c.Query("sessionId")), strings.TrimSpace(c.Query("projectId")), 1000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cap := s.layerCap()
	counts := map[memory.Layer]int{}
	for _, row := range rows {
		counts[row.Layer]++
	}
	var out []gin.H
	for _, layer := range memory.Layers() {
		out = append(out, gin.H{
			"layer": int(layer), "name": layer.Name(), "label": layer.Label(),
			"count": counts[layer], "cap": cap.ForLayer(layer),
		})
	}
	c.JSON(http.StatusOK, gin.H{"layers": out, "budget": s.Ret.Budget})
}

// searchMemory runs the layered retrieval the agent uses: bootstrap layers first, then BM25 +
// vector + RRF for the specific facts, under the item and character budget.
func (s *Server) searchMemory(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q required"})
		return
	}
	sessionID := strings.TrimSpace(c.Query("sessionId"))
	projectID := strings.TrimSpace(c.Query("projectId"))
	lines, err := s.Mem.RetrieveLevels(c.Request.Context(), sessionID, projectID, query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"query": query, "lines": lines, "budget": s.Ret.Budget})
}

type distillMemoryReq struct {
	SessionID string `json:"sessionId"`
	ProjectID string `json:"projectId"`
	Text      string `json:"text"`
}

// distillMemory runs the layer pipeline synchronously. The agent path distils off the turn; this
// endpoint exists so a batch of recorded text can be re-distilled and inspected immediately.
func (s *Server) distillMemory(c *gin.Context) {
	if s.Pipeline == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memory pipeline is not configured"})
		return
	}
	var req distillMemoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text required"})
		return
	}
	if err := s.Pipeline.Run(c.Request.Context(), strings.TrimSpace(req.SessionID), strings.TrimSpace(req.ProjectID), req.Text); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "pending": s.Pipeline.Pending()})
}

type registerAssetReq struct {
	Type       string `json:"type"`
	Name       string `json:"name"`
	OwnerID    string `json:"ownerId"`
	TeamID     string `json:"teamId"`
	Version    string `json:"version"`
	Status     string `json:"status"`
	Visibility string `json:"visibility"`
	Content    string `json:"content"`
}

// registerAsset puts a memory asset under ownership, version, status and visibility. Chat memory,
// skills, wiki pages and code graphs share one registry so access control exists once.
func (s *Server) registerAsset(c *gin.Context) {
	if s.Assets == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memory asset registry is not configured"})
		return
	}
	var req registerAssetReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	a := &memory.Asset{
		Type: strings.TrimSpace(req.Type), Name: strings.TrimSpace(req.Name),
		OwnerID: strings.TrimSpace(req.OwnerID), TeamID: strings.TrimSpace(req.TeamID),
		Version: strings.TrimSpace(req.Version), Status: strings.TrimSpace(req.Status),
		Visibility: strings.TrimSpace(req.Visibility), Content: req.Content,
	}
	if a.Type == "" || a.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type and name are required"})
		return
	}
	if !memory.ValidStatus(a.Status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}
	if !memory.ValidVisibility(a.Visibility) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid visibility"})
		return
	}
	if err := s.Assets.Register(c.Request.Context(), a); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, a)
}

func (s *Server) getAsset(c *gin.Context) {
	if s.Assets == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memory asset registry is not configured"})
		return
	}
	viewer := viewerFromRequest(c)
	a, err := s.Assets.Get(c.Request.Context(), strings.TrimSpace(c.Param("id")))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	ok, err := s.Assets.CanAccess(c.Request.Context(), a.ID, viewer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "viewer cannot access this asset", "assetId": a.ID})
		return
	}
	_ = s.Assets.TouchUsage(c.Request.Context(), a.ID)
	c.JSON(http.StatusOK, a)
}

func (s *Server) listAssets(c *gin.Context) {
	if s.Assets == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memory asset registry is not configured"})
		return
	}
	out, err := s.Assets.List(c.Request.Context(), viewerFromRequest(c), strings.TrimSpace(c.Query("type")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) shareAsset(c *gin.Context) {
	if s.Assets == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memory asset registry is not configured"})
		return
	}
	var body struct {
		Visibility string `json:"visibility"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	visibility := strings.TrimSpace(body.Visibility)
	if !memory.ValidVisibility(visibility) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid visibility"})
		return
	}
	if err := s.Assets.Share(c.Request.Context(), strings.TrimSpace(c.Param("id")), visibility); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) setAssetStatus(c *gin.Context) {
	if s.Assets == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memory asset registry is not configured"})
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	status := strings.TrimSpace(body.Status)
	if !memory.ValidStatus(status) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}
	if err := s.Assets.SetStatus(c.Request.Context(), strings.TrimSpace(c.Param("id")), status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type aclReq struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

func (s *Server) addAssetACL(c *gin.Context) {
	if s.Assets == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memory asset registry is not configured"})
		return
	}
	var body aclReq
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	kind, target := strings.TrimSpace(body.Kind), strings.TrimSpace(body.Target)
	if kind == "" || target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "kind and target are required"})
		return
	}
	if err := s.Assets.AddACL(c.Request.Context(), strings.TrimSpace(c.Param("id")), kind, target); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ok": true})
}

func (s *Server) revokeAssetACL(c *gin.Context) {
	if s.Assets == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memory asset registry is not configured"})
		return
	}
	var body aclReq
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.Assets.Revoke(c.Request.Context(), strings.TrimSpace(c.Param("id")), strings.TrimSpace(body.Kind), strings.TrimSpace(body.Target)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type bindAssetReq struct {
	AgentID string `json:"agentId"`
}

// bindAsset equips one asset onto an agent. Access control decides whether the asset is visible;
// the binding decides whether it is loaded into that agent's context by default.
func (s *Server) bindAsset(c *gin.Context) {
	if s.Assets == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memory asset registry is not configured"})
		return
	}
	var body bindAssetReq
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	agentID := strings.TrimSpace(body.AgentID)
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agentId required"})
		return
	}
	id, err := s.Assets.Bind(c.Request.Context(), strings.TrimSpace(c.Param("id")), agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ok": true, "bindingId": id})
}

func (s *Server) unbindAsset(c *gin.Context) {
	if s.Assets == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memory asset registry is not configured"})
		return
	}
	if err := s.Assets.Unbind(c.Request.Context(), strings.TrimSpace(c.Param("bindingId"))); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) getLoadout(c *gin.Context) {
	if s.Assets == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "memory asset registry is not configured"})
		return
	}
	out, err := s.Assets.Loadout(c.Request.Context(), viewerFromRequest(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	c.JSON(http.StatusOK, out)
}
