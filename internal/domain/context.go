package domain

// AssembledContext is the final output of the Context Builder (RECALL operation).
// It represents the optimal selection of memories for a given task and token budget,
// formatted for injection into an LLM prompt.
type AssembledContext struct {
	Text       string         `json:"text"`
	Sources    []RecallSource `json:"sources"`
	TokenCount int            `json:"token_count"`
	Confidence float64        `json:"confidence"`
}

// TokenBudget defines constraints for context assembly.
type TokenBudget struct {
	Total     int `json:"total"`
	Semantic  int `json:"semantic,omitempty"`  // max tokens for semantic memories
	Episodic  int `json:"episodic,omitempty"`  // max tokens for episodic memories
	Procedural int `json:"procedural,omitempty"` // max tokens for procedural memories
}

// DefaultBudget returns a reasonable token budget for standard context assembly.
func DefaultBudget() TokenBudget {
	return TokenBudget{
		Total:      4096,
		Semantic:   1500,
		Episodic:   1500,
		Procedural: 600,
	}
}
