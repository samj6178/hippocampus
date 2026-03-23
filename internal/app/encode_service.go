package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/memory"
	"github.com/hippocampus-mcp/hippocampus/internal/metrics"
)

// EncodeService implements the ENCODE operation of Memory Algebra.
// Flow: text -> Gate(threshold) -> Embed -> Score -> WorkingMemory + EpisodicRepo
type EncodeService struct {
	episodic   domain.EpisodicRepo
	semantic   domain.SemanticRepo
	emotional  domain.EmotionalTagRepo
	embedding  domain.EmbeddingProvider
	working    *memory.WorkingMemory
	causal     *CausalDetector
	procedural *ProceduralService
	logger     *slog.Logger
	gateThreshold   float64
	emotionDetector EmotionDetector
	recentHashes    map[string]struct{}
	recentHashesMu  sync.Mutex
}

type EncodeServiceConfig struct {
	GateThreshold float64
}

func (s *EncodeService) SetCausalDetector(cd *CausalDetector) {
	s.causal = cd
}

func (s *EncodeService) SetProceduralService(ps *ProceduralService) {
	s.procedural = ps
}

func NewEncodeService(
	episodic domain.EpisodicRepo,
	emotional domain.EmotionalTagRepo,
	embedding domain.EmbeddingProvider,
	working *memory.WorkingMemory,
	cfg EncodeServiceConfig,
	logger *slog.Logger,
) *EncodeService {
	if cfg.GateThreshold <= 0 {
		cfg.GateThreshold = 0.3
	}
	return &EncodeService{
		episodic:      episodic,
		emotional:     emotional,
		embedding:     embedding,
		working:       working,
		logger:        logger,
		gateThreshold: cfg.GateThreshold,
		recentHashes:  make(map[string]struct{}),
	}
}

func (s *EncodeService) SetSemanticRepo(sr domain.SemanticRepo) {
	s.semantic = sr
}

type EncodeRequest struct {
	Content    string            `json:"content"`
	ProjectID  *uuid.UUID        `json:"project_id,omitempty"`
	AgentID    string            `json:"agent_id"`
	SessionID  uuid.UUID         `json:"session_id"`
	Importance float64           `json:"importance"`
	Tags       []string          `json:"tags,omitempty"`
	Metadata   domain.Metadata   `json:"metadata,omitempty"`
}

type EncodeResponse struct {
	MemoryID       uuid.UUID          `json:"memory_id"`
	Encoded        bool               `json:"encoded"`
	GateScore      float64            `json:"gate_score"`
	TokenCount     int                `json:"token_count"`
	EmotionsFound  []DetectedEmotion  `json:"emotions,omitempty"`
}

