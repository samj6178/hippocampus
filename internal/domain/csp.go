package domain

import (
	"time"

	"github.com/google/uuid"
)

// CSPMessageType defines the types of messages in the Cognitive State Protocol.
// CSP is the universal interface for cognitive state exchange between agents.
type CSPMessageType string

const (
	CSPExperience  CSPMessageType = "experience"
	CSPFact        CSPMessageType = "fact"
	CSPQuery       CSPMessageType = "query"
	CSPPrediction  CSPMessageType = "prediction"
	CSPCorrection  CSPMessageType = "correction"
)

// CSPMessage is the universal message format for inter-agent communication
// through MOS shared memory. Agents never communicate directly — all
// messages flow through MOS for auditability, conflict prevention, and safety.
type CSPMessage struct {
	ID         uuid.UUID      `json:"id"`
	Sender     string         `json:"sender"`     // agent_id
	Type       CSPMessageType `json:"type"`
	Content    string         `json:"content"`
	Confidence float64        `json:"confidence"`  // calibrated, not self-reported
	Evidence   []uuid.UUID    `json:"evidence,omitempty"`

	CausalContext struct {
		CauseOf  []uuid.UUID `json:"cause_of,omitempty"`
		CausedBy []uuid.UUID `json:"caused_by,omitempty"`
	} `json:"causal_context"`

	Temporal struct {
		ValidFrom  time.Time   `json:"valid_from"`
		ValidUntil *time.Time  `json:"valid_until,omitempty"` // nil = permanent
		Supersedes []uuid.UUID `json:"supersedes,omitempty"`
	} `json:"temporal"`

	CreatedAt time.Time `json:"created_at"`
}
