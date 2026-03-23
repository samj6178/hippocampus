package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMemoryTierConstants(t *testing.T) {
	tiers := []MemoryTier{TierWorking, TierEpisodic, TierSemantic, TierProcedural, TierCausal}
	seen := make(map[MemoryTier]bool)
	for _, tier := range tiers {
		if seen[tier] {
			t.Errorf("duplicate tier: %s", tier)
		}
		seen[tier] = true
		if tier == "" {
			t.Error("empty tier constant")
		}
	}
}

func TestProceduralMemory_SuccessRate(t *testing.T) {
	tests := []struct {
		name    string
		success int
		failure int
		want    float64
	}{
		{"zero total", 0, 0, 0},
		{"all success", 10, 0, 1.0},
		{"all failure", 0, 5, 0},
		{"mixed", 7, 3, 0.7},
		{"single success", 1, 0, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ProceduralMemory{
				SuccessCount: tt.success,
				FailureCount: tt.failure,
			}
			got := p.SuccessRate()
			if got != tt.want {
				t.Errorf("SuccessRate() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestImportanceScore_Composite(t *testing.T) {
	score := ImportanceScore{
		SemanticSimilarity: 0.8,
		Recency:            0.6,
		ExplicitImportance: 0.9,
		EmotionalIntensity: 0.5,
	}

	if score.SemanticSimilarity != 0.8 {
		t.Error("semantic similarity mismatch")
	}
	if score.Recency != 0.6 {
		t.Error("recency mismatch")
	}
}

func TestCausalLink_NetEvidence(t *testing.T) {
	tests := []struct {
		name     string
		evidence int
		counter  int
		want     int
	}{
		{"all supporting", 5, 0, 5},
		{"all counter", 0, 3, -3},
		{"mixed positive", 7, 2, 5},
		{"balanced", 3, 3, 0},
		{"empty", 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := CausalLink{
				Evidence:        make([]uuid.UUID, tt.evidence),
				CounterEvidence: make([]uuid.UUID, tt.counter),
			}
			got := link.NetEvidence()
			if got != tt.want {
				t.Errorf("NetEvidence() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEmotionalTag_ConsolidationPriority(t *testing.T) {
	tests := []struct {
		valence   Valence
		intensity float64
		wantMin   float64
	}{
		{ValDanger, 1.0, 2.5},
		{ValSurprise, 1.0, 1.5},
		{ValFrustration, 1.0, 1.0},
		{ValSuccess, 1.0, 1.0},
		{ValNovelty, 1.0, 0.5},
		{ValDanger, 0.0, 0.0},
	}

	for _, tt := range tests {
		t.Run(string(tt.valence), func(t *testing.T) {
			tag := EmotionalTag{Valence: tt.valence, Intensity: tt.intensity}
			got := tag.ConsolidationPriority()
			if got < tt.wantMin {
				t.Errorf("ConsolidationPriority() = %f, want >= %f", got, tt.wantMin)
			}
		})
	}

	danger := EmotionalTag{Valence: ValDanger, Intensity: 1.0}
	novelty := EmotionalTag{Valence: ValNovelty, Intensity: 1.0}
	if danger.ConsolidationPriority() <= novelty.ConsolidationPriority() {
		t.Error("danger should have higher priority than novelty")
	}
}

func TestPrediction_SurpriseLevel(t *testing.T) {
	tests := []struct {
		predError float64
		sigma     float64
		want      string
	}{
		{0.1, 0.3, "expected"},
		{0.2, 0.3, "mild"},
		{0.4, 0.3, "surprising"},
		{0.8, 0.3, "shocking"},
		{0.0, 0.3, "expected"},
		{0.5, 0.0, "surprising"}, // sigma=0 defaults to 0.3, 0.5/0.3=1.67
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			p := Prediction{PredictionError: tt.predError}
			got := p.SurpriseLevel(tt.sigma)
			if got != tt.want {
				t.Errorf("SurpriseLevel(%f, %f) = %q, want %q", tt.predError, tt.sigma, got, tt.want)
			}
		})
	}
}

func TestPrediction_IsResolved(t *testing.T) {
	p := Prediction{}
	if p.IsResolved() {
		t.Error("unresolved prediction should return false")
	}

	now := time.Now()
	p.ResolvedAt = &now
	if !p.IsResolved() {
		t.Error("resolved prediction should return true")
	}
}
