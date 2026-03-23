package rest

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.project.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type projectWithStats struct {
		ID          string `json:"id"`
		Slug        string `json:"slug"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		IsActive    bool   `json:"is_active"`
		CreatedAt   string `json:"created_at"`
	}

	result := make([]projectWithStats, 0, len(projects))
	for _, p := range projects {
		result = append(result, projectWithStats{
			ID:          p.ID.String(),
			Slug:        p.Slug,
			DisplayName: p.DisplayName,
			Description: p.Description,
			IsActive:    p.IsActive,
			CreatedAt:   p.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"projects": result})
}

type createProjectRequest struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	RootPath    string `json:"root_path"`
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Slug == "" {
		writeError(w, http.StatusBadRequest, "slug is required")
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Slug
	}

	project, err := s.project.Create(r.Context(), req.Slug, req.DisplayName, req.Description, req.RootPath)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	project, err := s.project.GetBySlug(r.Context(), slug)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleGetProjectStats(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	project, err := s.project.GetBySlug(r.Context(), slug)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	stats, err := s.project.GetStats(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	project, err := s.project.GetBySlug(r.Context(), slug)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	if err := s.project.Delete(r.Context(), project.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
