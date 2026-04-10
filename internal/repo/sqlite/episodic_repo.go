package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/pkg/vecutil"
)

// EpisodicRepo implements domain.EpisodicRepo using SQLite.
// Full-text search uses FTS5 (episodic_fts virtual table).
// FTS5 is kept in sync manually: Insert adds a row, Delete removes it.
// Vector search is brute-force cosine similarity over a bounded candidate pool.
type EpisodicRepo struct {
	db *sql.DB
}

// NewEpisodicRepo creates an EpisodicRepo backed by the given *sql.DB.
func NewEpisodicRepo(db *sql.DB) *EpisodicRepo {
	return &EpisodicRepo{db: db}
}

func (r *EpisodicRepo) Insert(ctx context.Context, mem *domain.EpisodicMemory) error {
	tags, err := marshalTags(mem.Tags)
	if err != nil {
		return fmt.Errorf("episodic insert marshal tags: %w", err)
	}
	meta, err := marshalJSON(mem.Metadata)
	if err != nil {
		return fmt.Errorf("episodic insert marshal metadata: %w", err)
	}

	idStr := mem.ID.String()
	sessionStr := mem.SessionID.String()
	var projectStr *string
	if mem.ProjectID != nil {
		s := mem.ProjectID.String()
		projectStr = &s
	}

	createdAt := mem.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	contentHash := ContentHash(mem.Content)

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO episodic_memory
			(id, time, project_id, agent_id, session_id, content, summary,
			 embedding, importance, confidence, access_count, last_accessed,
			 token_count, tags, metadata, consolidated, content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		idStr,
		formatTime(createdAt),
		projectStr,
		mem.AgentID,
		sessionStr,
		mem.Content,
		mem.Summary,
		EncodeEmbedding(mem.Embedding),
		mem.Importance,
		mem.Confidence,
		mem.AccessCount,
		formatTime(mem.LastAccessed),
		mem.TokenCount,
		tags,
		meta,
		contentHash,
	)
	if err != nil {
		return fmt.Errorf("episodic insert: %w", err)
	}

	// Sync FTS5: insert content using the implicit rowid of the just-inserted row.
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO episodic_fts(rowid, content) SELECT rowid, ? FROM episodic_memory WHERE id = ?`,
		mem.Content, idStr,
	)
	if err != nil {
		// FTS sync failure is non-fatal for data integrity but degrades search.
		// Wrap and return so the caller is aware.
		return fmt.Errorf("episodic fts sync insert: %w", err)
	}

	return nil
}

// FindByContentHash returns an episodic memory matching the content hash within
// the given project, or domain.ErrNotFound if no match exists.
func (r *EpisodicRepo) FindByContentHash(ctx context.Context, projectID *uuid.UUID, contentHash string) (*domain.EpisodicMemory, error) {
	var row *sql.Row
	if projectID != nil {
		row = r.db.QueryRowContext(ctx, `
			SELECT id, time, project_id, agent_id, session_id, content, summary,
			       importance, confidence, access_count, last_accessed, token_count,
			       tags, metadata, consolidated
			FROM episodic_memory
			WHERE project_id = ? AND content_hash = ?
			LIMIT 1`, projectID.String(), contentHash)
	} else {
		row = r.db.QueryRowContext(ctx, `
			SELECT id, time, project_id, agent_id, session_id, content, summary,
			       importance, confidence, access_count, last_accessed, token_count,
			       tags, metadata, consolidated
			FROM episodic_memory
			WHERE project_id IS NULL AND content_hash = ?
			LIMIT 1`, contentHash)
	}

	mem, err := scanEpisodic(row)
	if err != nil {
		return nil, err
	}
	return mem, nil
}

func (r *EpisodicRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.EpisodicMemory, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, time, project_id, agent_id, session_id, content, summary,
		       importance, confidence, access_count, last_accessed, token_count,
		       tags, metadata, consolidated
		FROM episodic_memory
		WHERE id = ?
		LIMIT 1`, id.String())

	mem, err := scanEpisodic(row)
	if err != nil {
		return nil, fmt.Errorf("episodic get by id: %w", err)
	}
	return mem, nil
}

