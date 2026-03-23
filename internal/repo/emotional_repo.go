package repo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

type EmotionalTagRepo struct {
	pool *pgxpool.Pool
}

func NewEmotionalTagRepo(pool *pgxpool.Pool) *EmotionalTagRepo {
	return &EmotionalTagRepo{pool: pool}
}

func (r *EmotionalTagRepo) Insert(ctx context.Context, tag *domain.EmotionalTag) error {
	signals, _ := json.Marshal(tag.Signals)
	if signals == nil {
		signals = []byte("{}")
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO emotional_tags (memory_id, memory_tier, valence, intensity, signals)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (memory_id, valence) DO UPDATE SET
			intensity = GREATEST(emotional_tags.intensity, EXCLUDED.intensity),
			signals = EXCLUDED.signals`,
		tag.MemoryID, tag.MemoryTier, tag.Valence, tag.Intensity, signals,
	)
	if err != nil {
		return fmt.Errorf("emotional tag insert: %w", err)
	}
	return nil
}

func (r *EmotionalTagRepo) GetByMemory(ctx context.Context, memoryID uuid.UUID) ([]*domain.EmotionalTag, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT memory_id, memory_tier, valence, intensity, signals, created_at
		FROM emotional_tags
		WHERE memory_id = $1
		ORDER BY intensity DESC`, memoryID)
	if err != nil {
		return nil, fmt.Errorf("get emotional tags: %w", err)
	}
	defer rows.Close()

	var result []*domain.EmotionalTag
	for rows.Next() {
		var t domain.EmotionalTag
		var signalsJSON []byte

		err := rows.Scan(&t.MemoryID, &t.MemoryTier, &t.Valence,
			&t.Intensity, &signalsJSON, &t.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan emotional tag: %w", err)
		}
		if signalsJSON != nil {
			json.Unmarshal(signalsJSON, &t.Signals)
		}
		result = append(result, &t)
	}
	return result, rows.Err()
}

func (r *EmotionalTagRepo) GetHighPriority(ctx context.Context, projectID *uuid.UUID, limit int) ([]*domain.EmotionalTag, error) {
	query := `
		SELECT et.memory_id, et.memory_tier, et.valence, et.intensity, et.signals, et.created_at
		FROM emotional_tags et
		WHERE et.valence IN ('danger', 'surprise', 'frustration')
		ORDER BY et.intensity DESC
		LIMIT $1`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("get high priority: %w", err)
	}
	defer rows.Close()

	var result []*domain.EmotionalTag
	for rows.Next() {
		var t domain.EmotionalTag
		var signalsJSON []byte

		err := rows.Scan(&t.MemoryID, &t.MemoryTier, &t.Valence,
			&t.Intensity, &signalsJSON, &t.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan emotional tag: %w", err)
		}
		if signalsJSON != nil {
			json.Unmarshal(signalsJSON, &t.Signals)
		}
		result = append(result, &t)
	}
	return result, rows.Err()
}
