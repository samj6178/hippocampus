package domain

import (
	"time"

	"github.com/google/uuid"
)

// MemoryTier represents the cognitive tier a memory belongs to.
type MemoryTier string

const (
	TierWorking    MemoryTier = "working"
	TierEpisodic   MemoryTier = "episodic"
	TierSemantic   MemoryTier = "semantic"
	TierProcedural MemoryTier = "procedural"
	TierCausal     MemoryTier = "causal"
)

// MemoryItem is the universal representation of a single memory unit
// across all tiers. Specific tiers extend this with additional fields.
type MemoryItem struct {
	ID           uuid.UUID   `json:"id"`
	ProjectID    *uuid.UUID  `json:"project_id,omitempty"` // nil = global memory
	Tier         MemoryTier  `json:"tier"`
	Content      string      `json:"content"`
	Summary      string      `json:"summary,omitempty"`
	Embedding    []float32   `json:"embedding,omitempty"`
	Importance   float64     `json:"importance"`
	Confidence   float64     `json:"confidence"`
	AccessCount  int         `json:"access_count"`
	TokenCount   int         `json:"token_count"`
	LastAccessed time.Time   `json:"last_accessed"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	Metadata     Metadata    `json:"metadata,omitempty"`
	Tags         []string    `json:"tags,omitempty"`
	Similarity   float64     `json:"-"` // transient: SQL-computed cosine similarity to query vector
}

// Metadata is a flexible key-value store for memory-specific data.
type Metadata map[string]any

// EpisodicMemory extends MemoryItem with session and agent context.
type EpisodicMemory struct {
	MemoryItem
	AgentID   string    `json:"agent_id"`
	SessionID uuid.UUID `json:"session_id"`
}

// SemanticMemory extends MemoryItem with knowledge graph properties.
type SemanticMemory struct {
	MemoryItem
	EntityType     string      `json:"entity_type"` // fact, concept, pattern, rule
	SourceEpisodes []uuid.UUID `json:"source_episodes,omitempty"`
}

// ProceduralMemory stores versioned action patterns with success tracking.
type ProceduralMemory struct {
	MemoryItem
	TaskType     string   `json:"task_type"`
	Steps        []Step   `json:"steps"`
	SuccessCount int      `json:"success_count"`
	FailureCount int      `json:"failure_count"`
	Version      int      `json:"version"`
}

// Step represents a single action in a procedural sequence.
type Step struct {
	Order       int    `json:"order"`
	Description string `json:"description"`
	Tool        string `json:"tool,omitempty"`
	Expected    string `json:"expected,omitempty"`
}

// SuccessRate returns the ratio of successful uses to total uses.
func (p *ProceduralMemory) SuccessRate() float64 {
	total := p.SuccessCount + p.FailureCount
	if total == 0 {
		return 0
	}
	return float64(p.SuccessCount) / float64(total)
}

// ImportanceScore computes the composite importance of a memory item
// given the current time and a query embedding.
//
// I(m, t, q) = w_s * S(m,q) + w_k * K(m,q) + w_r * R(m,t) + w_e * E(m) + w_em * Em(m)
//
// Where:
//   S  = semantic similarity (cosine of embeddings)
//   K  = keyword relevance (BM25/overlap signal)
//   R  = recency = exp(-λ * (t - last_accessed))
//   E  = explicit importance marker
//   Em = emotional intensity modulation
type ImportanceScore struct {
	SemanticSimilarity float64 `json:"semantic_similarity"`
	KeywordRelevance   float64 `json:"keyword_relevance"`
	Recency            float64 `json:"recency"`
	ExplicitImportance float64 `json:"explicit_importance"`
	EmotionalIntensity float64 `json:"emotional_intensity"`
	Composite          float64 `json:"composite"`
}

// ScoredMemory pairs a memory item with its computed importance score.
type ScoredMemory struct {
	Memory *MemoryItem
	Score  ImportanceScore
}
