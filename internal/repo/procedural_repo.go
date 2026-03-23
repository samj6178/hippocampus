package repo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

type ProceduralRepo struct {
	pool *pgxpool.Pool
}

func NewProceduralRepo(pool *pgxpool.Pool) *ProceduralRepo {
	return &ProceduralRepo{pool: pool}
}

func (r *ProceduralRepo) Insert(ctx context.Context, mem *domain.ProceduralMemory) error {
	tags := mem.Tags
	if tags == nil {
		tags = []string{}
	}
	meta, _ := json.Marshal(mem.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	steps, _ := json.Marshal(mem.Steps)
	if steps == nil {
		steps = []byte("[]")
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO procedural_memory (id, project_id, task_type, description,
			steps, embedding, importance, confidence,
			success_count, failure_count,
			token_count, version, tags, metadata)
		VALUES ($1, $2, $3, $4, $5, $6::vector, $7, $8, $9, $10, $11, $12, $13, $14)`,
		mem.ID, mem.ProjectID, mem.TaskType, mem.Content,
		steps, encodeVector(mem.Embedding), mem.Importance, mem.Confidence,
		mem.SuccessCount, mem.FailureCount,
		mem.TokenCount, mem.Version, tags, meta,
	)
	if err != nil {
		return fmt.Errorf("procedural insert: %w", err)
	}
	return nil
}

func (r *ProceduralRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.ProceduralMemory, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, project_id, task_type, description, steps,
			importance, confidence, success_count, failure_count,
			access_count, last_accessed, token_count, version, tags, metadata,
			created_at, updated_at
		FROM procedural_memory
		WHERE id = $1`, id)

	return scanProcedural(row)
}

func (r *ProceduralRepo) SearchByTaskType(ctx context.Context, embedding []float32, projectID *uuid.UUID, limit int) ([]*domain.ProceduralMemory, error) {
	vec := encodeVector(embedding)
	var rows pgx.Rows
	var err error

	if projectID != nil {
		rows, err = r.pool.Query(ctx, `
			SELECT id, project_id, task_type, description, steps,
				embedding::text, 1 - (embedding <=> $2::vector) AS similarity,
				importance, confidence, success_count, failure_count,
				access_count, last_accessed, token_count, version, tags, metadata,
				created_at, updated_at
			FROM procedural_memory
			WHERE project_id = $1
			ORDER BY embedding <=> $2::vector
			LIMIT $3`, projectID, vec, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, project_id, task_type, description, steps,
				embedding::text, 1 - (embedding <=> $1::vector) AS similarity,
				importance, confidence, success_count, failure_count,
				access_count, last_accessed, token_count, version, tags, metadata,
				created_at, updated_at
			FROM procedural_memory
			ORDER BY embedding <=> $1::vector
			LIMIT $2`, vec, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("procedural search: %w", err)
	}
	defer rows.Close()

	return collectProceduralWithEmbedding(rows)
}

func (r *ProceduralRepo) IncrementSuccess(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE procedural_memory
		SET success_count = success_count + 1, last_accessed = NOW(), last_used = NOW()
		WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("increment success: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ProceduralRepo) IncrementFailure(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE procedural_memory
		SET failure_count = failure_count + 1, last_accessed = NOW(), last_used = NOW()
		WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("increment failure: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ProceduralRepo) Count(ctx context.Context, projectID *uuid.UUID) (int, error) {
	var count int
	var err error
	if projectID != nil {
		err = r.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM procedural_memory WHERE project_id = $1`, projectID).Scan(&count)
	} else {
		err = r.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM procedural_memory`).Scan(&count)
	}
	return count, err
}

func (r *ProceduralRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM procedural_memory WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("procedural delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func scanProcedural(row pgx.Row) (*domain.ProceduralMemory, error) {
	var m domain.ProceduralMemory
	var metaJSON, stepsJSON []byte

	err := row.Scan(
		&m.ID, &m.ProjectID, &m.TaskType, &m.Content, &stepsJSON,
		&m.Importance, &m.Confidence, &m.SuccessCount, &m.FailureCount,
		&m.AccessCount, &m.LastAccessed, &m.TokenCount, &m.Version, &m.Tags, &metaJSON,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan procedural: %w", err)
	}

	m.Tier = domain.TierProcedural
	if stepsJSON != nil {
		json.Unmarshal(stepsJSON, &m.Steps)
	}
	if metaJSON != nil {
		json.Unmarshal(metaJSON, &m.Metadata)
	}
	return &m, nil
}

func collectProcedural(rows pgx.Rows) ([]*domain.ProceduralMemory, error) {
	var result []*domain.ProceduralMemory
	for rows.Next() {
		var m domain.ProceduralMemory
		var metaJSON, stepsJSON []byte

		err := rows.Scan(
			&m.ID, &m.ProjectID, &m.TaskType, &m.Content, &stepsJSON,
			&m.Importance, &m.Confidence, &m.SuccessCount, &m.FailureCount,
			&m.AccessCount, &m.LastAccessed, &m.TokenCount, &m.Version, &m.Tags, &metaJSON,
			&m.CreatedAt, &m.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan procedural: %w", err)
		}

		m.Tier = domain.TierProcedural
		if stepsJSON != nil {
			json.Unmarshal(stepsJSON, &m.Steps)
		}
		if metaJSON != nil {
			json.Unmarshal(metaJSON, &m.Metadata)
		}
		result = append(result, &m)
	}
	return result, rows.Err()
}

func collectProceduralWithEmbedding(rows pgx.Rows) ([]*domain.ProceduralMemory, error) {
	var result []*domain.ProceduralMemory
	for rows.Next() {
		var m domain.ProceduralMemory
		var metaJSON, stepsJSON []byte
		var embeddingStr string

		err := rows.Scan(
			&m.ID, &m.ProjectID, &m.TaskType, &m.Content, &stepsJSON,
			&embeddingStr, &m.Similarity, &m.Importance, &m.Confidence, &m.SuccessCount, &m.FailureCount,
			&m.AccessCount, &m.LastAccessed, &m.TokenCount, &m.Version, &m.Tags, &metaJSON,
			&m.CreatedAt, &m.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan procedural+emb: %w", err)
		}

		m.Tier = domain.TierProcedural
		if stepsJSON != nil {
			json.Unmarshal(stepsJSON, &m.Steps)
		}
		if metaJSON != nil {
			json.Unmarshal(metaJSON, &m.Metadata)
		}
		if embeddingStr != "" {
			m.Embedding, _ = decodeVector(embeddingStr)
		}
		result = append(result, &m)
	}
	return result, rows.Err()
}
