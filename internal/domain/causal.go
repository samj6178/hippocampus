package domain

import (
	"time"

	"github.com/google/uuid"
)

// CausalRelation types between memories.
type CausalRelation string

const (
	RelCaused    CausalRelation = "caused"
	RelPrevented CausalRelation = "prevented"
	RelEnabled   CausalRelation = "enabled"
	RelDegraded  CausalRelation = "degraded"
	RelRequired  CausalRelation = "required"
)

// CausalLink represents a directed causal relationship between two memories.
// These form a causal graph enabling reasoning about interventions:
// "if I change X, what happens to Y?"
type CausalLink struct {
	ID               uuid.UUID      `json:"id"`
	CauseID          uuid.UUID      `json:"cause_id"`
	CauseTier        MemoryTier     `json:"cause_tier"`
	EffectID         uuid.UUID      `json:"effect_id"`
	EffectTier       MemoryTier     `json:"effect_tier"`
	Relation         CausalRelation `json:"relation_type"`
	Confidence       float64        `json:"confidence"`
	Evidence         []uuid.UUID    `json:"evidence_episodes,omitempty"`
	CounterEvidence  []uuid.UUID    `json:"counter_evidence,omitempty"`
	BoundaryConditions string       `json:"boundary_conditions,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// NetEvidence returns the balance of supporting vs contradicting episodes.
func (c *CausalLink) NetEvidence() int {
	return len(c.Evidence) - len(c.CounterEvidence)
}
