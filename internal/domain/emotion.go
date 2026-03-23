package domain

import (
	"time"

	"github.com/google/uuid"
)

// Valence represents a formal (non-anthropomorphic) emotional signal
// detected in an experience. These affect consolidation priority and decay rate.
type Valence string

const (
	ValSuccess     Valence = "success"     // task completed first try, user confirmed
	ValFrustration Valence = "frustration" // user corrections > 3 in one task
	ValSurprise    Valence = "surprise"    // prediction error > 2*sigma
	ValNovelty     Valence = "novelty"     // max cosine similarity with existing < 0.3
	ValDanger      Valence = "danger"      // rollback, data loss, production incident
)

// EmotionalTag attaches an affective signal to a memory item.
// Intensity ranges from 0.0 (barely detectable) to 1.0 (extreme).
type EmotionalTag struct {
	MemoryID  uuid.UUID `json:"memory_id"`
	MemoryTier MemoryTier `json:"memory_tier"`
	Valence   Valence   `json:"valence"`
	Intensity float64   `json:"intensity"`
	Signals   Metadata  `json:"signals"` // detection evidence: {"user_corrections": 5, "retry_count": 3}
	CreatedAt time.Time `json:"created_at"`
}

// ConsolidationPriority computes how urgently this memory should be consolidated.
// High-intensity emotional memories consolidate faster (amygdala-hippocampus interaction).
func (e *EmotionalTag) ConsolidationPriority() float64 {
	weight := 1.0
	switch e.Valence {
	case ValDanger:
		weight = 3.0 // danger memories are never auto-decayed
	case ValSurprise:
		weight = 2.0 // surprising events need integration into world model
	case ValFrustration:
		weight = 1.5 // painful errors should be remembered
	case ValSuccess:
		weight = 1.2 // reinforce successful patterns
	case ValNovelty:
		weight = 1.0
	}
	return e.Intensity * weight
}
