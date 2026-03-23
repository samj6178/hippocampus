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

type EpisodicRepo struct {
	pool *pgxpool.Pool
}

func NewEpisodicRepo(pool *pgxpool.Pool) *EpisodicRepo {
	return &EpisodicRepo{pool: pool}
}

func (r *EpisodicRepo) Insert(ctx context.Context, mem *domain.EpisodicMemory) error {
	tagsArr := mem.Tags
	if tagsArr == nil {
		tagsArr = []string{}
	}
	meta, _ := json.Marshal(mem.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO episodic_memory (id, time, project_id, agent_id, session_id,
			content, summary, embedding, importance, confidence,
			token_count, tags, metadata, consolidated)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::vector, $9, $10, $11, $12, $13, false)`,
		mem.ID, mem.CreatedAt, mem.ProjectID, mem.AgentID, mem.SessionID,
		mem.Content, mem.Summary, encodeVector(mem.Embedding),
		mem.Importance, mem.Confidence,
		mem.TokenCount, tagsArr, meta,
	)
	if err != nil {
		return fmt.Errorf("episodic insert: %w", err)
	}
	return nil
}

func (r *EpisodicRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.EpisodicMemory, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, time, project_id, agent_id, session_id,
			content, summary, importance, confidence,
			access_count, last_accessed, token_count, tags, metadata, consolidated
		FROM episodic_memory
		WHERE id = $1
		LIMIT 1`, id)

	return scanEpisodic(row)
}

func (r *EpisodicRepo) SearchSimilar(ctx context.Context, embedding []float32, projectID *uuid.UUID, limit int) ([]*domain.EpisodicMemory, error) {
	vec := encodeVector(embedding)

	var rows pgx.Rows
	var err error

	if projectID != nil {
		rows, err = r.pool.Query(ctx, `
			SELECT id, time, project_id, agent_id, session_id,
				content, summary, embedding::text,
				1 - (embedding <=> $2::vector) AS similarity,
				importance, confidence,
				access_count, last_accessed, token_count, tags, metadata, consolidated
			FROM episodic_memory
			WHERE project_id = $1 AND consolidated = false
			ORDER BY embedding <=> $2::vector
			LIMIT $3`, projectID, vec, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, time, project_id, agent_id, session_id,
				content, summary, embedding::text,
				1 - (embedding <=> $1::vector) AS similarity,
				importance, confidence,
				access_count, last_accessed, token_count, tags, metadata, consolidated
			FROM episodic_memory
			WHERE consolidated = false
			ORDER BY embedding <=> $1::vector
			LIMIT $2`, vec, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("episodic search: %w", err)
	}
	defer rows.Close()

	return collectEpisodicWithEmbedding(rows)
}

func (r *EpisodicRepo) SearchBM25(ctx context.Context, query string, projectID *uuid.UUID, limit int) ([]*domain.EpisodicMemory, error) {
	var rows pgx.Rows
	var err error

	if projectID != nil {
		rows, err = r.pool.Query(ctx, `
			SELECT id, time, project_id, agent_id, session_id,
				content, summary, embedding::text,
				ts_rank(to_tsvector('english', coalesce(content, '')), plainto_tsquery('english', $2)) AS similarity,
				importance, confidence,
				access_count, last_accessed, token_count, tags, metadata, consolidated
			FROM episodic_memory
			WHERE to_tsvector('english', coalesce(content, '')) @@ plainto_tsquery('english', $2)
				AND project_id = $1 AND consolidated = false
			ORDER BY ts_rank(to_tsvector('english', coalesce(content, '')), plainto_tsquery('english', $2)) DESC
			LIMIT $3`, projectID, query, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, time, project_id, agent_id, session_id,
				content, summary, embedding::text,
				ts_rank(to_tsvector('english', coalesce(content, '')), plainto_tsquery('english', $1)) AS similarity,
				importance, confidence,
				access_count, last_accessed, token_count, tags, metadata, consolidated
			FROM episodic_memory
			WHERE to_tsvector('english', coalesce(content, '')) @@ plainto_tsquery('english', $1)
				AND consolidated = false
			ORDER BY ts_rank(to_tsvector('english', coalesce(content, '')), plainto_tsquery('english', $1)) DESC
			LIMIT $2`, query, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("episodic bm25 search: %w", err)
	}
	defer rows.Close()

	return collectEpisodicWithEmbedding(rows)
}

func (r *EpisodicRepo) ListBySession(ctx context.Context, sessionID uuid.UUID) ([]*domain.EpisodicMemory, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, time, project_id, agent_id, session_id,
			content, summary, importance, confidence,
			access_count, last_accessed, token_count, tags, metadata, consolidated
		FROM episodic_memory
		WHERE session_id = $1
		ORDER BY time ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list by session: %w", err)
	}
	defer rows.Close()

	return collectEpisodic(rows)
}

