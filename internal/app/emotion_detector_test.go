package app

import (
	"testing"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

func TestEmotionDetector_Detect(t *testing.T) {
	d := EmotionDetector{}

	tests := []struct {
		name     string
		content  string
		wantVals []domain.Valence
		wantNone bool
	}{
		{
			name:     "danger: crash and rollback",
			content:  "Production crash after deployment, had to rollback immediately",
			wantVals: []domain.Valence{domain.ValDanger},
		},
		{
			name:     "frustration: repeated failures",
			content:  "Still not working after third attempt, same error keeps appearing",
			wantVals: []domain.Valence{domain.ValFrustration},
		},
		{
			name:     "success: bug fixed",
			content:  "Bug fixed, all tests pass, deployed successfully",
			wantVals: []domain.Valence{domain.ValSuccess},
		},
		{
			name:     "surprise: unexpected behavior",
			content:  "Unexpected: turns out the function was actually returning cached data, not what I thought",
			wantVals: []domain.Valence{domain.ValSurprise},
		},
		{
			name:     "novelty: new approach",
			content:  "New approach using prototype: first time trying event sourcing in this project",
			wantVals: []domain.Valence{domain.ValNovelty},
		},
		{
			name:     "multiple emotions: danger + frustration",
			content:  "Production crash, tried everything but still not working, data loss confirmed",
			wantVals: []domain.Valence{domain.ValDanger, domain.ValFrustration},
		},
		{
			name:     "neutral content: no emotions",
			content:  "Updated the config file to use port 8080 instead of 3000",
			wantNone: true,
		},
		{
			name:     "error keyword triggers frustration",
			content:  "Got an error when compiling the module",
			wantVals: []domain.Valence{domain.ValFrustration},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emotions := d.Detect(tt.content)

			if tt.wantNone {
				if len(emotions) > 0 {
					t.Errorf("expected no emotions, got %d: %+v", len(emotions), emotions)
				}
				return
			}

			if len(emotions) == 0 {
				t.Errorf("expected emotions %v, got none", tt.wantVals)
				return
			}

			for _, wantVal := range tt.wantVals {
				found := false
				for _, e := range emotions {
					if e.Valence == wantVal {
						found = true
						if e.Intensity <= 0 || e.Intensity > 1.0 {
							t.Errorf("valence %s intensity out of range: %f", wantVal, e.Intensity)
						}
						break
					}
				}
				if !found {
					t.Errorf("expected valence %s not found in %+v", wantVal, emotions)
				}
			}
		})
	}
}

func TestEmotionDetector_IntensityScaling(t *testing.T) {
	d := EmotionDetector{}

	single := d.Detect("Production crash detected")
	multi := d.Detect("Production crash, panic, fatal error, corruption, data loss")

	if len(single) == 0 || len(multi) == 0 {
		t.Fatal("expected emotions for both inputs")
	}

	singleIntensity := single[0].Intensity
	multiIntensity := multi[0].Intensity

	if multiIntensity <= singleIntensity {
		t.Errorf("multiple signals should have higher intensity: single=%f, multi=%f",
			singleIntensity, multiIntensity)
	}
}

func TestEmotionalImportanceBoost(t *testing.T) {
	tests := []struct {
		name    string
		emotions []DetectedEmotion
		wantMin float64
		wantMax float64
	}{
		{
			name:    "no emotions",
			emotions: nil,
			wantMin: 0,
			wantMax: 0,
		},
		{
			name: "danger high intensity",
			emotions: []DetectedEmotion{
				{Valence: domain.ValDanger, Intensity: 1.0},
			},
			wantMin: 0.25,
			wantMax: 0.35,
		},
		{
			name: "success low boost",
			emotions: []DetectedEmotion{
				{Valence: domain.ValSuccess, Intensity: 0.5},
			},
			wantMin: 0.01,
			wantMax: 0.1,
		},
		{
			name: "max of multiple",
			emotions: []DetectedEmotion{
				{Valence: domain.ValSuccess, Intensity: 0.5},
				{Valence: domain.ValDanger, Intensity: 0.8},
			},
			wantMin: 0.2,
			wantMax: 0.3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			boost := emotionalImportanceBoost(tt.emotions)
			if boost < tt.wantMin || boost > tt.wantMax {
				t.Errorf("boost=%f, want [%f, %f]", boost, tt.wantMin, tt.wantMax)
			}
		})
	}
}
