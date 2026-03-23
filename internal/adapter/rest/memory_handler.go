package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/app"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

func (s *Server) handleListMemories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	filter := app.ListMemoriesFilter{
		ProjectSlug: q.Get("project"),
		Limit:       limit,
		Offset:      offset,
	}

	items, total, err := s.memory.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"memories": items,
		"total":    total,
	})
}

type createMemoryRequest struct {
	Content    string   `json:"content"`
	Project    string   `json:"project,omitempty"`
	Importance float64  `json:"importance"`
	Tags       []string `json:"tags,omitempty"`
}

func (s *Server) handleCreateMemory(w http.ResponseWriter, r *http.Request) {
	var req createMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	var projectID *uuid.UUID
	if req.Project != "" {
		p, err := s.project.GetBySlug(r.Context(), req.Project)
		if err != nil {
			writeError(w, http.StatusBadRequest, "project not found: "+req.Project)
			return
		}
		projectID = &p.ID
	}

	if req.Importance <= 0 {
		req.Importance = 0.5
	}

	encReq := &app.EncodeRequest{
		Content:    req.Content,
		ProjectID:  projectID,
		AgentID:    "dashboard",
		SessionID:  uuid.New(),
		Importance: req.Importance,
		Tags:       req.Tags,
	}

	resp, err := s.encode.Encode(r.Context(), encReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Note: StoreIfProcedural is already called inside EncodeService.Encode()
	go func() {
		if s.ctxWriter != nil {
			s.ctxWriter.WriteAll(context.Background())
		}
	}()

	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleGetMemory(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	mem, err := s.memory.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "memory not found")
		return
	}

	writeJSON(w, http.StatusOK, mem)
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := s.memory.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type recallRequest struct {
	Query       string `json:"query"`
	Project     string `json:"project,omitempty"`
	BudgetTokens int   `json:"budget_tokens,omitempty"`
}

func (s *Server) handleRecall(w http.ResponseWriter, r *http.Request) {
	var req recallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Query == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"context":              "No query provided. Describe what you need to know.",
			"token_count":          10,
			"sources_used":        0,
			"candidates_considered": 0,
			"confidence":           0,
		})
		return
	}

	var projectID *uuid.UUID
	if req.Project != "" {
		p, err := s.project.GetBySlug(r.Context(), req.Project)
		if err != nil {
			writeError(w, http.StatusBadRequest, "project not found: "+req.Project)
			return
		}
		projectID = &p.ID
	}

	budget := domain.DefaultBudget()
	if req.BudgetTokens > 0 {
		budget.Total = req.BudgetTokens
	}

	recallReq := &app.RecallRequest{
		Query:         req.Query,
		ProjectID:     projectID,
		Budget:        budget,
		AgentID:       "dashboard",
		IncludeGlobal: projectID == nil,
	}

	resp, err := s.recall.Recall(r.Context(), recallReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MemoryID string `json:"memory_id"`
		Useful   bool   `json:"useful"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	memID, err := uuid.Parse(req.MemoryID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid memory_id: "+err.Error())
		return
	}

	result, err := s.memory.Feedback(r.Context(), memID, req.Useful)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleBenchmark(w http.ResponseWriter, r *http.Request) {
	report, err := s.benchmark.Run(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"formatted_report": report.Formatted,
		"aggregate":        report.Aggregate,
		"by_category":      report.ByCategory,
		"results":          report.Results,
	})
}