// SearchSimilar performs brute-force cosine similarity search.
// To avoid scanning all rows, we pre-select candidates ordered by importance DESC,
// capped at max(limit*10, 500). Then we score and return the top `limit`.
func (r *EpisodicRepo) SearchSimilar(ctx context.Context, embedding []float32, projectID *uuid.UUID, limit int) ([]*domain.EpisodicMemory, error) {
	candidateLimit := limit * 10
	if candidateLimit < 500 {
		candidateLimit = 500
	}

	var (
		rows *sql.Rows
		err  error
	)
	if projectID != nil {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, time, project_id, agent_id, session_id, content, summary,
			       embedding, importance, confidence, access_count, last_accessed,
			       token_count, tags, metadata, consolidated
			FROM episodic_memory
			WHERE project_id = ? AND consolidated = 0 AND embedding IS NOT NULL
			ORDER BY importance DESC
			LIMIT ?`, projectID.String(), candidateLimit)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, time, project_id, agent_id, session_id, content, summary,
			       embedding, importance, confidence, access_count, last_accessed,
			       token_count, tags, metadata, consolidated
			FROM episodic_memory
			WHERE consolidated = 0 AND embedding IS NOT NULL
			ORDER BY importance DESC
			LIMIT ?`, candidateLimit)
	}
	if err != nil {
		return nil, fmt.Errorf("episodic search similar query: %w", err)
	}
	defer rows.Close()

	candidates, err := collectEpisodicWithBlob(rows)
	if err != nil {
		return nil, err
	}

	// Score candidates.
	type scored struct {
		mem        *domain.EpisodicMemory
		similarity float64
	}
	results := make([]scored, 0, len(candidates))
	for _, m := range candidates {
		if len(m.Embedding) == 0 {
			continue
		}
		sim := vecutil.CosineSimilarity(embedding, m.Embedding)
		results = append(results, scored{m, sim})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].similarity > results[j].similarity
	})

	if len(results) > limit {
		results = results[:limit]
	}

	out := make([]*domain.EpisodicMemory, len(results))
	for i, s := range results {
		s.mem.Similarity = s.similarity
		out[i] = s.mem
	}
	return out, nil
}

// SearchBM25 uses the episodic_fts FTS5 virtual table.
// Rows are joined back to episodic_memory via implicit rowid.
func (r *EpisodicRepo) SearchBM25(ctx context.Context, query string, projectID *uuid.UUID, limit int) ([]*domain.EpisodicMemory, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if projectID != nil {
		rows, err = r.db.QueryContext(ctx, `
			SELECT em.id, em.time, em.project_id, em.agent_id, em.session_id,
			       em.content, em.summary, em.importance, em.confidence,
			       em.access_count, em.last_accessed, em.token_count,
			       em.tags, em.metadata, em.consolidated
			FROM episodic_memory em
			JOIN episodic_fts ON episodic_fts.rowid = em.rowid
			WHERE episodic_fts MATCH ?
			  AND em.project_id = ?
			  AND em.consolidated = 0
			ORDER BY rank
			LIMIT ?`, query, projectID.String(), limit)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT em.id, em.time, em.project_id, em.agent_id, em.session_id,
			       em.content, em.summary, em.importance, em.confidence,
			       em.access_count, em.last_accessed, em.token_count,
			       em.tags, em.metadata, em.consolidated
			FROM episodic_memory em
			JOIN episodic_fts ON episodic_fts.rowid = em.rowid
			WHERE episodic_fts MATCH ?
			  AND em.consolidated = 0
			ORDER BY rank
			LIMIT ?`, query, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("episodic bm25 search: %w", err)
	}
	defer rows.Close()

	return collectEpisodic(rows)
}

func (r *EpisodicRepo) ListBySession(ctx context.Context, sessionID uuid.UUID) ([]*domain.EpisodicMemory, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, time, project_id, agent_id, session_id, content, summary,
		       importance, confidence, access_count, last_accessed, token_count,
		       tags, metadata, consolidated
		FROM episodic_memory
		WHERE session_id = ?
		ORDER BY time ASC`, sessionID.String())
	if err != nil {
		return nil, fmt.Errorf("episodic list by session: %w", err)
	}
	defer rows.Close()

	return collectEpisodic(rows)
}

func (r *EpisodicRepo) ListUnconsolidated(ctx context.Context, projectID *uuid.UUID, limit int) ([]*domain.EpisodicMemory, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if projectID != nil {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, time, project_id, agent_id, session_id, content, summary,
			       importance, confidence, access_count, last_accessed, token_count,
			       tags, metadata, consolidated
			FROM episodic_memory
			WHERE consolidated = 0 AND project_id = ?
			ORDER BY time DESC
			LIMIT ?`, projectID.String(), limit)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, time, project_id, agent_id, session_id, content, summary,
			       importance, confidence, access_count, last_accessed, token_count,
			       tags, metadata, consolidated
			FROM episodic_memory
			WHERE consolidated = 0
			ORDER BY time DESC
			LIMIT ?`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("episodic list unconsolidated: %w", err)
	}
	defer rows.Close()

	return collectEpisodic(rows)
}

