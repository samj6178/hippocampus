package repo

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

type MetaCognitiveRepo struct {
	pool *pgxpool.Pool
}

func NewMetaCognitiveRepo(pool *pgxpool.Pool) *MetaCognitiveRepo {
	return &MetaCognitiveRepo{pool: pool}
}

func (r *MetaCognitiveRepo) Insert(ctx context.Context, entry *domain.MetaCognitiveEntry) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO metacognitive_log (id, domain, predicted_confidence,
			actual_accuracy, strategy_used, strategy_outcome, agent_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.ID, entry.Domain, entry.PredictedConfidence,
		entry.ActualAccuracy, entry.StrategyUsed, entry.StrategyOutcome,
		entry.AgentID,
	)
	if err != nil {
		return fmt.Errorf("metacognitive insert: %w", err)
	}
	return nil
}

func (r *MetaCognitiveRepo) GetCalibrationByDomain(ctx context.Context, agentID string) (map[string]*domain.DomainCalibration, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT domain, sample_count,
			avg_predicted_confidence, avg_actual_accuracy, calibration_offset
		FROM metacognitive_calibration
		WHERE agent_id = $1`, agentID)
	if err != nil {
		return nil, fmt.Errorf("get calibration: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*domain.DomainCalibration)
	for rows.Next() {
		var cal domain.DomainCalibration
		err := rows.Scan(
			&cal.Domain, &cal.SampleCount,
			&cal.PredictedConfidence, &cal.ActualAccuracy, &cal.CalibrationOffset,
		)
		if err != nil {
			return nil, fmt.Errorf("scan calibration: %w", err)
		}
		result[cal.Domain] = &cal
	}
	return result, rows.Err()
}

func (r *MetaCognitiveRepo) GetGaps(ctx context.Context, projectID *uuid.UUID) ([]*domain.KnowledgeGap, error) {
	// Knowledge gaps: domains where low confidence is queried frequently.
	// Queries metacognitive_log for domains with high query count but low accuracy.
	query := `
		SELECT domain,
			COUNT(*) AS query_count,
			AVG(actual_accuracy) AS avg_confidence
		FROM metacognitive_log
		WHERE actual_accuracy IS NOT NULL`

	args := make([]any, 0)
	if projectID != nil {
		// If we had project_id in metacognitive_log we'd filter here.
		// For now, return all domains.
	}

	query += `
		GROUP BY domain
		HAVING AVG(actual_accuracy) < 0.6 AND COUNT(*) >= 3
		ORDER BY COUNT(*) * (1.0 - AVG(actual_accuracy)) DESC
		LIMIT 20`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get gaps: %w", err)
	}
	defer rows.Close()

	var result []*domain.KnowledgeGap
	for rows.Next() {
		var g domain.KnowledgeGap
		err := rows.Scan(&g.Domain, &g.QueryCount, &g.AvgConfidence)
		if err != nil {
			return nil, fmt.Errorf("scan gap: %w", err)
		}
		g.GapScore = float64(g.QueryCount) * (1.0 - g.AvgConfidence)
		result = append(result, &g)
	}
	return result, rows.Err()
}
