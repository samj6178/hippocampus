package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// MemoryAlgebra defines the 8 primitive operations of the Hippocampus
// Memory Operating System. These form a complete algebra for cognitive
// memory — any future implementation (quantum, neuromorphic, biological-digital)
// must implement these operations, just as any DBMS implements relational algebra.
//
// Formal properties:
//   - CONSOLIDATE is associative: Consolidate(Consolidate(A,B), C) = Consolidate(A,B,C)
//   - FORGET is irreversible: there is no Unforget (safety by design)
//   - RECALL is submodular: marginal value of additional memory in context has diminishing returns
//   - PREDICT + SURPRISE = optimal learning signal (Shannon information theory)
type MemoryAlgebra interface {
	// ENCODE transforms an experience into a memory item and stores it.
	// Applies thalamic gating: low-novelty/low-relevance experiences may be rejected.
	// Automatically detects emotional signals and attaches tags.
	Encode(ctx context.Context, req EncodeRequest) (*EncodeResponse, error)

	// RECALL assembles optimal context for a task within a token budget.
	// Uses submodular greedy selection to maximize information while minimizing redundancy.
	// Retrieves from all memory tiers in parallel, scores by I(m,t,q), respects project scope.
	Recall(ctx context.Context, req RecallRequest) (*RecallResponse, error)

	// CONSOLIDATE transfers episodic memories into semantic facts.
	// Implements selective replay: surprising/emotional episodes are prioritized.
	// Grounded in Complementary Learning Systems theory (McClelland et al., 1995).
	Consolidate(ctx context.Context, req ConsolidateRequest) (*ConsolidateResponse, error)

	// FORGET permanently removes a memory. Irreversible by design.
	// Required for safety (dangerous knowledge removal) and privacy (right to be forgotten).
	Forget(ctx context.Context, req ForgetRequest) error

	// PREDICT generates an expected outcome for a task, based on similar past experiences.
	// Grounded in Predictive Processing framework (Friston's Free Energy Principle).
	Predict(ctx context.Context, req PredictRequest) (*PredictResponse, error)

	// SURPRISE compares a prediction with actual outcome and computes prediction error.
	// Only the delta (surprising part) is stored — information-theoretically optimal (Shannon).
	Surprise(ctx context.Context, req SurpriseRequest) (*SurpriseResponse, error)

	// ANALOGIZE finds structural similarities between memories across domains/projects.
	// Enables cross-domain transfer: a pattern from one project may apply to another.
	// Grounded in Structure Mapping Theory (Gentner, 1983).
	Analogize(ctx context.Context, req AnalogizeRequest) (*AnalogizeResponse, error)

	// META performs self-assessment of memory quality, calibration, and gaps.
	// Returns calibrated confidence per domain, detected knowledge gaps,
	// and strategy effectiveness rankings.
	Meta(ctx context.Context, req MetaRequest) (*MetaResponse, error)
}

// --- ENCODE ---

type EncodeRequest struct {
	Content    string     `json:"content"`
	ProjectID  *uuid.UUID `json:"project_id,omitempty"`
	AgentID    string     `json:"agent_id"`
	SessionID  uuid.UUID  `json:"session_id"`
	Importance float64    `json:"importance,omitempty"` // 0 = auto-detect
	Tier       MemoryTier `json:"tier,omitempty"`       // empty = auto-classify
	Tags       []string   `json:"tags,omitempty"`
	Outcome    string     `json:"outcome,omitempty"` // for procedural: was it successful?
}

type EncodeResponse struct {
	MemoryID      uuid.UUID      `json:"memory_id"`
	Tier          MemoryTier     `json:"tier"`
	EmotionalTags []EmotionalTag `json:"emotional_tags,omitempty"`
	GateDecision  GateDecision   `json:"gate_decision"`
}

type GateDecision struct {
	Passed    bool    `json:"passed"`
	Novelty   float64 `json:"novelty"`
	InfoGain  float64 `json:"info_gain"`
	Relevance float64 `json:"relevance"`
	Score     float64 `json:"score"`
}

// --- RECALL ---

type RecallRequest struct {
	Task        string     `json:"task"`
	BudgetToken int        `json:"budget_tokens"`
	ProjectID   *uuid.UUID `json:"project_id,omitempty"`
	AgentID     string     `json:"agent_id,omitempty"`
}

type RecallResponse struct {
	Context    string          `json:"context"`
	Sources    []RecallSource  `json:"sources"`
	TokenCount int             `json:"token_count"`
	Confidence float64         `json:"confidence"` // calibrated confidence of assembled context
	LatencyMs  float64         `json:"latency_ms"`
}

