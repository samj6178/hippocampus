package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

func TestClassifySurprise(t *testing.T) {
	tests := []struct {
		predErr  float64
		expected string
	}{
		{0.0, "expected"},
		{0.1, "expected"},
		{0.19, "expected"},
		{0.2, "surprising"},
		{0.3, "surprising"},
		{0.49, "surprising"},
		{0.5, "shocking"},
		{0.8, "shocking"},
		{1.0, "shocking"},
	}
	for _, tt := range tests {
		got := classifySurprise(tt.predErr)
		if got != tt.expected {
			t.Errorf("classifySurprise(%f) = %q, want %q", tt.predErr, got, tt.expected)
		}
	}
}

func TestExplainLearning(t *testing.T) {
	pred := &domain.Prediction{
		TaskDescription: "deploy to production",
		PredictedOutput: "success without issues",
		Confidence:      0.9,
	}

	t.Run("success", func(t *testing.T) {
		got := explainLearning(pred, true)
		if got == "" {
			t.Error("expected non-empty explanation")
		}
	})

	t.Run("failure", func(t *testing.T) {
		got := explainLearning(pred, false)
		if got == "" {
			t.Error("expected non-empty explanation")
		}
	})
}

func TestPredictionService_PredictAndResolve(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	mockEmb := &mockEmbedding{
		embeddings: [][]float32{{0.1, 0.2, 0.3}},
	}
	mockEpRepo := &mockEpisodicRepo{}
	mockEmotionalRepo := &mockEmotionalTagRepo{}

	encodeSvc := NewEncodeService(
		mockEpRepo, mockEmotionalRepo, mockEmb, nil,
		EncodeServiceConfig{GateThreshold: 0.3}, logger,
	)

	ps := NewPredictionService(encodeSvc, mockEmb, logger)

	resp, err := ps.Predict(context.Background(), &PredictRequest{
		Action:     "compile project",
		Expected:   "compiles without errors",
		Confidence: 0.85,
		Domain:     "go-compilation",
		AgentID:    "test-agent",
	})
	if err != nil {
		t.Fatalf("Predict failed: %v", err)
	}
	if resp.PredictionID == uuid.Nil {
		t.Error("expected non-nil prediction ID")
	}

	if ps.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", ps.PendingCount())
	}

	resolveResp, err := ps.Resolve(context.Background(), &ResolveRequest{
		PredictionID: resp.PredictionID,
		Outcome:      "compiled successfully",
		Success:      true,
	})
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if resolveResp.PredictionError < 0 || resolveResp.PredictionError > 1 {
		t.Errorf("prediction_error out of range: %f", resolveResp.PredictionError)
	}

	if ps.PendingCount() != 0 {
		t.Errorf("expected 0 pending after resolve, got %d", ps.PendingCount())
	}
}

func TestPredictionService_ResolveNonExistent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ps := NewPredictionService(nil, nil, logger)

	_, err := ps.Resolve(context.Background(), &ResolveRequest{
		PredictionID: uuid.New(),
		Outcome:      "something",
		Success:      true,
	})
	if err == nil {
		t.Error("expected error for non-existent prediction")
	}
}

func TestPredictionService_GetCalibration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockEmb := &mockEmbedding{embeddings: [][]float32{{0.1, 0.2, 0.3}}}
	mockEpRepo := &mockEpisodicRepo{}
	mockEmotionalRepo := &mockEmotionalTagRepo{}
	encodeSvc := NewEncodeService(mockEpRepo, mockEmotionalRepo, mockEmb, nil,
		EncodeServiceConfig{GateThreshold: 0.3}, logger)

	ps := NewPredictionService(encodeSvc, mockEmb, logger)

	resp, _ := ps.Predict(context.Background(), &PredictRequest{
		Action: "test action", Expected: "success", Confidence: 0.9,
		Domain: "test-domain", AgentID: "test",
	})
	ps.Resolve(context.Background(), &ResolveRequest{
		PredictionID: resp.PredictionID, Outcome: "success", Success: true,
	})

	cal := ps.GetCalibration()
	if cal == nil {
		t.Fatal("calibration should not be nil")
	}
	if _, ok := cal["test-domain"]; !ok {
		t.Error("expected calibration for test-domain")
	}

	// Verify copy-on-read
	cal["test-domain"] = nil
	cal2 := ps.GetCalibration()
	if cal2["test-domain"] == nil {
		t.Error("calibration should be copy-on-read")
	}
}

func TestPredictionService_DefaultValues(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockEmb := &mockEmbedding{embeddings: [][]float32{{0.1, 0.2, 0.3}}}
	mockEpRepo := &mockEpisodicRepo{}
	mockEmotionalRepo := &mockEmotionalTagRepo{}
	encodeSvc := NewEncodeService(mockEpRepo, mockEmotionalRepo, mockEmb, nil,
		EncodeServiceConfig{GateThreshold: 0.3}, logger)

	ps := NewPredictionService(encodeSvc, mockEmb, logger)

	resp, err := ps.Predict(context.Background(), &PredictRequest{
		Action:   "test",
		Expected: "result",
	})
	if err != nil {
		t.Fatalf("Predict failed: %v", err)
	}
	if resp.PredictionID == uuid.Nil {
		t.Error("should assign ID even with defaults")
	}
}

// Mock for EmotionalTagRepo
type mockEmotionalTagRepo struct{}

func (m *mockEmotionalTagRepo) Insert(_ context.Context, _ *domain.EmotionalTag) error { return nil }
func (m *mockEmotionalTagRepo) GetByMemory(_ context.Context, _ uuid.UUID) ([]*domain.EmotionalTag, error) {
	return nil, nil
}
func (m *mockEmotionalTagRepo) GetHighPriority(_ context.Context, _ *uuid.UUID, _ int) ([]*domain.EmotionalTag, error) {
	return nil, nil
}
