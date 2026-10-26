package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mobtgzhang/clawmind/backend/internal/codectx"
	"github.com/mobtgzhang/clawmind/backend/internal/domain"
	"github.com/mobtgzhang/clawmind/backend/internal/evolution"
	"github.com/mobtgzhang/clawmind/backend/internal/harness"
	"github.com/mobtgzhang/clawmind/backend/internal/store"
)

// harnessRoutes registers the self-evolution endpoints.
func (s *Server) harnessRoutes(g *gin.RouterGroup) {
	if s.Harness == nil {
		return
	}
	g.GET("/harness", s.getHarness)
	g.GET("/harness/file", s.getHarnessFile)
	g.POST("/harness/file", s.putHarnessFile)
	g.POST("/harness/round", s.runHarnessRound)
	g.GET("/harness/rounds", s.listHarnessRounds)
	g.POST("/harness/adopt", s.adoptHarness)
	g.POST("/harness/lessons", s.addHarnessLesson)
	g.GET("/harness/library", s.getHarnessLibrary)
}

// harnessView is the current harness for one scope.
type harnessView struct {
	ProjectID     string                 `json:"projectId"`
	Dir           string                 `json:"dir"`
	Files         map[string]string      `json:"files"`
	AlwaysOn      []string               `json:"alwaysOn"`
	AlwaysOnChars int                    `json:"alwaysOnChars"`
	Skills        []harness.Skill        `json:"skills"`
	Scripts       []string               `json:"scripts"`
	Problems      []string               `json:"problems"`
	Lessons       []domain.HarnessLesson `json:"lessons"`
	Entries       []evolution.Entry      `json:"entries"`
}

func (s *Server) getHarness(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.Query("projectId"))
	t, err := s.Harness.Load(projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	lessons, _ := s.Store.ListHarnessLessons(ctx, projectID, 40)
	entries, _ := s.Harness.ListEntries(projectID)
	c.JSON(http.StatusOK, harnessView{
		ProjectID:     projectID,
		Dir:           s.Harness.Dir(projectID),
		Files:         t,
		AlwaysOn:      t.AlwaysOnPaths(),
		AlwaysOnChars: t.AlwaysOnChars(),
		Skills:        t.Skills(),
		Scripts:       t.Scripts(),
		Problems:      t.Validate(),
		Lessons:       lessons,
		Entries:       entries,
	})
}

func (s *Server) getHarnessFile(c *gin.Context) {
	rel := normalizeHarnessPath(c.Query("path"))
	if !evolution.OnSurface(rel) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is not on the editable harness surface"})
		return
	}
	t, err := s.Harness.Load(strings.TrimSpace(c.Query("projectId")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"path": rel, "content": t[rel]})
}

type putHarnessFileReq struct {
	ProjectID string `json:"projectId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

// putHarnessFile writes one component. The whole tree is validated first: a skill without a
// trigger line, or a reference to a script that does not exist, is rejected rather than stored.
func (s *Server) putHarnessFile(c *gin.Context) {
	var req putHarnessFileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rel := normalizeHarnessPath(req.Path)
	if !evolution.OnSurface(rel) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is not on the editable harness surface"})
		return
	}
	t, err := s.Harness.Load(req.ProjectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	t[rel] = req.Content
	if problems := t.Validate(); len(problems) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": strings.Join(problems, "; ")})
		return
	}
	if err := t.Write(s.Harness.Dir(req.ProjectID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "path": rel, "alwaysOnChars": t.AlwaysOnChars()})
}

type runHarnessRoundReq struct {
	ProjectID string `json:"projectId"`
	SessionID string `json:"sessionId"`
	Evidence  string `json:"evidence"`
}

// runHarnessRound performs one Propose-Probe-Compose-Select-Reflect step over a recorded
// trajectory. Evidence may be supplied directly or read from a session.
func (s *Server) runHarnessRound(c *gin.Context) {
	ctx := c.Request.Context()
	var req runHarnessRoundReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	evidence := strings.TrimSpace(req.Evidence)
	if evidence == "" && strings.TrimSpace(req.SessionID) != "" {
		evidence = buildEvidence(ctx, s.Store, strings.TrimSpace(req.SessionID))
	}
	if evidence == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "evidence or sessionId is required"})
		return
	}
	res, err := s.Harness.RunRound(ctx, strings.TrimSpace(req.ProjectID), strings.TrimSpace(req.SessionID), evidence)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "result": res})
		return
	}
	c.JSON(http.StatusOK, res)
}

func (s *Server) listHarnessRounds(c *gin.Context) {
	rounds, err := s.Store.ListHarnessRounds(c.Request.Context(), strings.TrimSpace(c.Query("projectId")), 30)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rounds)
}

type adoptHarnessReq struct {
	ProjectID string `json:"projectId"`
	Entry     string `json:"entry"`
}

func (s *Server) adoptHarness(c *gin.Context) {
	var req adoptHarnessReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := s.Harness.Adopt(req.ProjectID, req.Entry)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "files": t})
}

type addHarnessLessonReq struct {
	ProjectID  string `json:"projectId"`
	Cue        string `json:"cue"`
	Lesson     string `json:"lesson"`
	Evidence   string `json:"evidence"`
	Confidence string `json:"confidence"`
}

func (s *Server) addHarnessLesson(c *gin.Context) {
	var req addHarnessLessonReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	l := &domain.HarnessLesson{
		ProjectID:  strings.TrimSpace(req.ProjectID),
		Cue:        strings.TrimSpace(req.Cue),
		Lesson:     strings.TrimSpace(req.Lesson),
		Evidence:   strings.TrimSpace(req.Evidence),
		Confidence: strings.TrimSpace(req.Confidence),
	}
	if l.Cue == "" || l.Lesson == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cue and lesson are required"})
		return
	}
	if err := s.Store.AddHarnessLesson(c.Request.Context(), l); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, l)
}

func (s *Server) getHarnessLibrary(c *gin.Context) {
	path := filepath.Join(s.Harness.Dir(strings.TrimSpace(c.Query("projectId"))), "INDEX.md")
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, gin.H{"path": path, "content": ""})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"path": path, "content": string(b)})
}

func normalizeHarnessPath(rel string) string {
	return strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
}

// repoProfile profiles the workspace for code tasks: languages, manifests and the inferred
// verification command. Detection is bounded and never shells out.
func (s *Server) repoProfile(workspace string) *codectx.Profile {
	if !envFlag("CLAWMIND_CODE_PROFILE", true) {
		return nil
	}
	return codectx.Detect(workspace)
}

// envFlag reads a boolean environment flag.
func envFlag(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// buildEvidence renders the recorded trajectory for one session: the request, tool calls with
// their results, and the final answer. The editor reads it as data; nothing here is executed.
func buildEvidence(ctx context.Context, st *store.Store, sessionID string) string {
	msgs, err := st.ListMessages(ctx, sessionID)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, m := range msgs {
		for _, p := range m.Parts {
			switch p.Type {
			case domain.PartText:
				if strings.TrimSpace(p.Text) == "" {
					continue
				}
				role := string(m.Role)
				b.WriteString("### " + role + "\n" + p.Text + "\n\n")
			case domain.PartToolCall:
				b.WriteString("tool_call " + p.Name + " " + p.Arguments + "\n")
			case domain.PartToolResult:
				if strings.TrimSpace(p.Result) != "" {
					b.WriteString("tool_result " + p.Result + "\n\n")
				}
			}
		}
	}
	return strings.TrimSpace(b.String())
}
