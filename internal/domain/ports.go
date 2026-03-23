package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EpisodicRepo defines storage operations for episodic memory.
// Implemented by internal/repo, consumed by internal/app.
type EpisodicRepo interface {
	Insert(ctx context.Context, mem *EpisodicMemory) error
	GetByID(ctx context.Context, id uuid.UUID) (*EpisodicMemory, error)
	SearchSimilar(ctx context.Context, embedding []float32, projectID *uuid.UUID, limit int) ([]*EpisodicMemory, error)
	SearchBM25(ctx context.Context, query string, projectID *uuid.UUID, limit int) ([]*EpisodicMemory, error)
	ListBySession(ctx context.Context, sessionID uuid.UUID) ([]*EpisodicMemory, error)
	ListUnconsolidated(ctx context.Context, projectID *uuid.UUID, limit int) ([]*EpisodicMemory, error)
	UpdateImportance(ctx context.Context, id uuid.UUID, importance float64) error
	MarkConsolidated(ctx context.Context, ids []uuid.UUID) error
	ListByTags(ctx context.Context, projectID *uuid.UUID, tags []string, limit int) ([]*EpisodicMemory, error)
	DecayImportance(ctx context.Context, olderThan time.Duration, factor float64, floor float64) (int, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Count(ctx context.Context, projectID *uuid.UUID) (int, error)
}

// SemanticRepo defines storage operations for semantic (knowledge graph) memory.
type SemanticRepo interface {
	Insert(ctx context.Context, mem *SemanticMemory) error
	GetByID(ctx context.Context, id uuid.UUID) (*SemanticMemory, error)
	SearchSimilar(ctx context.Context, embedding []float32, projectID *uuid.UUID, limit int) ([]*SemanticMemory, error)
	SearchBM25(ctx context.Context, query string, projectID *uuid.UUID, limit int) ([]*SemanticMemory, error)
	SearchGlobal(ctx context.Context, embedding []float32, limit int) ([]*SemanticMemory, error)
	ListByProject(ctx context.Context, projectID *uuid.UUID, limit int) ([]*SemanticMemory, error)
	ListGlobal(ctx context.Context, limit int) ([]*SemanticMemory, error)
	ListByEntityType(ctx context.Context, projectID *uuid.UUID, entityType string, limit int) ([]*SemanticMemory, error)
	Update(ctx context.Context, mem *SemanticMemory) error
	UpdateImportance(ctx context.Context, id uuid.UUID, importance float64) error
	DecayImportance(ctx context.Context, olderThan time.Duration, factor float64, floor float64) (int, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Count(ctx context.Context, projectID *uuid.UUID) (int, error)
}

// ProceduralRepo defines storage operations for procedural memory.
type ProceduralRepo interface {
	Insert(ctx context.Context, mem *ProceduralMemory) error
	GetByID(ctx context.Context, id uuid.UUID) (*ProceduralMemory, error)
	SearchByTaskType(ctx context.Context, embedding []float32, projectID *uuid.UUID, limit int) ([]*ProceduralMemory, error)
	IncrementSuccess(ctx context.Context, id uuid.UUID) error
	IncrementFailure(ctx context.Context, id uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
	Count(ctx context.Context, projectID *uuid.UUID) (int, error)
}

// CausalRepo defines storage operations for causal links.
type CausalRepo interface {
	Insert(ctx context.Context, link *CausalLink) error
	GetByID(ctx context.Context, id uuid.UUID) (*CausalLink, error)
	GetCauses(ctx context.Context, effectID uuid.UUID) ([]*CausalLink, error)
	GetEffects(ctx context.Context, causeID uuid.UUID) ([]*CausalLink, error)
	AddEvidence(ctx context.Context, id uuid.UUID, episodeID uuid.UUID) error
	AddCounterEvidence(ctx context.Context, id uuid.UUID, episodeID uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// PredictionRepo defines storage for the Predictive Memory Engine.
type PredictionRepo interface {
	Insert(ctx context.Context, pred *Prediction) error
	GetByID(ctx context.Context, id uuid.UUID) (*Prediction, error)
	Resolve(ctx context.Context, id uuid.UUID, actualOutcome string, predictionError float64) error
	ListUnresolved(ctx context.Context, agentID string) ([]*Prediction, error)
	GetCalibration(ctx context.Context, domain string, agentID string) (*DomainCalibration, error)
}

// MetaCognitiveRepo defines storage for meta-cognitive log entries.
type MetaCognitiveRepo interface {
	Insert(ctx context.Context, entry *MetaCognitiveEntry) error
	GetCalibrationByDomain(ctx context.Context, agentID string) (map[string]*DomainCalibration, error)
	GetGaps(ctx context.Context, projectID *uuid.UUID) ([]*KnowledgeGap, error)
}

// ProjectRepo defines storage operations for project management.
type ProjectRepo interface {
	Create(ctx context.Context, project *Project) error
	GetByID(ctx context.Context, id uuid.UUID) (*Project, error)
	GetBySlug(ctx context.Context, slug string) (*Project, error)
	List(ctx context.Context) ([]*Project, error)
	Update(ctx context.Context, project *Project) error
	Delete(ctx context.Context, id uuid.UUID) error
	GetStats(ctx context.Context, id uuid.UUID) (*ProjectStats, error)
}

// EmotionalTagRepo defines storage for emotional tags on memories.
type EmotionalTagRepo interface {
	Insert(ctx context.Context, tag *EmotionalTag) error
	GetByMemory(ctx context.Context, memoryID uuid.UUID) ([]*EmotionalTag, error)
	GetHighPriority(ctx context.Context, projectID *uuid.UUID, limit int) ([]*EmotionalTag, error)
}

// EmbeddingProvider generates vector embeddings from text.
// Abstracts the embedding model (OpenAI, local, etc.).
type EmbeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
	ModelID() string
}

// LLMProvider abstracts text generation (chat completions).
// Implemented by adapter/llm.SwitchableProvider which wraps
// OpenAICompatProvider and supports runtime hot-swap.
type LLMProvider interface {
	Chat(ctx context.Context, messages []ChatMessage, opts ChatOptions) (string, error)
	Name() string
	IsAvailable(ctx context.Context) bool
}

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatOptions controls LLM generation parameters.
type ChatOptions struct {
	Model       string  `json:"model,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens   int     `json:"max_tokens,omitempty"`
}
