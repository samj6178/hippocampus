package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// PredictionRepo implements domain.PredictionRepo against a SQLite database.
type PredictionRepo struct {
	db *sql.DB
}

// NewPredictionRepo constructs a PredictionRepo backed by db.
func NewPredictionRepo(db *sql.DB) *PredictionRepo {
	return &PredictionRepo{db: db}
}

// Insert stores a new prediction. The embedding blob is encoded via EncodeEmbedding.
func (r *PredictionRepo) Insert(ctx context.Context, pred *domain.Prediction) error {
	var projectID *string
	if pred.ProjectID != nil {
		s := pred.ProjectID.String()
		projectID = &s
	}

	embeddingBlob := EncodeEmbedding(pred.TaskEmbedding)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO predictions
			(id, project_id, agent_id, domain, action,
			 expected_outcome, actual_outcome, confidence,
			 prediction_error, embedding, resolved,
			 created_at, resolved_at)
		VALUES (?, ?, ?, ?, ?, ?, NULL, ?, NULL, ?, 0, ?, NULL)`,
		pred.ID.String(), projectID, pred.AgentID, pred.Domain,
		pred.TaskDescription, pred.PredictedOutput,
		pred.Confidence, embeddingBlob, now,
	)
	if err != nil {
		return fmt.Errorf("prediction insert: %w", err)
	}
	return nil
}

// GetByID returns the prediction with the given ID or domain.ErrNotFound.
func (r *PredictionRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Prediction, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, project_id, agent_id, domain, action,
		       expected_outcome, actual_outcome, confidence,
		       prediction_error, resolved, created_at, resolved_at
		FROM predictions
		WHERE id = ?
		LIMIT 1`, id.String())

	pred, err := scanPrediction(row)
	if err != nil {
		return nil, fmt.Errorf("prediction get by id: %w", err)
	}
	return pred, nil
}

// Resolve marks a prediction as resolved with the given actual outcome and error.
func (r *PredictionRepo) Resolve(ctx context.Context, id uuid.UUID, actualOutcome string, predictionError float64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := r.db.ExecContext(ctx, `
		UPDATE predictions
		SET actual_outcome = ?, prediction_error = ?, resolved = 1, resolved_at = ?
		WHERE id = ? AND resolved = 0`,
		actualOutcome, predictionError, now, id.String())
	if err != nil {
		return fmt.Errorf("resolve prediction: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve prediction rows affected: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ListUnresolved returns all unresolved predictions for the given agent.
func (r *PredictionRepo) ListUnresolved(ctx context.Context, agentID string) ([]*domain.Prediction, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, project_id, agent_id, domain, action,
		       expected_outcome, actual_outcome, confidence,
		       prediction_error, resolved, created_at, resolved_at
		FROM predictions
		WHERE agent_id = ? AND resolved = 0
		ORDER BY created_at DESC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list unresolved predictions: %w", err)
	}
	defer rows.Close()

	return collectPredictions(rows)
}

// GetCalibration computes calibration stats for the given domain and agent from
// resolved predictions. Returns domain.ErrNotFound if no resolved rows exist.
//
// CalibrationOffset = AvgConfidence - AvgActual where AvgActual is the fraction
// of predictions with prediction_error < 0.2 (treated as "correct").
func (r *PredictionRepo) GetCalibration(ctx context.Context, domainName string, agentID string) (*domain.DomainCalibration, error) {
	var (
		cal        domain.DomainCalibration
		sampleCount int
		avgConf    float64
		avgActual  float64
	)

	err := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			AVG(confidence),
			AVG(CASE WHEN prediction_error < 0.2 THEN 1.0 ELSE 0.0 END)
		FROM predictions
		WHERE domain = ? AND agent_id = ? AND resolved = 1`,
		domainName, agentID,
	).Scan(&sampleCount, &avgConf, &avgActual)
	if err != nil {
		return nil, fmt.Errorf("get calibration: %w", err)
	}
	if sampleCount == 0 {
		return nil, domain.ErrNotFound
	}

	cal.Domain = domainName
	cal.SampleCount = sampleCount
	cal.PredictedConfidence = avgConf
	cal.ActualAccuracy = avgActual
	cal.CalibrationOffset = avgConf - avgActual
	return &cal, nil
}

// scanPrediction reads a single predictions row from *sql.Row.
func scanPrediction(row *sql.Row) (*domain.Prediction, error) {
	var (
		pred           domain.Prediction
		idStr          string
		projectIDStr   *string
		actualOutcome  *string
		predError      *float64
		resolved       int
		createdStr     string
		resolvedAtStr  *string
	)

	err := row.Scan(
		&idStr, &projectIDStr, &pred.AgentID, &pred.Domain, &pred.TaskDescription,
		&pred.PredictedOutput, &actualOutcome, &pred.Confidence,
		&predError, &resolved, &createdStr, &resolvedAtStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan prediction: %w", err)
	}

	return parsePredictionFields(&pred, idStr, projectIDStr, actualOutcome,
		predError, resolved, createdStr, resolvedAtStr)
}

// collectPredictions iterates rows and collects predictions.
func collectPredictions(rows *sql.Rows) ([]*domain.Prediction, error) {
	var result []*domain.Prediction
	for rows.Next() {
		var (
			pred           domain.Prediction
			idStr          string
			projectIDStr   *string
			actualOutcome  *string
			predError      *float64
			resolved       int
			createdStr     string
			resolvedAtStr  *string
		)

		err := rows.Scan(
			&idStr, &projectIDStr, &pred.AgentID, &pred.Domain, &pred.TaskDescription,
			&pred.PredictedOutput, &actualOutcome, &pred.Confidence,
			&predError, &resolved, &createdStr, &resolvedAtStr,
		)
		if err != nil {
			return nil, fmt.Errorf("scan prediction row: %w", err)
		}

		parsed, err := parsePredictionFields(&pred, idStr, projectIDStr, actualOutcome,
			predError, resolved, createdStr, resolvedAtStr)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, rows.Err()
}

// parsePredictionFields fills pred from raw scanned values.
func parsePredictionFields(
	pred *domain.Prediction,
	idStr string,
	projectIDStr *string,
	actualOutcome *string,
	predError *float64,
	resolved int,
	createdStr string,
	resolvedAtStr *string,
) (*domain.Prediction, error) {
	var err error

	pred.ID, err = uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("parse prediction id: %w", err)
	}

	if projectIDStr != nil {
		pid, err := uuid.Parse(*projectIDStr)
		if err != nil {
			return nil, fmt.Errorf("parse prediction project_id: %w", err)
		}
		pred.ProjectID = &pid
	}

	if actualOutcome != nil {
		pred.ActualOutcome = *actualOutcome
	}
	if predError != nil {
		pred.PredictionError = *predError
	}

	pred.CreatedAt, err = parseTimestamp(createdStr)
	if err != nil {
		return nil, fmt.Errorf("parse prediction created_at: %w", err)
	}

	if resolvedAtStr != nil {
		t, err := parseTimestamp(*resolvedAtStr)
		if err != nil {
			return nil, fmt.Errorf("parse prediction resolved_at: %w", err)
		}
		pred.ResolvedAt = &t
	}

	return pred, nil
}

// parseTimestamp parses a RFC3339Nano or RFC3339 timestamp string.
func parseTimestamp(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
