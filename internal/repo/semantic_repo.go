package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

type SemanticRepo struct {
	pool *pgxpool.Pool
}

func NewSemanticRepo(pool *pgxpool.Pool) *SemanticRepo {
	return &SemanticRepo{pool: pool}
}

func (r *SemanticRepo) Insert(ctx context.Context, mem *domain.SemanticMemory) error {
	tags := mem.Tags
	if tags == nil {
		tags = []string{}
	}
	meta, _ := json.Marshal(mem.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	srcEpisodes := mem.SourceEpisodes
	if srcEpisodes == nil {
		srcEpisodes = []uuid.UUID{}
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO semantic_memory (id, project_id, entity_type, content, summary,
			embedding, importance, confidence, source_episodes,
			token_count, tags, metadata)
		VALUES ($1, $2, $3, $4, $5, $6::vector, $7, $8, $9, $10, $11, $12)`,
		mem.ID, mem.ProjectID, mem.EntityType, mem.Content, mem.Summary,
		encodeVector(mem.Embedding), mem.Importance, mem.Confidence, srcEpisodes,
		mem.TokenCount, tags, meta,
	)
	if err != nil {
		return fmt.Errorf("semantic insert: %w", err)
	}
	return nil
}

func (r *SemanticRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.SemanticMemory, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, project_id, entity_type, content, summary,
			importance, confidence, source_episodes,
			access_count, last_accessed, token_count, tags, metadata,
			created_at, updated_at
		FROM semantic_memory
		WHERE id = $1`, id)

	return scanSemantic(row)
}

func (r *SemanticRepo) SearchSimilar(ctx context.Context, embedding []float32, projectID *uuid.UUID, limit int) ([]*domain.SemanticMemory, error) {
	vec := encodeVector(embedding)
	var rows pgx.Rows
	var err error

	if projectID != nil {
		rows, err = r.pool.Query(ctx, `
			SELECT id, project_id, entity_type, content, summary,
				embedding::text, 1 - (embedding <=> $2::vector) AS similarity,
				importance, confidence, source_episodes,
				access_count, last_accessed, token_count, tags, metadata,
				created_at, updated_at
			FROM semantic_memory
			WHERE project_id = $1
			ORDER BY embedding <=> $2::vector
			LIMIT $3`, projectID, vec, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, project_id, entity_type, content, summary,
				embedding::text, 1 - (embedding <=> $1::vector) AS similarity,
				importance, confidence, source_episodes,
				access_count, last_accessed, token_count, tags, metadata,
				created_at, updated_at
			FROM semantic_memory
			ORDER BY embedding <=> $1::vector
			LIMIT $2`, vec, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}
	defer rows.Close()

	return collectSemanticWithEmbedding(rows)
}

func (r *SemanticRepo) SearchBM25(ctx context.Context, query string, projectID *uuid.UUID, limit int) ([]*domain.SemanticMemory, error) {
	var rows pgx.Rows
	var err error

	if projectID != nil {
		rows, err = r.pool.Query(ctx, `
			SELECT id, project_id, entity_type, content, summary,
				embedding::text, ts_rank(to_tsvector('english', coalesce(content, '')), plainto_tsquery('english', $2)) AS similarity,
				importance, confidence, source_episodes,
				access_count, last_accessed, token_count, tags, metadata,
				created_at, updated_at
			FROM semantic_memory
			WHERE to_tsvector('english', coalesce(content, '')) @@ plainto_tsquery('english', $2)
				AND project_id = $1
			ORDER BY ts_rank(to_tsvector('english', coalesce(content, '')), plainto_tsquery('english', $2)) DESC
			LIMIT $3`, projectID, query, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, project_id, entity_type, content, summary,
				embedding::text, ts_rank(to_tsvector('english', coalesce(content, '')), plainto_tsquery('english', $1)) AS similarity,
				importance, confidence, source_episodes,
				access_count, last_accessed, token_count, tags, metadata,
				created_at, updated_at
			FROM semantic_memory
			WHERE to_tsvector('english', coalesce(content, '')) @@ plainto_tsquery('english', $1)
			ORDER BY ts_rank(to_tsvector('english', coalesce(content, '')), plainto_tsquery('english', $1)) DESC
			LIMIT $2`, query, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("semantic bm25 search: %w", err)
	}
	defer rows.Close()

	return collectSemanticWithEmbedding(rows)
}

func (r *SemanticRepo) ListByEntityType(ctx context.Context, projectID *uuid.UUID, entityType string, limit int) ([]*domain.SemanticMemory, error) {
	var rows pgx.Rows
	var err error

	if projectID != nil {
		rows, err = r.pool.Query(ctx, `
			SELECT id, project_id, entity_type, content, summary,
				embedding::text, 0.0 AS similarity,
				importance, confidence, source_episodes,
				access_count, last_accessed, token_count, tags, metadata,
				created_at, updated_at
			FROM semantic_memory
			WHERE project_id = $1 AND entity_type = $2
			ORDER BY importance DESC
			LIMIT $3`, projectID, entityType, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, project_id, entity_type, content, summary,
				embedding::text, 0.0 AS similarity,
				importance, confidence, source_episodes,
				access_count, last_accessed, token_count, tags, metadata,
				created_at, updated_at
			FROM semantic_memory
			WHERE entity_type = $1
			ORDER BY importance DESC
			LIMIT $2`, entityType, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("semantic list by entity type: %w", err)
	}
	defer rows.Close()

	return collectSemanticWithEmbedding(rows)
}

func (r *SemanticRepo) SearchGlobal(ctx context.Context, embedding []float32, limit int) ([]*domain.SemanticMemory, error) {
	vec := encodeVector(embedding)
	rows, err := r.pool.Query(ctx, `
		SELECT id, project_id, entity_type, content, summary,
			embedding::text, 1 - (embedding <=> $1::vector) AS similarity,
			importance, confidence, source_episodes,
			access_count, last_accessed, token_count, tags, metadata,
			created_at, updated_at
		FROM semantic_memory
		WHERE project_id IS NULL
		ORDER BY embedding <=> $1::vector
		LIMIT $2`, vec, limit)
	if err != nil {
		return nil, fmt.Errorf("semantic global search: %w", err)
	}
	defer rows.Close()

	return collectSemanticWithEmbedding(rows)
}

func (r *SemanticRepo) ListByProject(ctx context.Context, projectID *uuid.UUID, limit int) ([]*domain.SemanticMemory, error) {
	var rows pgx.Rows
	var err error
	if projectID != nil {
		rows, err = r.pool.Query(ctx, `
			SELECT id, project_id, entity_type, content, summary,
				importance, confidence, source_episodes,
				access_count, last_accessed, token_count, tags, metadata,
				created_at, updated_at
			FROM semantic_memory
			WHERE project_id = $1
			ORDER BY importance DESC
			LIMIT $2`, projectID, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, project_id, entity_type, content, summary,
				importance, confidence, source_episodes,
				access_count, last_accessed, token_count, tags, metadata,
				created_at, updated_at
			FROM semantic_memory
			ORDER BY importance DESC
			LIMIT $1`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list semantic by project: %w", err)
	}
	defer rows.Close()
	return collectSemantic(rows)
}

func (r *SemanticRepo) ListGlobal(ctx context.Context, limit int) ([]*domain.SemanticMemory, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, project_id, entity_type, content, summary,
			importance, confidence, source_episodes,
			access_count, last_accessed, token_count, tags, metadata,
			created_at, updated_at
		FROM semantic_memory
		WHERE project_id IS NULL
		ORDER BY importance DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list semantic global: %w", err)
	}
	defer rows.Close()
	return collectSemantic(rows)
}

func (r *SemanticRepo) Update(ctx context.Context, mem *domain.SemanticMemory) error {
	meta, _ := json.Marshal(mem.Metadata)
	tags := mem.Tags
	if tags == nil {
		tags = []string{}
	}

	tag, err := r.pool.Exec(ctx, `
		UPDATE semantic_memory SET
			content = $2, summary = $3, importance = $4, confidence = $5,
			entity_type = $6, tags = $7, metadata = $8, updated_at = NOW()
		WHERE id = $1`,
		mem.ID, mem.Content, mem.Summary, mem.Importance, mem.Confidence,
		mem.EntityType, tags, meta,
	)
	if err != nil {
		return fmt.Errorf("semantic update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *SemanticRepo) UpdateImportance(ctx context.Context, id uuid.UUID, importance float64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE semantic_memory SET importance = $2, last_accessed = NOW(), updated_at = NOW(), access_count = access_count + 1 WHERE id = $1`,
		id, importance)
	if err != nil {
		return fmt.Errorf("semantic update importance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *SemanticRepo) DecayImportance(ctx context.Context, olderThan time.Duration, factor float64, floor float64) (int, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE semantic_memory sm
		SET importance = GREATEST($3, sm.importance * $2)
		WHERE sm.last_accessed < NOW() - $1::interval
		  AND sm.importance > $3
		  AND NOT EXISTS (
		    SELECT 1 FROM emotional_tags et
		    WHERE et.memory_id = sm.id
		      AND et.valence IN ('danger', 'frustration')
		      AND et.intensity >= 0.5
		  )`,
		olderThan.String(), factor, floor)
	if err != nil {
		return 0, fmt.Errorf("decay semantic importance: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *SemanticRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM semantic_memory WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("semantic delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *SemanticRepo) Count(ctx context.Context, projectID *uuid.UUID) (int, error) {
	var count int
	var err error
	if projectID != nil {
		err = r.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM semantic_memory WHERE project_id = $1`,
			projectID).Scan(&count)
	} else {
		err = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM semantic_memory`).Scan(&count)
	}
	if err != nil {
		return 0, fmt.Errorf("count semantic: %w", err)
	}
	return count, nil
}

func scanSemantic(row pgx.Row) (*domain.SemanticMemory, error) {
	var m domain.SemanticMemory
	var metaJSON []byte

	err := row.Scan(
		&m.ID, &m.ProjectID, &m.EntityType, &m.Content, &m.Summary,
		&m.Importance, &m.Confidence, &m.SourceEpisodes,
		&m.AccessCount, &m.LastAccessed, &m.TokenCount, &m.Tags, &metaJSON,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan semantic: %w", err)
	}

	m.Tier = domain.TierSemantic
	if metaJSON != nil {
		json.Unmarshal(metaJSON, &m.Metadata)
	}
	return &m, nil
}

func collectSemantic(rows pgx.Rows) ([]*domain.SemanticMemory, error) {
	var result []*domain.SemanticMemory
	for rows.Next() {
		var m domain.SemanticMemory
		var metaJSON []byte

		err := rows.Scan(
			&m.ID, &m.ProjectID, &m.EntityType, &m.Content, &m.Summary,
			&m.Importance, &m.Confidence, &m.SourceEpisodes,
			&m.AccessCount, &m.LastAccessed, &m.TokenCount, &m.Tags, &metaJSON,
			&m.CreatedAt, &m.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan semantic: %w", err)
		}

		m.Tier = domain.TierSemantic
		if metaJSON != nil {
			json.Unmarshal(metaJSON, &m.Metadata)
		}
		result = append(result, &m)
	}
	return result, rows.Err()
}

func collectSemanticWithEmbedding(rows pgx.Rows) ([]*domain.SemanticMemory, error) {
	var result []*domain.SemanticMemory
	for rows.Next() {
		var m domain.SemanticMemory
		var metaJSON []byte
		var embeddingStr string

		err := rows.Scan(
			&m.ID, &m.ProjectID, &m.EntityType, &m.Content, &m.Summary,
			&embeddingStr, &m.Similarity, &m.Importance, &m.Confidence, &m.SourceEpisodes,
			&m.AccessCount, &m.LastAccessed, &m.TokenCount, &m.Tags, &metaJSON,
			&m.CreatedAt, &m.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan semantic+emb: %w", err)
		}

		m.Tier = domain.TierSemantic
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