func (r *EpisodicRepo) UpdateImportance(ctx context.Context, id uuid.UUID, importance float64) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE episodic_memory
		SET importance = ?, last_accessed = ?, access_count = access_count + 1
		WHERE id = ?`,
		importance, formatTime(time.Now().UTC()), id.String(),
	)
	if err != nil {
		return fmt.Errorf("episodic update importance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("episodic update importance rows affected: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *EpisodicRepo) MarkConsolidated(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id.String()
	}

	q := fmt.Sprintf(`UPDATE episodic_memory SET consolidated = 1 WHERE id IN (%s)`,
		strings.Join(placeholders, ","))
	_, err := r.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("episodic mark consolidated: %w", err)
	}
	return nil
}

// ListByTags returns memories whose tags JSON array overlaps with the given tags.
// Uses json_each to iterate the stored JSON array and checks membership with IN.
func (r *EpisodicRepo) ListByTags(ctx context.Context, projectID *uuid.UUID, tags []string, limit int) ([]*domain.EpisodicMemory, error) {
	if len(tags) == 0 {
		return nil, nil
	}

	// Build the IN clause placeholders for tag values.
	tagPlaceholders := make([]string, len(tags))
	tagArgs := make([]any, len(tags))
	for i, t := range tags {
		tagPlaceholders[i] = "?"
		tagArgs[i] = t
	}
	inClause := strings.Join(tagPlaceholders, ",")

	var (
		rows *sql.Rows
		err  error
	)

	if projectID != nil {
		args := append([]any{projectID.String()}, tagArgs...)
		args = append(args, limit)
		rows, err = r.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT id, time, project_id, agent_id, session_id, content, summary,
			       importance, confidence, access_count, last_accessed, token_count,
			       tags, metadata, consolidated
			FROM episodic_memory
			WHERE project_id = ?
			  AND EXISTS (
			      SELECT 1 FROM json_each(tags) AS t WHERE t.value IN (%s)
			  )
			ORDER BY importance DESC, time DESC
			LIMIT ?`, inClause), args...)
	} else {
		args := append(tagArgs, limit)
		rows, err = r.db.QueryContext(ctx, fmt.Sprintf(`
			SELECT id, time, project_id, agent_id, session_id, content, summary,
			       importance, confidence, access_count, last_accessed, token_count,
			       tags, metadata, consolidated
			FROM episodic_memory
			WHERE EXISTS (
			      SELECT 1 FROM json_each(tags) AS t WHERE t.value IN (%s)
			  )
			ORDER BY importance DESC, time DESC
			LIMIT ?`, inClause), args...)
	}
	if err != nil {
		return nil, fmt.Errorf("episodic list by tags: %w", err)
	}
	defer rows.Close()

	return collectEpisodic(rows)
}

