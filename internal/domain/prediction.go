package domain

import (
	"time"

	"github.com/google/uuid"
)

// Prediction stores an expected outcome before a task is executed.
// After execution, SURPRISE compares predicted vs actual to compute
// the prediction error — the core learning signal.
type Prediction struct {
	ID              uuid.UUID  `json:"id"`
	TaskDescription string     `json:"task_description"`
	TaskEmbedding   []float32  `json:"task_embedding,omitempty"`
	PredictedOutput string     `json:"predicted_outcome"`
	ActualOutcome   string     `json:"actual_outcome,omitempty"`
	PredictionError float64    `json:"prediction_error,omitempty"`
	Domain          string     `json:"domain"`
	AgentID         string     `json:"agent_id"`
	ProjectID       *uuid.UUID `json:"project_id,omitempty"`
	Confidence      float64    `json:"confidence"`
	CreatedAt       time.Time  `json:"created_at"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
}

// IsResolved returns true if the prediction has been compared with reality.
func (p *Prediction) IsResolved() bool {
	return p.ResolvedAt != nil
}

// SurpriseLevel categorizes the prediction error into human-readable levels.
// Uses the principle: surprise ∝ |prediction_error| / expected_variance
func (p *Prediction) SurpriseLevel(sigma float64) string {
	if sigma <= 0 {
		sigma = 0.3 // default expected variance
	}
	ratio := p.PredictionError / sigma
	switch {
	case ratio < 0.5:
		return "expected"
	case ratio < 1.0:
		return "mild"
	case ratio < 2.0:
		return "surprising"
	default:
		return "shocking"
	}
}

// MetaCognitiveEntry logs a single prediction-outcome pair for calibration.
type MetaCognitiveEntry struct {
	ID                  uuid.UUID `json:"id"`
	Domain              string    `json:"domain"`
	PredictedConfidence float64   `json:"predicted_confidence"`
	ActualAccuracy      float64   `json:"actual_accuracy,omitempty"`
	StrategyUsed        string    `json:"strategy_used,omitempty"`
	StrategyOutcome     string    `json:"strategy_outcome,omitempty"` // success, partial, failure
	AgentID             string    `json:"agent_id"`
	CreatedAt           time.Time `json:"created_at"`
}
