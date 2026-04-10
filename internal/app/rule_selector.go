package app

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// ABTrial tracks an active comparison between two rule versions.
type ABTrial struct {
	RuleID        uuid.UUID `json:"rule_id"`
	OldVersion    int       `json:"old_version"`
	NewVersion    int       `json:"new_version"`
	SessionsSeen  int       `json:"sessions_seen"`
	RequiredSessions int    `json:"required_sessions"`
	StartedAt     time.Time `json:"started_at"`
}

// RuleSelector manages A/B trials between rule versions.
// After sufficient sessions, it picks the winner and deprecates the loser.
type RuleSelector struct {
	tracker *RuleTracker

	mu     sync.RWMutex
	trials map[uuid.UUID]*ABTrial
}

func NewRuleSelector(tracker *RuleTracker) *RuleSelector {
	return &RuleSelector{
		tracker: tracker,
		trials:  make(map[uuid.UUID]*ABTrial),
	}
}

// StartTrial begins an A/B trial for a mutated rule.
func (rs *RuleSelector) StartTrial(ruleID uuid.UUID, oldVersion, newVersion int) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.trials[ruleID] = &ABTrial{
		RuleID:           ruleID,
		OldVersion:       oldVersion,
		NewVersion:       newVersion,
		RequiredSessions: 5,
		StartedAt:        time.Now(),
	}
}

// RecordSession increments the session counter for all active trials.
func (rs *RuleSelector) RecordSession() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, trial := range rs.trials {
		trial.SessionsSeen++
	}
}

// CompletedTrials returns trials that have enough data to decide a winner.
func (rs *RuleSelector) CompletedTrials() []*ABTrial {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	var result []*ABTrial
	for _, trial := range rs.trials {
		if trial.SessionsSeen >= trial.RequiredSessions {
			cp := *trial
			result = append(result, &cp)
		}
	}
	return result
}

// ResolveTrial decides the winner. The new version wins if the current
// effectiveness score is >= 0.5 (the mutation improved things).
// Returns true if the new version won.
func (rs *RuleSelector) ResolveTrial(ruleID uuid.UUID) bool {
	stats := rs.tracker.Stats()
	eff, ok := stats[ruleID]
	if !ok {
		rs.removeTrial(ruleID)
		return false
	}

	// New version wins if score >= 0.5 (better than random)
	won := eff.Score >= 0.5

	rs.removeTrial(ruleID)
	return won
}

func (rs *RuleSelector) removeTrial(ruleID uuid.UUID) {
	rs.mu.Lock()
	delete(rs.trials, ruleID)
	rs.mu.Unlock()
}

// ActiveTrialCount returns the number of ongoing A/B trials.
func (rs *RuleSelector) ActiveTrialCount() int {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return len(rs.trials)
}
