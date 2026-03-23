package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/embedding"
	"github.com/hippocampus-mcp/hippocampus/internal/memory"
)

// MemoryService provides read/delete operations for the REST dashboard.
// Write operations go through EncodeService; recall through RecallService.
type MemoryService struct {
	episodic  domain.EpisodicRepo
	semantic  domain.SemanticRepo
	project   domain.ProjectRepo
	emb       *embedding.OpenAIProvider
	working   *memory.WorkingMemory
	logger    *slog.Logger
}

func NewMemoryService(
	episodic domain.EpisodicRepo,
	semantic domain.SemanticRepo,
	project domain.ProjectRepo,
	emb *embedding.OpenAIProvider,
	working *memory.WorkingMemory,
	logger *slog.Logger,
) *MemoryService {
	return &MemoryService{
		episodic: episodic,
		semantic: semantic,
		project:  project,
		emb:      emb,
		working:  working,
		logger:   logger,
	}
}

type ListMemoriesFilter struct {
	ProjectSlug string
	Limit       int
	Offset      int
}

type MemoryListItem struct {
	ID         uuid.UUID         `json:"id"`
	ProjectID  *uuid.UUID        `json:"project_id,omitempty"`
	Tier       domain.MemoryTier `json:"tier"`
	Content    string            `json:"content"`
	Importance float64           `json:"importance"`
	TokenCount int               `json:"token_count"`
	Tags       []string          `json:"tags,omitempty"`
	AgentID    string            `json:"agent_id"`
	CreatedAt  string            `json:"created_at"`
}

type SystemStats struct {
	TotalEpisodic  int   `json:"total_episodic"`
	TotalSemantic  int   `json:"total_semantic"`
	WorkingMemFill int   `json:"working_memory_fill"`
	WorkingMemCap  int   `json:"working_memory_capacity"`
	CacheHits      int64 `json:"cache_hits"`
	CacheMisses    int64 `json:"cache_misses"`
	CacheSize      int   `json:"cache_size"`
	EmbeddingModel string `json:"embedding_model"`
	EmbeddingDims  int   `json:"embedding_dims"`
}

func (s *MemoryService) List(ctx context.Context, filter ListMemoriesFilter) ([]MemoryListItem, int, error) {
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 20
	}

	var projectID *uuid.UUID
	if filter.ProjectSlug != "" {
		p, err := s.project.GetBySlug(ctx, filter.ProjectSlug)
		if err != nil {
			return nil, 0, fmt.Errorf("project %q: %w", filter.ProjectSlug, err)
		}
		projectID = &p.ID
	}

	total, err := s.episodic.Count(ctx, projectID)
	if err != nil {
		return nil, 0, fmt.Errorf("count: %w", err)
	}

	memories, err := s.episodic.ListUnconsolidated(ctx, projectID, filter.Limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list: %w", err)
	}

	items := make([]MemoryListItem, 0, len(memories))
	for _, m := range memories {
		items = append(items, MemoryListItem{
			ID:         m.ID,
			ProjectID:  m.ProjectID,
			Tier:       m.Tier,
			Content:    m.Content,
			Importance: m.Importance,
			TokenCount: m.TokenCount,
			Tags:       m.Tags,
			AgentID:    m.AgentID,
			CreatedAt:  m.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	return items, total, nil
}

func (s *MemoryService) GetByID(ctx context.Context, id uuid.UUID) (*domain.EpisodicMemory, error) {
	return s.episodic.GetByID(ctx, id)
}

func (s *MemoryService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.episodic.Delete(ctx, id)
}

type FeedbackResult struct {
	MemoryID      uuid.UUID `json:"memory_id"`
	Tier          string    `json:"tier"`
	OldImportance float64   `json:"old_importance"`
	NewImportance float64   `json:"new_importance"`
}

// Feedback adjusts a memory's importance based on whether a recall was useful.
// Useful = importance * 1.3 (cap 1.0), not useful = importance * 0.7 (floor 0.1).
func (s *MemoryService) Feedback(ctx context.Context, memoryID uuid.UUID, useful bool) (*FeedbackResult, error) {
	ep, err := s.episodic.GetByID(ctx, memoryID)
	if err == nil {
		oldImp := ep.Importance
		newImp := s.adjustImportance(oldImp, useful)
		if err := s.episodic.UpdateImportance(ctx, memoryID, newImp); err != nil {
			return nil, fmt.Errorf("update episodic importance: %w", err)
		}
		s.logger.Info("feedback applied", "id", memoryID, "tier", "episodic", "useful", useful, "old", oldImp, "new", newImp)
		return &FeedbackResult{MemoryID: memoryID, Tier: "episodic", OldImportance: oldImp, NewImportance: newImp}, nil
	}

	sem, err := s.semantic.GetByID(ctx, memoryID)
	if err == nil {
		oldImp := sem.Importance
		newImp := s.adjustImportance(oldImp, useful)
		if err := s.semantic.UpdateImportance(ctx, memoryID, newImp); err != nil {
			return nil, fmt.Errorf("update semantic importance: %w", err)
		}
		s.logger.Info("feedback applied", "id", memoryID, "tier", "semantic", "useful", useful, "old", oldImp, "new", newImp)
		return &FeedbackResult{MemoryID: memoryID, Tier: "semantic", OldImportance: oldImp, NewImportance: newImp}, nil
	}

	return nil, fmt.Errorf("memory %s not found in any tier", memoryID)
}

func (s *MemoryService) adjustImportance(old float64, useful bool) float64 {
	if useful {
		v := old * 1.3
		if v > 1.0 {
			return 1.0
		}
		return v
	}
	v := old * 0.7
	if v < 0.1 {
		return 0.1
	}
	return v
}

func (s *MemoryService) Stats(ctx context.Context) (*SystemStats, error) {
	epCount, err := s.episodic.Count(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("episodic count: %w", err)
	}

	semCount, err := s.semantic.Count(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("semantic count: %w", err)
	}

	hits, misses, cacheSize := s.emb.CacheStats()
	snap := s.working.Snapshot(ctx)

	return &SystemStats{
		TotalEpisodic:  epCount,
		TotalSemantic:  semCount,
		WorkingMemFill: len(snap),
		WorkingMemCap:  s.working.Capacity(),
		CacheHits:      hits,
		CacheMisses:    misses,
		CacheSize:      cacheSize,
		EmbeddingModel: s.emb.ModelID(),
		EmbeddingDims:  s.emb.Dimensions(),
	}, nil
}
