package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// MutationStrategy determines how an underperforming rule is improved.
type MutationStrategy string

const (
	StrategyNarrowScope     MutationStrategy = "narrow_scope"      // too many false positives
	StrategyImproveClarity  MutationStrategy = "improve_clarity"   // too many ignores
	StrategyRefineAntipattern MutationStrategy = "refine_antipattern" // default
)

// RuleMutation records a rule version change.
type RuleMutation struct {
	ID          uuid.UUID        `json:"id"`
	RuleID      uuid.UUID        `json:"rule_id"`
	FromVersion int              `json:"from_version"`
	ToVersion   int              `json:"to_version"`
	Strategy    MutationStrategy `json:"strategy"`
	CreatedAt   time.Time        `json:"created_at"`
}

// RuleMutator evolves underperforming rules via LLM-delegated mutation.
type RuleMutator struct {
	tracker  *RuleTracker
	semantic domain.SemanticRepo
	llm      domain.LLMProvider
	logger   *slog.Logger
}

func NewRuleMutator(tracker *RuleTracker, semantic domain.SemanticRepo, llm domain.LLMProvider, logger *slog.Logger) *RuleMutator {
	return &RuleMutator{
		tracker:  tracker,
		semantic: semantic,
		llm:      llm,
		logger:   logger,
	}
}

// selectStrategy picks mutation approach based on failure pattern.
func selectStrategy(eff *RuleEffectiveness) MutationStrategy {
	if eff.FalsePositives > eff.PreventedCount {
		return StrategyNarrowScope
	}
	if eff.IgnoredCount > eff.PreventedCount {
		return StrategyImproveClarity
	}
	return StrategyRefineAntipattern
}

// MutateUnderperformingRules finds low-score rules and generates mutations.
// Uses two-phase LLM delegation: returns pending tasks when no LLM available.
func (rm *RuleMutator) MutateUnderperformingRules(ctx context.Context) ([]RuleMutation, []PendingTask) {
	lowScore := rm.tracker.GetLowScoreRules(0.5)
	if len(lowScore) == 0 {
		return nil, nil
	}

	var mutations []RuleMutation
	var pending []PendingTask

	for _, eff := range lowScore {
		rule, err := rm.semantic.GetByID(ctx, eff.RuleID)
		if err != nil {
			continue
		}

		strategy := selectStrategy(eff)
		prompt := rm.buildMutationPrompt(rule, eff, strategy)

		if rm.llm != nil && rm.llm.IsAvailable(ctx) {
			mutated, err := rm.llm.Chat(ctx, []domain.ChatMessage{
				{Role: "system", Content: "You are a code quality expert. Improve the given prevention rule based on its performance data."},
				{Role: "user", Content: prompt},
			}, domain.ChatOptions{Temperature: 0.3, MaxTokens: 500})
			if err != nil {
				rm.logger.Warn("rule mutation LLM failed", "rule_id", eff.RuleID, "error", err)
				continue
			}

			if err := rm.applyMutation(ctx, rule, eff, strategy, mutated); err != nil {
				rm.logger.Warn("apply mutation failed", "rule_id", eff.RuleID, "error", err)
				continue
			}

			mutations = append(mutations, RuleMutation{
				ID:          uuid.New(),
				RuleID:      eff.RuleID,
				FromVersion: eff.Version,
				ToVersion:   eff.Version + 1,
				Strategy:    strategy,
				CreatedAt:   time.Now(),
			})
		} else {
			pending = append(pending, PendingTask{
				ID:   uuid.New().String(),
				Type: "mutate_rule",
				Prompt: prompt,
				Metadata: map[string]any{
					"rule_id":  eff.RuleID.String(),
					"strategy": string(strategy),
					"version":  eff.Version,
				},
			})
		}
	}

	return mutations, pending
}

func (rm *RuleMutator) applyMutation(ctx context.Context, rule *domain.SemanticMemory, eff *RuleEffectiveness, strategy MutationStrategy, newContent string) error {
	if strings.TrimSpace(newContent) == "" {
		return fmt.Errorf("empty mutation result")
	}

	rule.Content = newContent
	if rule.Metadata == nil {
		rule.Metadata = make(domain.Metadata)
	}
	rule.Metadata["mutation_history"] = append(
		metadataSlice(rule.Metadata, "mutation_history"),
		map[string]any{
			"from_version": eff.Version,
			"to_version":   eff.Version + 1,
			"strategy":     string(strategy),
			"timestamp":    time.Now().Format(time.RFC3339),
		},
	)
	rule.UpdatedAt = time.Now()

	// Reset effectiveness counters for the new version
	rm.tracker.mu.Lock()
	if e, ok := rm.tracker.stats[eff.RuleID]; ok {
		e.Version = eff.Version + 1
		e.ExposureCount = 0
		e.PreventedCount = 0
		e.IgnoredCount = 0
		e.FalsePositives = 0
		e.Score = 0
	}
	rm.tracker.mu.Unlock()

	return rm.semantic.Update(ctx, rule)
}

func (rm *RuleMutator) buildMutationPrompt(rule *domain.SemanticMemory, eff *RuleEffectiveness, strategy MutationStrategy) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Rule Mutation Request (strategy: %s)\n\n", strategy))
	b.WriteString(fmt.Sprintf("### Current Rule (v%d)\n%s\n\n", eff.Version, rule.Content))
	b.WriteString(fmt.Sprintf("### Performance Data\n"))
	b.WriteString(fmt.Sprintf("- Exposures: %d\n", eff.ExposureCount))
	b.WriteString(fmt.Sprintf("- Prevented: %d\n", eff.PreventedCount))
	b.WriteString(fmt.Sprintf("- Ignored: %d\n", eff.IgnoredCount))
	b.WriteString(fmt.Sprintf("- False Positives: %d\n", eff.FalsePositives))
	b.WriteString(fmt.Sprintf("- Score: %.2f\n\n", eff.Score))

	switch strategy {
	case StrategyNarrowScope:
		b.WriteString("### Problem: Too many false positives.\n")
		b.WriteString("Make the WHEN clause more specific. Narrow the scope to reduce false matches.\n")
	case StrategyImproveClarity:
		b.WriteString("### Problem: Agents see the warning but still introduce the bug.\n")
		b.WriteString("Make the DO instruction more actionable. Add a concrete code example.\n")
	case StrategyRefineAntipattern:
		b.WriteString("### Problem: The ANTIPATTERN regex may be too broad or too narrow.\n")
		b.WriteString("Refine the regex to better catch the actual bug pattern.\n")
	}

	b.WriteString("\nRespond with ONLY the improved rule in WHEN/WATCH/BECAUSE/DO/ANTIPATTERN format.")
	return b.String()
}

func metadataSlice(m domain.Metadata, key string) []any {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	if s, ok := raw.([]any); ok {
		return s
	}
	return nil
}
