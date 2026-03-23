package rest

import (
	"encoding/json"
	"net/http"

	"github.com/hippocampus-mcp/hippocampus/internal/adapter/llm"
)

type LLMSettingsRequest struct {
	Provider string `json:"provider"` // display label: "ollama", "deepseek", "qwen", "openai", "custom"
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	MaxRPM   int    `json:"max_rpm"`
}

func (s *Server) handleGetLLMSettings(w http.ResponseWriter, r *http.Request) {
	if s.llmSwitch == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM provider not configured")
		return
	}
	status := s.llmSwitch.Status(r.Context())
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleUpdateLLMSettings(w http.ResponseWriter, r *http.Request) {
	if s.llmSwitch == nil {
		writeError(w, http.StatusServiceUnavailable, "LLM provider not configured")
		return
	}

	var req LLMSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "base_url is required")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	maxRPM := req.MaxRPM
	if maxRPM <= 0 {
		maxRPM = 60
	}

	newProvider := llm.NewOpenAICompatProvider(llm.ProviderConfig{
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Model:   req.Model,
		MaxRPM:  maxRPM,
	}, s.logger)

	s.llmSwitch.Switch(newProvider)

	status := s.llmSwitch.Status(r.Context())
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleTestLLMConnection(w http.ResponseWriter, r *http.Request) {
	var req LLMSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	if req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "base_url is required")
		return
	}

	testProvider := llm.NewOpenAICompatProvider(llm.ProviderConfig{
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Model:   req.Model,
		MaxRPM:  120,
	}, s.logger)

	available := testProvider.IsAvailable(r.Context())

	writeJSON(w, http.StatusOK, map[string]any{
		"available": available,
		"provider":  testProvider.Name(),
	})
}