type RecallSource struct {
	MemoryID  uuid.UUID  `json:"memory_id"`
	Tier      MemoryTier `json:"tier"`
	Relevance float64    `json:"relevance"`
	Snippet   string     `json:"snippet,omitempty"`
}

// --- CONSOLIDATE ---

type ConsolidateRequest struct {
	ProjectID *uuid.UUID `json:"project_id,omitempty"`
	Force     bool       `json:"force,omitempty"`
}

type ConsolidateResponse struct {
	EpisodesProcessed int `json:"episodes_processed"`
	FactsExtracted    int `json:"facts_extracted"`
	CausalLinksFound  int `json:"causal_links_found"`
	Contradictions    int `json:"contradictions_detected"`
	CompressedCount   int `json:"compressed_count"`
}

// --- FORGET ---

type ForgetRequest struct {
	MemoryID uuid.UUID `json:"memory_id"`
	Reason   string    `json:"reason"`
}

// --- PREDICT ---

type PredictRequest struct {
	Task      string     `json:"task"`
	ProjectID *uuid.UUID `json:"project_id,omitempty"`
	AgentID   string     `json:"agent_id"`
}

type PredictResponse struct {
	PredictionID    uuid.UUID        `json:"prediction_id"`
	PredictedOutput string           `json:"predicted_outcome"`
	Confidence      float64          `json:"confidence"`
	SimilarPast     []PastExperience `json:"similar_past_tasks,omitempty"`
}

type PastExperience struct {
	Summary string  `json:"summary"`
	Outcome string  `json:"outcome"`
	Score   float64 `json:"similarity_score"`
}

// --- SURPRISE ---

type SurpriseRequest struct {
	PredictionID  uuid.UUID `json:"prediction_id"`
	ActualOutcome string    `json:"actual_outcome"`
}

type SurpriseResponse struct {
	PredictionError float64 `json:"prediction_error"`
	SurpriseLevel   string  `json:"surprise_level"` // "expected", "mild", "surprising", "shocking"
	StoredAsDelta   bool    `json:"stored_as_delta"`
	DeltaMemoryID   *uuid.UUID `json:"delta_memory_id,omitempty"`
}

// --- ANALOGIZE ---

type AnalogizeRequest struct {
	SourceMemoryID uuid.UUID  `json:"source_memory_id"`
	TargetProject  *uuid.UUID `json:"target_project,omitempty"` // nil = search all projects
}

type AnalogizeResponse struct {
	Analogies []Analogy `json:"analogies"`
}

type Analogy struct {
	SourceID         uuid.UUID `json:"source_id"`
	TargetID         uuid.UUID `json:"target_id"`
	StructuralScore  float64   `json:"structural_score"`
	Explanation      string    `json:"explanation"`
	TransferSuggestion string  `json:"transfer_suggestion,omitempty"`
}

// --- META ---

type MetaRequest struct {
	ProjectID *uuid.UUID `json:"project_id,omitempty"`
}

type MetaResponse struct {
	CalibrationByDomain map[string]DomainCalibration `json:"calibration_by_domain"`
	GapsDetected        []KnowledgeGap               `json:"gaps_detected"`
	BestStrategies      []string                      `json:"best_strategies"`
	TotalPredictions    int                            `json:"total_predictions"`
	OverallAccuracy     float64                        `json:"overall_accuracy"`
	MemoryHealth        MemoryHealth                   `json:"memory_health"`
}

type DomainCalibration struct {
	Domain              string  `json:"domain"`
	PredictedConfidence float64 `json:"avg_predicted_confidence"`
	ActualAccuracy      float64 `json:"actual_accuracy"`
	CalibrationOffset   float64 `json:"calibration_offset"` // predicted - actual
	SampleCount         int     `json:"sample_count"`
}

type KnowledgeGap struct {
	Domain      string  `json:"domain"`
	Description string  `json:"description"`
	QueryCount  int     `json:"query_count"`  // how many times this area was queried
	AvgConfidence float64 `json:"avg_confidence"` // how confident responses were
	GapScore    float64 `json:"gap_score"`   // query_count * (1 - avg_confidence)
}

type MemoryHealth struct {
	TotalMemories     int            `json:"total_memories"`
	ByTier            map[MemoryTier]int `json:"by_tier"`
	ByProject         map[string]int `json:"by_project"`
	StorageBytes      int64          `json:"storage_bytes"`
	AvgRecallLatency  time.Duration  `json:"avg_recall_latency"`
	ContradictionRate float64        `json:"contradiction_rate"`
	LastConsolidation time.Time      `json:"last_consolidation"`
}