func (s *EncodeService) Encode(ctx context.Context, req *EncodeRequest) (*EncodeResponse, error) {
	if req.Content == "" {
		return nil, domain.ErrEmptyContent
	}
	if req.AgentID == "" {
		req.AgentID = "hippocampus-internal"
	}
	if req.SessionID == uuid.Nil {
		req.SessionID = uuid.New()
	}
	if req.Importance <= 0 {
		req.Importance = 0.5
	}

	const maxContentChars = 4000
	if len(req.Content) > maxContentChars {
		truncated := req.Content[:maxContentChars]
		if idx := strings.LastIndex(truncated, ". "); idx > maxContentChars/2 {
			truncated = truncated[:idx+1]
		}
		s.logger.Warn("content truncated", "original_len", len(req.Content), "truncated_to", len(truncated))
		req.Content = truncated
	}

	// Hash-based exact dedup (fast O(1) check before expensive embedding)
	hash := sha256.Sum256([]byte(strings.TrimSpace(req.Content)))
	hashKey := hex.EncodeToString(hash[:16]) // 128-bit prefix is enough
	s.recentHashesMu.Lock()
	if _, dup := s.recentHashes[hashKey]; dup {
		s.recentHashesMu.Unlock()
		return &EncodeResponse{Encoded: false, GateScore: 0}, nil
	}
	s.recentHashes[hashKey] = struct{}{}
	// Cap map size to prevent unbounded growth
	if len(s.recentHashes) > 10000 {
		// Simple eviction: clear all (rare, ~10K unique memories)
		s.recentHashes = make(map[string]struct{})
		s.recentHashes[hashKey] = struct{}{}
	}
	s.recentHashesMu.Unlock()

	// Content quality gate: reject low-information inputs
	qualityScore := contentQualityScore(req.Content)
	if qualityScore < 0.15 {
		return &EncodeResponse{
			Encoded:   false,
			GateScore: qualityScore,
		}, nil
	}

	emb, err := s.embedding.Embed(ctx, req.Content)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}

	// Novelty detection: how different is this from existing memories?
	novelty := s.computeNovelty(ctx, emb, req.ProjectID)

	// Hard reject near-duplicates: if a memory with >90% similarity exists,
	// this is redundant information regardless of importance.
	if novelty < 0.10 {
		s.logger.Debug("memory gated (near-duplicate)",
			"novelty", novelty,
			"importance", req.Importance,
		)
		return &EncodeResponse{
			Encoded:   false,
			GateScore: novelty,
		}, nil
	}

	// Thalamic Gate: combine importance, quality, and novelty
	gateScore := req.Importance*0.5 + qualityScore*0.2 + novelty*0.3
	if gateScore < s.gateThreshold {
		s.logger.Debug("memory gated (below threshold)",
			"gate_score", gateScore,
			"threshold", s.gateThreshold,
			"novelty", novelty,
		)
		return &EncodeResponse{
			Encoded:   false,
			GateScore: gateScore,
		}, nil
	}

	// Auto-enrich tags
	autoTags := extractAutoTags(req.Content)
	mergedTags := mergeTagSets(req.Tags, autoTags)

	tokenCount := estimateTokens(req.Content)
	summary := extractSummary(req.Content)
	now := time.Now()
	id := uuid.New()

	mem := &domain.EpisodicMemory{
		MemoryItem: domain.MemoryItem{
			ID:           id,
			ProjectID:    req.ProjectID,
			Tier:         domain.TierEpisodic,
			Content:      req.Content,
			Summary:      summary,
			Embedding:    emb,
			Importance:   req.Importance,
			Confidence:   1.0,
			TokenCount:   tokenCount,
			LastAccessed: now,
			CreatedAt:    now,
			UpdatedAt:    now,
			Tags:         mergedTags,
			Metadata:     req.Metadata,
		},
		AgentID:   req.AgentID,
		SessionID: req.SessionID,
	}

	if err := s.episodic.Insert(ctx, mem); err != nil {
		return nil, fmt.Errorf("insert episodic: %w", err)
	}

	emotions := s.emotionDetector.Detect(req.Content)
	if len(emotions) > 0 && s.emotional != nil {
		for _, em := range emotions {
			tag := &domain.EmotionalTag{
				MemoryID:   id,
				MemoryTier: domain.TierEpisodic,
				Valence:    em.Valence,
				Intensity:  em.Intensity,
				Signals:    em.Signals,
			}
			if err := s.emotional.Insert(ctx, tag); err != nil {
				s.logger.Warn("emotional tag insert failed", "valence", em.Valence, "error", err)
			}
		}

		boost := emotionalImportanceBoost(emotions)
		if boost > 0 {
			boosted := req.Importance + boost
			if boosted > 1.0 {
				boosted = 1.0
			}
			if err := s.episodic.UpdateImportance(ctx, id, boosted); err != nil {
				s.logger.Warn("emotional boost failed", "error", err)
			} else {
				s.logger.Info("emotional importance boost",
					"id", id,
					"original", req.Importance,
					"boosted", boosted,
					"emotions", len(emotions),
				)
			}
		}
	}

	if s.causal != nil {
		go s.causal.DetectAndStore(context.Background(), id, req.Content, emb, req.ProjectID)
	}

	if s.procedural != nil {
		go func() {
			if procID, err := s.procedural.StoreIfProcedural(context.Background(), req.Content, req.ProjectID); err == nil && procID != nil {
				s.logger.Info("auto-created procedural memory", "proc_id", procID, "source", id)
			}
		}()
	}

	s.demoteSimilar(ctx, emb, req.ProjectID, id, req.Importance)

	evicted := s.working.Put(ctx, &mem.MemoryItem)
	if evicted != nil {
		s.logger.Debug("working memory eviction",
			"evicted_id", evicted.ID,
			"evicted_importance", evicted.Importance,
		)
	}

	metrics.EncodeTotal.Inc()

	s.logger.Info("memory encoded",
		"id", id,
		"project", req.ProjectID,
		"agent", req.AgentID,
		"importance", req.Importance,
		"tokens", tokenCount,
	)

	return &EncodeResponse{
		MemoryID:      id,
		Encoded:       true,
		GateScore:     gateScore,
		TokenCount:    tokenCount,
		EmotionsFound: emotions,
	}, nil
}

