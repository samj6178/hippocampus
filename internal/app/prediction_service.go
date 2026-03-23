package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// PredictionService implements the PREDICT and SURPRISE memory algebra operations.
//
// Neuroscience basis: the hippocampus encodes memories proportionally to
// prediction error (dopamine signal). High surprise → strong encoding.
// Low surprise → weak or no encoding. Over time, the system builds
// calibration curves per domain, enabling meta-cognitive awareness:
// "I know what I don't know."
//
// Mathematical formulation:
//   importance(m) = base_importance × (1 + α × prediction_error)
//   prediction_error = |predicted_confidence - actual_outcome|
//   calibration_offset(domain) = E[predicted] - E[actual]
//
// Where α is the surprise amplification factor (default 1.5).
type PredictionService struct {
	encode    *EncodeService
	embedding domain.EmbeddingProvider
	logger    *slog.Logger

	mu          sync.RWMutex
	predictions map[uuid.UUID]*domain.Prediction
	calibration map[string]*domain.DomainCalibration // domain -> calibration
}

func NewPredictionService(
	encode *EncodeService,
	embedding domain.EmbeddingProvider,
	logger *slog.Logger,
) *PredictionService {
	return &PredictionService{
		encode:      encode,
		embedding:   embedding,
		logger:      logger,
		predictions: make(map[uuid.UUID]*domain.Prediction),
		calibration: make(map[string]*domain.DomainCalibration),
	}
}

type PredictRequest struct {
	Action     string     `json:"action"`
	Expected   string     `json:"expected_outcome"`
	Confidence float64    `json:"confidence"`
	Domain     string     `json:"domain"`
	ProjectID  *uuid.UUID `json:"project_id,omitempty"`
	AgentID    string     `json:"agent_id"`
}

type PredictResponse struct {
	PredictionID     uuid.UUID                `json:"prediction_id"`
	Registered       bool                     `json:"registered"`
	CalibrationWarn  string                   `json:"calibration_warning,omitempty"`
	DomainCalibration *domain.DomainCalibration `json:"domain_calibration,omitempty"`
}

// Predict registers a prediction before an action is taken.
// Returns a calibration warning if the agent is historically overconfident.
func (ps *PredictionService) Predict(ctx context.Context, req *PredictRequest) (*PredictResponse, error) {
	if req.Action == "" {
		return nil, fmt.Errorf("action description is required")
	}
	if req.Confidence <= 0 || req.Confidence > 1.0 {
		req.Confidence = 0.7
	}
	if req.Domain == "" {
		req.Domain = "general"
	}
	if req.AgentID == "" {
		req.AgentID = "cursor"
	}

	pred := &domain.Prediction{
		ID:              uuid.New(),
		TaskDescription: req.Action,
		PredictedOutput: req.Expected,
		Confidence:      req.Confidence,
		Domain:          req.Domain,
		AgentID:         req.AgentID,
		ProjectID:       req.ProjectID,
		CreatedAt:       time.Now(),
	}

	ps.mu.Lock()
	ps.predictions[pred.ID] = pred
	cal := ps.calibration[req.Domain]
	ps.mu.Unlock()

	resp := &PredictResponse{
		PredictionID:      pred.ID,
		Registered:        true,
		DomainCalibration: cal,
	}

	if cal != nil && cal.SampleCount >= 3 {
		if cal.CalibrationOffset > 0.15 {
			resp.CalibrationWarn = fmt.Sprintf(
				"OVERCONFIDENCE WARNING: In '%s' domain, you predict %.0f%% confidence but actual success rate is %.0f%% (based on %d past predictions). Consider lowering confidence or adding safeguards.",
				req.Domain,
				cal.PredictedConfidence*100,
				cal.ActualAccuracy*100,
				cal.SampleCount,
			)
		} else if cal.CalibrationOffset < -0.15 {
			resp.CalibrationWarn = fmt.Sprintf(
				"UNDERCONFIDENCE: In '%s' domain, you predict %.0f%% confidence but actual success rate is %.0f%%. You're better than you think!",
				req.Domain,
				cal.PredictedConfidence*100,
				cal.ActualAccuracy*100,
			)
		}
	}

	ps.logger.Info("prediction registered",
		"id", pred.ID,
		"domain", req.Domain,
		"confidence", req.Confidence,
		"calibration_warn", resp.CalibrationWarn != "",
	)

	return resp, nil
}

type ResolveRequest struct {
	PredictionID uuid.UUID `json:"prediction_id"`
	Outcome      string    `json:"actual_outcome"`
	Success      bool      `json:"success"`
}

type ResolveResponse struct {
	PredictionError   float64 `json:"prediction_error"`
	SurpriseLevel     string  `json:"surprise_level"` // "expected", "surprising", "shocking"
	MemoryImportance  float64 `json:"memory_importance"`
	MemoryID          *uuid.UUID `json:"memory_id,omitempty"`
	CalibrationUpdate string  `json:"calibration_update"`
	LearningSignal    string  `json:"learning_signal"`
}

