package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// MetaCognitiveRepo implements domain.MetaCognitiveRepo against a SQLite database.
type MetaCognitiveRepo struct {
	db *sql.DB
}

// NewMetaCognitiveRepo constructs a MetaCognitiveRepo backed by db.
func NewMetaCognitiveRepo(db *sql.DB) *MetaCognitiveRepo {
	return &MetaCognitiveRepo{db: db}
}

// Insert stores a new metacognitive log entry.
func (r *MetaCognitiveRepo) Insert(ctx context.Context, entry *domain.MetaCognitiveEntry) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO metacognitive_log
			(id, agent_id, domain, predicted_confidence, actual_outcome,
			 calibration_error, context, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID.String(), entry.AgentID, entry.Domain,
		entry.PredictedConfidence, entry.ActualAccuracy,
		// calibration_error = |predicted - actual|
		abs(entry.PredictedConfidence-entry.ActualAccuracy),
		entry.StrategyUsed, now,
	)
	if err != nil {
		return fmt.Errorf("metacognitive insert: %w", err)
	}
	return nil
}

// GetCalibrationByDomain returns per-domain calibration stats aggregated from
// metacognitive_log for the given agent.
func (r *MetaCognitiveRepo) GetCalibrationByDomain(ctx context.Context, agentID string) (map[string]*domain.DomainCalibration, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			domain,
			COUNT(*) AS sample_count,
			AVG(predicted_confidence) AS avg_predicted,
			AVG(actual_outcome) AS avg_actual
		FROM metacognitive_log
		WHERE agent_id = ?
		GROUP BY domain
		ORDER BY domain`, agentID)
	if err != nil {
		return nil, fmt.Errorf("metacognitive calibration by domain: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*domain.DomainCalibration)
	for rows.Next() {
		var (
			cal         domain.DomainCalibration
			sampleCount int
			avgPred     float64
			avgActual   float64
		)
		if err := rows.Scan(&cal.Domain, &sampleCount, &avgPred, &avgActual); err != nil {
			return nil, fmt.Errorf("scan metacognitive calibration: %w", err)
		}
		cal.SampleCount = sampleCount
		cal.PredictedConfidence = avgPred
		cal.ActualAccuracy = avgActual
		cal.CalibrationOffset = avgPred - avgActual
		result[cal.Domain] = &cal
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metacognitive calibration rows: %w", err)
	}
	return result, nil
}

// GetGaps returns knowledge gaps inferred from the metacognitive log.
// A gap is a domain where average actual accuracy is below 0.6 and there are
// at least 3 entries. projectID is accepted for interface compatibility but
// metacognitive_log does not carry a project dimension — all entries are returned.
func (r *MetaCognitiveRepo) GetGaps(ctx context.Context, projectID *uuid.UUID) ([]*domain.KnowledgeGap, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			domain,
			COUNT(*) AS query_count,
			AVG(actual_outcome) AS avg_confidence
		FROM metacognitive_log
		GROUP BY domain
		HAVING AVG(actual_outcome) < 0.6 AND COUNT(*) >= 3
		ORDER BY COUNT(*) * (1.0 - AVG(actual_outcome)) DESC
		LIMIT 20`)
	if err != nil {
		return nil, fmt.Errorf("metacognitive get gaps: %w", err)
	}
	defer rows.Close()

	var result []*domain.KnowledgeGap
	for rows.Next() {
		var g domain.KnowledgeGap
		if err := rows.Scan(&g.Domain, &g.QueryCount, &g.AvgConfidence); err != nil {
			return nil, fmt.Errorf("scan knowledge gap: %w", err)
		}
		g.GapScore = float64(g.QueryCount) * (1.0 - g.AvgConfidence)
		result = append(result, &g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metacognitive gaps rows: %w", err)
	}
	return result, nil
}

// abs returns the absolute value of x (avoids importing math for a single use).
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Ensure the repo compiles even when the sql package is only used for sql.ErrNoRows.
var _ *sql.DB
