package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

type PredictionRepo struct {
	pool *pgxpool.Pool
}

func NewPredictionRepo(pool *pgxpool.Pool) *PredictionRepo {
	return &PredictionRepo{pool: pool}
}

func (r *PredictionRepo) Insert(ctx context.Context, pred *domain.Prediction) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO predictions (id, task_description, task_embedding,
			predicted_outcome, domain, agent_id, project_id, confidence)
		VALUES ($1, $2, $3::vector, $4, $5, $6, $7, $8)`,
		pred.ID, pred.TaskDescription, encodeVector(pred.TaskEmbedding),
		pred.PredictedOutput, pred.Domain, pred.AgentID,
		pred.ProjectID, pred.Confidence,
	)
	if err != nil {
		return fmt.Errorf("prediction insert: %w", err)
	}
	return nil
}

func (r *PredictionRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Prediction, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, task_description, predicted_outcome, actual_outcome,
			prediction_error, domain, agent_id, project_id,
			confidence, created_at, resolved_at
		FROM predictions
		WHERE id = $1`, id)

	return scanPrediction(row)
}

func (r *PredictionRepo) Resolve(ctx context.Context, id uuid.UUID, actualOutcome string, predictionError float64) error {
	now := time.Now()
	tag, err := r.pool.Exec(ctx, `
		UPDATE predictions
		SET actual_outcome = $2, prediction_error = $3, resolved_at = $4
		WHERE id = $1 AND resolved_at IS NULL`, id, actualOutcome, predictionError, now)
	if err != nil {
		return fmt.Errorf("resolve prediction: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PredictionRepo) ListUnresolved(ctx context.Context, agentID string) ([]*domain.Prediction, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, task_description, predicted_outcome, actual_outcome,
			prediction_error, domain, agent_id, project_id,
			confidence, created_at, resolved_at
		FROM predictions
		WHERE agent_id = $1 AND resolved_at IS NULL
		ORDER BY created_at DESC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list unresolved: %w", err)
	}
	defer rows.Close()

	return collectPredictions(rows)
}

func (r *PredictionRepo) GetCalibration(ctx context.Context, domainName string, agentID string) (*domain.DomainCalibration, error) {
	var cal domain.DomainCalibration
	err := r.pool.QueryRow(ctx, `
		SELECT domain, agent_id, sample_count,
			avg_predicted_confidence, avg_actual_accuracy, calibration_offset
		FROM metacognitive_calibration
		WHERE domain = $1 AND agent_id = $2`, domainName, agentID).Scan(
		&cal.Domain, new(string), &cal.SampleCount,
		&cal.PredictedConfidence, &cal.ActualAccuracy, &cal.CalibrationOffset,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get calibration: %w", err)
	}
	return &cal, nil
}

func scanPrediction(row pgx.Row) (*domain.Prediction, error) {
	var p domain.Prediction
	var actualOutcome *string
	var predError *float64

	err := row.Scan(
		&p.ID, &p.TaskDescription, &p.PredictedOutput, &actualOutcome,
		&predError, &p.Domain, &p.AgentID, &p.ProjectID,
		&p.Confidence, &p.CreatedAt, &p.ResolvedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan prediction: %w", err)
	}
	if actualOutcome != nil {
		p.ActualOutcome = *actualOutcome
	}
	if predError != nil {
		p.PredictionError = *predError
	}
	return &p, nil
}

func collectPredictions(rows pgx.Rows) ([]*domain.Prediction, error) {
	var result []*domain.Prediction
	for rows.Next() {
		var p domain.Prediction
		var actualOutcome *string
		var predError *float64

		err := rows.Scan(
			&p.ID, &p.TaskDescription, &p.PredictedOutput, &actualOutcome,
			&predError, &p.Domain, &p.AgentID, &p.ProjectID,
			&p.Confidence, &p.CreatedAt, &p.ResolvedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan prediction: %w", err)
		}
		if actualOutcome != nil {
			p.ActualOutcome = *actualOutcome
		}
		if predError != nil {
			p.PredictionError = *predError
		}
		result = append(result, &p)
	}
	return result, rows.Err()
}