func emotionalImportanceBoost(emotions []DetectedEmotion) float64 {
	var maxBoost float64
	for _, e := range emotions {
		var boost float64
		switch e.Valence {
		case domain.ValDanger:
			boost = e.Intensity * 0.3
		case domain.ValSurprise:
			boost = e.Intensity * 0.2
		case domain.ValFrustration:
			boost = e.Intensity * 0.15
		case domain.ValSuccess:
			boost = e.Intensity * 0.1
		case domain.ValNovelty:
			boost = e.Intensity * 0.05
		}
		if boost > maxBoost {
			maxBoost = boost
		}
	}
	return maxBoost
}

// computeNovelty estimates how novel the content is compared to existing memories.
// Returns 0.0 (exact duplicate exists) to 1.0 (completely novel).
func (s *EncodeService) computeNovelty(ctx context.Context, emb []float32, projectID *uuid.UUID) float64 {
	maxSim := 0.0

	similar, err := s.episodic.SearchSimilar(ctx, emb, projectID, 3)
	if err == nil {
		for _, ep := range similar {
			if ep.Similarity > maxSim {
				maxSim = ep.Similarity
			}
		}
	}

	if s.semantic != nil {
		semSimilar, err := s.semantic.SearchSimilar(ctx, emb, projectID, 3)
		if err == nil {
			for _, sm := range semSimilar {
				if sm.Similarity > maxSim {
					maxSim = sm.Similarity
				}
			}
		}
	}

	novelty := 1.0 - maxSim
	if novelty < 0 {
		novelty = 0
	}
	return novelty
}

// demoteSimilar finds memories very similar to the new one (cosine > 0.75)
// and reduces their importance across both episodic and semantic tiers.
// This prevents stale superseded memories from competing during recall.
func (s *EncodeService) demoteSimilar(ctx context.Context, emb []float32, projectID *uuid.UUID, newID uuid.UUID, newImportance float64) {
	similar, err := s.episodic.SearchSimilar(ctx, emb, projectID, 5)
	if err != nil {
		s.logger.Warn("demoteSimilar: search failed", "error", err)
		return
	}
	for _, ep := range similar {
		if ep.ID == newID {
			continue
		}
		if ep.Similarity < 0.75 {
			continue
		}
		demoted := ep.Importance * 0.7
		if demoted < 0.1 {
			demoted = 0.1
		}
		if err := s.episodic.UpdateImportance(ctx, ep.ID, demoted); err != nil {
			s.logger.Warn("demoteSimilar: update failed", "id", ep.ID, "error", err)
		} else {
			s.logger.Info("demoted superseded memory",
				"old_id", ep.ID,
				"old_importance", ep.Importance,
				"new_importance", demoted,
				"similarity", ep.Similarity,
			)
		}
	}

	// Cross-tier: also demote highly similar semantic memories
	if s.semantic != nil {
		semSimilar, err := s.semantic.SearchSimilar(ctx, emb, projectID, 3)
		if err != nil {
			return
		}
		for _, sm := range semSimilar {
			if sm.Similarity < 0.80 {
				continue
			}
			demoted := sm.Importance * 0.8
			if demoted < 0.1 {
				demoted = 0.1
			}
			if err := s.semantic.UpdateImportance(ctx, sm.ID, demoted); err != nil {
				s.logger.Warn("demoteSimilar: semantic update failed", "id", sm.ID, "error", err)
			} else {
				s.logger.Info("demoted superseded semantic memory",
					"id", sm.ID,
					"similarity", sm.Similarity,
				)
			}
		}
	}
}

func estimateTokens(text string) int {
	// GPT-family heuristic: ~4 chars per token for English, ~2-3 for code
	return len(text) / 4
}
