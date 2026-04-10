package domain

import (
	"time"

	"github.com/google/uuid"
)

// KnowledgeTriple represents a temporal fact in the knowledge graph.
// Facts have validity windows (valid_from/valid_to) enabling point-in-time queries.
// Invalidated facts are soft-deleted: valid_to is set, row is never removed.
type KnowledgeTriple struct {
	ID            uuid.UUID  `json:"id"`
	ProjectID     *uuid.UUID `json:"project_id,omitempty"`
	Subject       string     `json:"subject"`
	Predicate     string     `json:"predicate"`
	Object        string     `json:"object"`
	ValidFrom     time.Time  `json:"valid_from"`
	ValidTo       *time.Time `json:"valid_to,omitempty"` // nil = still valid
	Confidence    float64    `json:"confidence"`
	SourceSession string     `json:"source_session,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// IsValid returns true if the triple is currently valid (not expired).
func (t *KnowledgeTriple) IsValid() bool {
	return t.ValidTo == nil || t.ValidTo.After(time.Now())
}

// IsValidAt returns true if the triple was valid at the given point in time.
func (t *KnowledgeTriple) IsValidAt(at time.Time) bool {
	if at.Before(t.ValidFrom) {
		return false
	}
	return t.ValidTo == nil || t.ValidTo.After(at)
}
