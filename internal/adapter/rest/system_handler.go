package rest

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hippocampus-mcp/hippocampus/internal/app"
)

func (s *Server) handleHealthLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "alive",
	})
}

func (s *Server) handleHealthReady(w http.ResponseWriter, r *http.Request) {
	report := s.health.Check(r.Context())
	status := http.StatusOK
	if report.Status == "unhealthy" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, report)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.memory.Stats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleConsolidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	var result *app.ConsolidateResult
	var err error

	if req.Project != "" {
		project, pErr := s.project.GetBySlug(r.Context(), req.Project)
		if pErr != nil {
			writeError(w, http.StatusNotFound, "project not found: "+req.Project)
			return
		}
		result, err = s.consolidate.Run(r.Context(), &project.ID)
	} else {
		result, err = s.consolidate.Run(r.Context(), nil)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if s.ctxWriter != nil {
		go s.ctxWriter.WriteAll(context.Background())
	}

	writeJSON(w, http.StatusOK, result)
}
