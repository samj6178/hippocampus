package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

type HealthService struct {
	episodic  domain.EpisodicRepo
	semantic  domain.SemanticRepo
	embedding domain.EmbeddingProvider
	logger    *slog.Logger
}

func NewHealthService(
	episodic domain.EpisodicRepo,
	semantic domain.SemanticRepo,
	embedding domain.EmbeddingProvider,
	logger *slog.Logger,
) *HealthService {
	return &HealthService{
		episodic:  episodic,
		semantic:  semantic,
		embedding: embedding,
		logger:    logger,
	}
}

type HealthReport struct {
	Status          string            `json:"status"` // "healthy", "degraded", "unhealthy"
	EmbeddingOK     bool              `json:"embedding_ok"`
	EmbeddingModel  string            `json:"embedding_model"`
	EmbeddingLatMs  int64             `json:"embedding_latency_ms"`
	DatabaseOK      bool              `json:"database_ok"`
	EpisodicCount   int               `json:"episodic_count"`
	SemanticCount   int               `json:"semantic_count"`
	Issues          []string          `json:"issues,omitempty"`
	Recommendations []string          `json:"recommendations,omitempty"`
	Timestamp       time.Time         `json:"timestamp"`
}

func (h *HealthService) Check(ctx context.Context) *HealthReport {
	report := &HealthReport{
		Status:         "healthy",
		EmbeddingModel: h.embedding.ModelID(),
		Timestamp:      time.Now(),
	}

	start := time.Now()
	_, err := h.embedding.Embed(ctx, "health check ping")
	report.EmbeddingLatMs = time.Since(start).Milliseconds()
	if err != nil {
		report.EmbeddingOK = false
		report.Issues = append(report.Issues, fmt.Sprintf("Embedding provider failed: %v", err))
		report.Status = "unhealthy"
	} else {
		report.EmbeddingOK = true
		if report.EmbeddingLatMs > 2000 {
			report.Issues = append(report.Issues, fmt.Sprintf("Embedding latency high: %dms", report.EmbeddingLatMs))
			report.Recommendations = append(report.Recommendations, "Check Ollama server load or model availability")
		}
	}

	epCount, err := h.episodic.Count(ctx, nil)
	if err != nil {
		report.DatabaseOK = false
		report.Issues = append(report.Issues, fmt.Sprintf("Episodic DB error: %v", err))
		report.Status = "unhealthy"
	} else {
		report.DatabaseOK = true
		report.EpisodicCount = epCount
	}

	semCount, err := h.semantic.Count(ctx, nil)
	if err != nil {
		report.DatabaseOK = false
		report.Issues = append(report.Issues, fmt.Sprintf("Semantic DB error: %v", err))
		report.Status = "unhealthy"
	} else {
		report.SemanticCount = semCount
	}

	if epCount > 500 {
		report.Recommendations = append(report.Recommendations,
			fmt.Sprintf("High episodic count (%d). Run mos_consolidate to compress.", epCount))
	}
	if semCount > 1000 {
		report.Recommendations = append(report.Recommendations,
			fmt.Sprintf("High semantic count (%d). Consider pruning low-importance facts.", semCount))
	}
	if epCount == 0 && semCount == 0 {
		report.Recommendations = append(report.Recommendations,
			"Memory is empty. Use mos_remember to store knowledge or mos_ingest_codebase for cold start.")
	}

	if len(report.Issues) > 0 && report.Status == "healthy" {
		report.Status = "degraded"
	}

	h.logger.Info("health check completed",
		"status", report.Status,
		"embedding_ok", report.EmbeddingOK,
		"embedding_lat_ms", report.EmbeddingLatMs,
		"episodic", report.EpisodicCount,
		"semantic", report.SemanticCount,
		"issues", len(report.Issues),
	)

	return report
}
