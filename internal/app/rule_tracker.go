package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// RuleEffectiveness tracks how well a prevention rule performs.
type RuleEffectiveness struct {
	RuleID         uuid.UUID `json:"rule_id"`
	Version        int       `json:"version"`
	ExposureCount  int       `json:"exposure_count"`
	PreventedCount int       `json:"prevented_count"`
	IgnoredCount   int       `json:"ignored_count"`
	FalsePositives int       `json:"false_positives"`
	Score          float64   `json:"score"`
}

// RuleTracker records and persists rule effectiveness data.
// Effectiveness is stored in the rule's SemanticMemory metadata field
// under the key "rule_effectiveness".
type RuleTracker struct {
	semantic domain.SemanticRepo
	logger   *slog.Logger

	mu    sync.RWMutex
	stats map[uuid.UUID]*RuleEffectiveness
}

func NewRuleTracker(semantic domain.SemanticRepo, logger *slog.Logger) *RuleTracker {
	return &RuleTracker{
		semantic: semantic,
		logger:   logger,
		stats:    make(map[uuid.UUID]*RuleEffectiveness),
	}
}

func (rt *RuleTracker) get(id uuid.UUID) *RuleEffectiveness {
	e, ok := rt.stats[id]
	if !ok {
		e = &RuleEffectiveness{RuleID: id, Version: 1}
		rt.stats[id] = e
	}
	return e
}

func (rt *RuleTracker) recomputeScore(e *RuleEffectiveness) {
	total := e.PreventedCount + e.IgnoredCount + e.FalsePositives
	if total == 0 {
		e.Score = 0
		return
	}
	e.Score = float64(e.PreventedCount) / float64(total)
}

// RecordExposure increments exposure count for each rule shown to the agent.
func (rt *RuleTracker) RecordExposure(ruleIDs []uuid.UUID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, id := range ruleIDs {
		e := rt.get(id)
		e.ExposureCount++
	}
}

// RecordOutcome records whether a rule prevented or was ignored (from PreventionAnalyzer).
func (rt *RuleTracker) RecordOutcome(ruleID uuid.UUID, prevented bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	e := rt.get(ruleID)
	if prevented {
		e.PreventedCount++
	} else {
		e.IgnoredCount++
	}
	rt.recomputeScore(e)
}

// RecordFalsePositive records when a user marks a rule-based recall as not useful.
func (rt *RuleTracker) RecordFalsePositive(ruleID uuid.UUID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	e := rt.get(ruleID)
	e.FalsePositives++
	rt.recomputeScore(e)
}

// GetLowScoreRules returns rules with enough data (>=5 exposures) and score < threshold.
func (rt *RuleTracker) GetLowScoreRules(threshold float64) []*RuleEffectiveness {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	var result []*RuleEffectiveness
	for _, e := range rt.stats {
		if e.ExposureCount >= 5 && e.Score < threshold {
			cp := *e
			result = append(result, &cp)
		}
	}
	return result
}

// Stats returns a snapshot of all tracked rules.
func (rt *RuleTracker) Stats() map[uuid.UUID]*RuleEffectiveness {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	result := make(map[uuid.UUID]*RuleEffectiveness, len(rt.stats))
	for id, e := range rt.stats {
		cp := *e
		result[id] = &cp
	}
	return result
}

// Persist writes effectiveness data into each rule's semantic memory metadata.
func (rt *RuleTracker) Persist(ctx context.Context) {
	rt.mu.RLock()
	snapshot := make(map[uuid.UUID]*RuleEffectiveness, len(rt.stats))
	for id, e := range rt.stats {
		cp := *e
		snapshot[id] = &cp
	}
	rt.mu.RUnlock()

	for id, eff := range snapshot {
		mem, err := rt.semantic.GetByID(ctx, id)
		if err != nil {
			continue
		}
		if mem.Metadata == nil {
			mem.Metadata = make(domain.Metadata)
		}
		effJSON, _ := json.Marshal(eff)
		var effMap map[string]any
		json.Unmarshal(effJSON, &effMap)
		mem.Metadata["rule_effectiveness"] = effMap
		if err := rt.semantic.Update(ctx, mem); err != nil {
			rt.logger.Warn("persist rule effectiveness failed", "rule_id", id, "error", err)
		}
	}
}

// LoadFromMetadata restores effectiveness data from semantic memory metadata.
func (rt *RuleTracker) LoadFromMetadata(ctx context.Context, projectID *uuid.UUID) {
	rules, err := rt.semantic.ListByEntityType(ctx, projectID, "rule", 500)
	if err != nil {
		rt.logger.Warn("load rule effectiveness failed", "error", err)
		return
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, rule := range rules {
		if rule.Metadata == nil {
			continue
		}
		raw, ok := rule.Metadata["rule_effectiveness"]
		if !ok {
			continue
		}
		data, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var eff RuleEffectiveness
		if err := json.Unmarshal(data, &eff); err != nil {
			continue
		}
		eff.RuleID = rule.ID
		rt.stats[rule.ID] = &eff
	}

	rt.logger.Info("rule effectiveness loaded", "rules_tracked", len(rt.stats))
}

// EvolutionStats returns aggregate stats for mos_metrics.
func (rt *RuleTracker) EvolutionStats() map[string]any {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	var total, evolved int
	var sumScore float64
	var withData int

	for _, e := range rt.stats {
		total++
		if e.Version > 1 {
			evolved++
		}
		if e.ExposureCount >= 5 {
			sumScore += e.Score
			withData++
		}
	}

	avgRate := 0.0
	if withData > 0 {
		avgRate = sumScore / float64(withData)
	}

	return map[string]any{
		"total_rules":         total,
		"evolved_rules":       evolved,
		"avg_prevention_rate": avgRate,
		"rules_with_data":     withData,
	}
}