// DecayImportance multiplies importance by factor (floored at floor) for unconsolidated
// memories not accessed since olderThan duration ago, excluding emotionally-tagged ones.
func (r *EpisodicRepo) DecayImportance(ctx context.Context, olderThan time.Duration, factor float64, floor float64) (int, error) {
	cutoff := time.Now().UTC().Add(-olderThan)

	res, err := r.db.ExecContext(ctx, `
		UPDATE episodic_memory
		SET importance = MAX(?, importance * ?)
		WHERE last_accessed < ?
		  AND importance > ?
		  AND consolidated = 0
		  AND id NOT IN (
		      SELECT memory_id FROM emotional_tags
		      WHERE valence IN ('danger', 'frustration')
		        AND intensity >= 0.5
		  )`,
		floor,
		factor,
		formatTime(cutoff),
		floor,
	)
	if err != nil {
		return 0, fmt.Errorf("episodic decay importance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("episodic decay rows affected: %w", err)
	}
	return int(n), nil
}

func (r *EpisodicRepo) Delete(ctx context.Context, id uuid.UUID) error {
	idStr := id.String()

	// Remove from FTS5 before deleting the row (rowid lookup requires row to exist).
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM episodic_fts WHERE rowid = (SELECT rowid FROM episodic_memory WHERE id = ?)`,
		idStr,
	)
	if err != nil {
		return fmt.Errorf("episodic fts sync delete: %w", err)
	}

	res, err := r.db.ExecContext(ctx, `DELETE FROM episodic_memory WHERE id = ?`, idStr)
	if err != nil {
		return fmt.Errorf("episodic delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("episodic delete rows affected: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *EpisodicRepo) Count(ctx context.Context, projectID *uuid.UUID) (int, error) {
	var count int
	var err error
	if projectID != nil {
		err = r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM episodic_memory WHERE project_id = ?`, projectID.String()).Scan(&count)
	} else {
		err = r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodic_memory`).Scan(&count)
	}
	if err != nil {
		return 0, fmt.Errorf("episodic count: %w", err)
	}
	return count, nil
}

// scanEpisodic reads a single episodic row from *sql.Row (no embedding column).
func scanEpisodic(row *sql.Row) (*domain.EpisodicMemory, error) {
	var m domain.EpisodicMemory
	var (
		idStr, timeStr, sessionStr   string
		projectStr                   sql.NullString
		lastAccessedStr, tagsStr     string
		metaStr                      string
		consolidated                 int
	)

	err := row.Scan(
		&idStr, &timeStr, &projectStr, &m.AgentID, &sessionStr,
		&m.Content, &m.Summary, &m.Importance, &m.Confidence,
		&m.AccessCount, &lastAccessedStr, &m.TokenCount,
		&tagsStr, &metaStr, &consolidated,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan episodic: %w", err)
	}

	if err := fillEpisodic(&m, idStr, timeStr, sessionStr, projectStr, lastAccessedStr, tagsStr, metaStr, consolidated, nil); err != nil {
		return nil, err
	}
	return &m, nil
}

// collectEpisodic reads all rows from *sql.Rows (no embedding column).
func collectEpisodic(rows *sql.Rows) ([]*domain.EpisodicMemory, error) {
	var result []*domain.EpisodicMemory
	for rows.Next() {
		var m domain.EpisodicMemory
		var (
			idStr, timeStr, sessionStr string
			projectStr                 sql.NullString
			lastAccessedStr, tagsStr   string
			metaStr                    string
			consolidated               int
		)

		err := rows.Scan(
			&idStr, &timeStr, &projectStr, &m.AgentID, &sessionStr,
			&m.Content, &m.Summary, &m.Importance, &m.Confidence,
			&m.AccessCount, &lastAccessedStr, &m.TokenCount,
			&tagsStr, &metaStr, &consolidated,
		)
		if err != nil {
			return nil, fmt.Errorf("scan episodic row: %w", err)
		}

		if err := fillEpisodic(&m, idStr, timeStr, sessionStr, projectStr, lastAccessedStr, tagsStr, metaStr, consolidated, nil); err != nil {
			return nil, err
		}
		result = append(result, &m)
	}
	return result, rows.Err()
}

// collectEpisodicWithBlob reads rows that include the embedding BLOB column.
func collectEpisodicWithBlob(rows *sql.Rows) ([]*domain.EpisodicMemory, error) {
	var result []*domain.EpisodicMemory
	for rows.Next() {
		var m domain.EpisodicMemory
		var (
			idStr, timeStr, sessionStr string
			projectStr                 sql.NullString
			lastAccessedStr, tagsStr   string
			metaStr                    string
			consolidated               int
			embBlob                    []byte
		)

		err := rows.Scan(
			&idStr, &timeStr, &projectStr, &m.AgentID, &sessionStr,
			&m.Content, &m.Summary, &embBlob,
			&m.Importance, &m.Confidence,
			&m.AccessCount, &lastAccessedStr, &m.TokenCount,
			&tagsStr, &metaStr, &consolidated,
		)
		if err != nil {
			return nil, fmt.Errorf("scan episodic+blob row: %w", err)
		}

		if err := fillEpisodic(&m, idStr, timeStr, sessionStr, projectStr, lastAccessedStr, tagsStr, metaStr, consolidated, embBlob); err != nil {
			return nil, err
		}
		result = append(result, &m)
	}
	return result, rows.Err()
}

// fillEpisodic populates an EpisodicMemory from raw scanned values.
// embBlob may be nil if the query did not select the embedding column.
func fillEpisodic(
	m *domain.EpisodicMemory,
	idStr, timeStr, sessionStr string,
	projectStr sql.NullString,
	lastAccessedStr, tagsStr, metaStr string,
	consolidated int,
	embBlob []byte,
) error {
	id, err := uuid.Parse(idStr)
	if err != nil {
		return fmt.Errorf("parse episodic id: %w", err)
	}
	m.ID = id

	sessionID, err := uuid.Parse(sessionStr)
	if err != nil {
		return fmt.Errorf("parse episodic session_id: %w", err)
	}
	m.SessionID = sessionID

	if projectStr.Valid && projectStr.String != "" {
		pid, err := uuid.Parse(projectStr.String)
		if err != nil {
			return fmt.Errorf("parse episodic project_id: %w", err)
		}
		m.ProjectID = &pid
	}

	m.CreatedAt, _ = time.Parse(time.RFC3339Nano, timeStr)
	m.UpdatedAt = m.CreatedAt
	m.LastAccessed, _ = time.Parse(time.RFC3339Nano, lastAccessedStr)
	m.Tier = domain.TierEpisodic
	_ = consolidated // consolidated is an integer flag stored in DB; no exported field on EpisodicMemory

	if tagsStr != "" && tagsStr != "[]" {
		_ = json.Unmarshal([]byte(tagsStr), &m.Tags)
	}
	if metaStr != "" && metaStr != "{}" {
		_ = json.Unmarshal([]byte(metaStr), &m.Metadata)
	}

	if embBlob != nil {
		m.Embedding = DecodeEmbedding(embBlob)
	}
	return nil
}

// marshalTags encodes a string slice as a JSON array.
func marshalTags(tags []string) (string, error) {
	if len(tags) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
