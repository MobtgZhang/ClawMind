package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type createProjectReq struct {
	Title string `json:"title"`
}

func (s *Server) createProject(c *gin.Context) {
	var req createProjectReq
	_ = c.ShouldBindJSON(&req)
	p, err := s.Store.CreateProject(c.Request.Context(), req.Title)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (s *Server) listProjects(c *gin.Context) {
	list, err := s.Store.ListProjects(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

func (s *Server) deleteProject(c *gin.Context) {
	if err := s.Store.DeleteProject(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

type patchProjectReq struct {
	Title string `json:"title"`
}

func (s *Server) patchProject(c *gin.Context) {
	var req patchProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.Store.UpdateProjectTitle(c.Request.Context(), c.Param("id"), req.Title); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