// Resolve compares prediction with reality, computes prediction error,
// and uses it as a learning signal for memory encoding.
//
// This is the core of the dopamine prediction error theory:
//   δ = r - V(s)   (actual reward minus predicted value)
//   if δ > 0: positive surprise → strengthen memory
//   if δ < 0: negative surprise → strengthen memory (learn from failure)
//   if δ ≈ 0: expected → weak encoding
func (ps *PredictionService) Resolve(ctx context.Context, req *ResolveRequest) (*ResolveResponse, error) {
	ps.mu.Lock()
	pred, ok := ps.predictions[req.PredictionID]
	if !ok {
		ps.mu.Unlock()
		return nil, fmt.Errorf("prediction %s not found (may have expired)", req.PredictionID)
	}
	delete(ps.predictions, req.PredictionID)
	ps.mu.Unlock()

	actualValue := 0.0
	if req.Success {
		actualValue = 1.0
	}

	predictionError := math.Abs(pred.Confidence - actualValue)

	now := time.Now()
	pred.ActualOutcome = req.Outcome
	pred.PredictionError = predictionError
	pred.ResolvedAt = &now

	surpriseLevel := classifySurprise(predictionError)

	const surpriseAmplification = 1.5
	memoryImportance := 0.5 * (1.0 + surpriseAmplification*predictionError)
	if memoryImportance > 1.0 {
		memoryImportance = 1.0
	}
	if memoryImportance < 0.3 {
		memoryImportance = 0.3
	}

	ps.updateCalibration(pred)

	resp := &ResolveResponse{
		PredictionError:  predictionError,
		SurpriseLevel:    surpriseLevel,
		MemoryImportance: memoryImportance,
	}

	if predictionError > 0.2 {
		content := fmt.Sprintf(
			"PREDICTION ERROR (δ=%.2f, %s):\n"+
				"Action: %s\n"+
				"Predicted: %s (confidence: %.0f%%)\n"+
				"Actual: %s (success: %v)\n"+
				"Domain: %s\n"+
				"Learning: %s",
			predictionError, surpriseLevel,
			pred.TaskDescription,
			pred.PredictedOutput, pred.Confidence*100,
			req.Outcome, req.Success,
			pred.Domain,
			explainLearning(pred, req.Success),
		)

		tags := []string{"prediction_error", pred.Domain, surpriseLevel}
		if !req.Success {
			tags = append(tags, "error", "learned_pattern")
		}

		encResp, err := ps.encode.Encode(ctx, &EncodeRequest{
			Content:    content,
			ProjectID:  pred.ProjectID,
			AgentID:    pred.AgentID,
			Importance: memoryImportance,
			Tags:       tags,
		})
		if err != nil {
			ps.logger.Warn("failed to encode prediction error", "error", err)
		} else if encResp.Encoded {
			resp.MemoryID = &encResp.MemoryID
		}
	}

	ps.mu.RLock()
	cal := ps.calibration[pred.Domain]
	ps.mu.RUnlock()

	if cal != nil {
		resp.CalibrationUpdate = fmt.Sprintf(
			"Domain '%s': %d predictions, avg predicted confidence %.0f%%, actual accuracy %.0f%%, calibration offset %+.0f%%",
			pred.Domain, cal.SampleCount,
			cal.PredictedConfidence*100,
			cal.ActualAccuracy*100,
			cal.CalibrationOffset*100,
		)
	}

	resp.LearningSignal = fmt.Sprintf(
		"Prediction error δ=%.2f → memory importance %.2f (surprise amplification %.1fx). %s",
		predictionError, memoryImportance, surpriseAmplification,
		explainLearning(pred, req.Success),
	)

	ps.logger.Info("prediction resolved",
		"id", pred.ID,
		"domain", pred.Domain,
		"prediction_error", predictionError,
		"surprise", surpriseLevel,
		"memory_importance", memoryImportance,
	)

	return resp, nil
}

func (ps *PredictionService) updateCalibration(pred *domain.Prediction) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	cal, ok := ps.calibration[pred.Domain]
	if !ok {
		cal = &domain.DomainCalibration{Domain: pred.Domain}
		ps.calibration[pred.Domain] = cal
	}

	actual := 0.0
	if pred.PredictionError < 0.5 {
		actual = 1.0
	}

	n := float64(cal.SampleCount)
	cal.PredictedConfidence = (cal.PredictedConfidence*n + pred.Confidence) / (n + 1)
	cal.ActualAccuracy = (cal.ActualAccuracy*n + actual) / (n + 1)
	cal.CalibrationOffset = cal.PredictedConfidence - cal.ActualAccuracy
	cal.SampleCount++
}

// GetCalibration returns calibration data for all domains.
func (ps *PredictionService) GetCalibration() map[string]*domain.DomainCalibration {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	result := make(map[string]*domain.DomainCalibration, len(ps.calibration))
	for k, v := range ps.calibration {
		cp := *v
		result[k] = &cp
	}
	return result
}

// PendingCount returns the number of unresolved predictions.
func (ps *PredictionService) PendingCount() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.predictions)
}

func classifySurprise(error float64) string {
	switch {
	case error < 0.2:
		return "expected"
	case error < 0.5:
		return "surprising"
	default:
		return "shocking"
	}
}

func explainLearning(pred *domain.Prediction, success bool) string {
	if success && pred.Confidence < 0.5 {
		return "Unexpected success — model was pessimistic. Updating upward."
	}
	if !success && pred.Confidence > 0.7 {
		return "Unexpected failure — model was overconfident. This is a high-value learning signal."
	}
	if success && pred.Confidence > 0.8 {
		return "Expected success — prediction was well-calibrated."
	}
	if !success && pred.Confidence < 0.3 {
		return "Expected failure — prediction was well-calibrated."
	}
	return "Moderate surprise — updating model calibration."
}