func (r *EpisodicRepo) ListUnconsolidated(ctx context.Context, projectID *uuid.UUID, limit int) ([]*domain.EpisodicMemory, error) {
	var rows pgx.Rows
	var err error

	if projectID != nil {
		rows, err = r.pool.Query(ctx, `
			SELECT id, time, project_id, agent_id, session_id,
				content, summary, embedding::text, 0::float8 AS similarity,
				importance, confidence,
				access_count, last_accessed, token_count, tags, metadata, consolidated
			FROM episodic_memory
			WHERE consolidated = false AND project_id = $1
			ORDER BY time DESC
			LIMIT $2`, projectID, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, time, project_id, agent_id, session_id,
				content, summary, embedding::text, 0::float8 AS similarity,
				importance, confidence,
				access_count, last_accessed, token_count, tags, metadata, consolidated
			FROM episodic_memory
			WHERE consolidated = false
			ORDER BY time DESC
			LIMIT $1`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list unconsolidated: %w", err)
	}
	defer rows.Close()

	return collectEpisodicWithEmbedding(rows)
}

func (r *EpisodicRepo) MarkConsolidated(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE episodic_memory
		SET consolidated = true
		WHERE id = ANY($1)`, ids)
	if err != nil {
		return fmt.Errorf("mark consolidated: %w", err)
	}
	return nil
}

func (r *EpisodicRepo) UpdateImportance(ctx context.Context, id uuid.UUID, importance float64) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE episodic_memory
		SET importance = $2, last_accessed = NOW(), access_count = access_count + 1
		WHERE id = $1`, id, importance)
	if err != nil {
		return fmt.Errorf("update importance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *EpisodicRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM episodic_memory WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete episodic: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *EpisodicRepo) DecayImportance(ctx context.Context, olderThan time.Duration, factor float64, floor float64) (int, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE episodic_memory em
		SET importance = GREATEST($3, em.importance * $2)
		WHERE em.last_accessed < NOW() - $1::interval
		  AND em.importance > $3
		  AND em.consolidated = false
		  AND NOT EXISTS (
		    SELECT 1 FROM emotional_tags et
		    WHERE et.memory_id = em.id
		      AND et.valence IN ('danger', 'frustration')
		      AND et.intensity >= 0.5
		  )`,
		olderThan.String(), factor, floor)
	if err != nil {
		return 0, fmt.Errorf("decay episodic importance: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *EpisodicRepo) Count(ctx context.Context, projectID *uuid.UUID) (int, error) {
	var count int
	var err error
	if projectID != nil {
		err = r.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM episodic_memory WHERE project_id = $1`, projectID).Scan(&count)
	} else {
		err = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM episodic_memory`).Scan(&count)
	}
	if err != nil {
		return 0, fmt.Errorf("count episodic: %w", err)
	}
	return count, nil
}

func (r *EpisodicRepo) ListByTags(ctx context.Context, projectID *uuid.UUID, tags []string, limit int) ([]*domain.EpisodicMemory, error) {
	var rows pgx.Rows
	var err error
	if projectID != nil {
		rows, err = r.pool.Query(ctx, `
			SELECT id, time, project_id, agent_id, session_id,
				content, summary, importance, confidence,
				access_count, last_accessed, token_count, tags, metadata, consolidated
			FROM episodic_memory
			WHERE project_id = $1 AND tags && $2::text[]
			ORDER BY importance DESC, time DESC
			LIMIT $3`, projectID, tags, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, time, project_id, agent_id, session_id,
				content, summary, importance, confidence,
				access_count, last_accessed, token_count, tags, metadata, consolidated
			FROM episodic_memory
			WHERE tags && $1::text[]
			ORDER BY importance DESC, time DESC
			LIMIT $2`, tags, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list by tags: %w", err)
	}
	defer rows.Close()
	return collectEpisodic(rows)
}

func scanEpisodic(row pgx.Row) (*domain.EpisodicMemory, error) {
	var m domain.EpisodicMemory
	var metaJSON []byte
	var consolidated bool
	var createdAt time.Time

	err := row.Scan(
		&m.ID, &createdAt, &m.ProjectID, &m.AgentID, &m.SessionID,
		&m.Content, &m.Summary, &m.Importance, &m.Confidence,
		&m.AccessCount, &m.LastAccessed, &m.TokenCount, &m.Tags, &metaJSON, &consolidated,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan episodic: %w", err)
	}

	m.Tier = domain.TierEpisodic
	m.CreatedAt = createdAt
	m.UpdatedAt = createdAt
	if metaJSON != nil {
		json.Unmarshal(metaJSON, &m.Metadata)
	}
	return &m, nil
}

func collectEpisodic(rows pgx.Rows) ([]*domain.EpisodicMemory, error) {
	var result []*domain.EpisodicMemory
	for rows.Next() {
		var m domain.EpisodicMemory
		var metaJSON []byte
		var consolidated bool
		var createdAt time.Time

		err := rows.Scan(
			&m.ID, &createdAt, &m.ProjectID, &m.AgentID, &m.SessionID,
			&m.Content, &m.Summary, &m.Importance, &m.Confidence,
			&m.AccessCount, &m.LastAccessed, &m.TokenCount, &m.Tags, &metaJSON, &consolidated,
		)
		if err != nil {
			return nil, fmt.Errorf("scan episodic: %w", err)
		}

		m.Tier = domain.TierEpisodic
		m.CreatedAt = createdAt
		m.UpdatedAt = createdAt
		if metaJSON != nil {
			json.Unmarshal(metaJSON, &m.Metadata)
		}
		result = append(result, &m)
	}
	return result, rows.Err()
}

func collectEpisodicWithEmbedding(rows pgx.Rows) ([]*domain.EpisodicMemory, error) {
	var result []*domain.EpisodicMemory
	for rows.Next() {
		var m domain.EpisodicMemory
		var metaJSON []byte
		var embeddingStr string
		var consolidated bool
		var createdAt time.Time

		err := rows.Scan(
			&m.ID, &createdAt, &m.ProjectID, &m.AgentID, &m.SessionID,
			&m.Content, &m.Summary, &embeddingStr, &m.Similarity,
			&m.Importance, &m.Confidence,
			&m.AccessCount, &m.LastAccessed, &m.TokenCount, &m.Tags, &metaJSON, &consolidated,
		)
		if err != nil {
			return nil, fmt.Errorf("scan episodic+emb: %w", err)
		}

		m.Tier = domain.TierEpisodic
		m.CreatedAt = createdAt
		m.UpdatedAt = createdAt
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
