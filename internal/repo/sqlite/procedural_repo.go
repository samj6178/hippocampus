package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/pkg/vecutil"
)

// Compile-time interface assertion.
var _ domain.ProceduralRepo = (*ProceduralRepo)(nil)

// ProceduralRepo implements domain.ProceduralRepo against a SQLite database.
type ProceduralRepo struct {
	db *sql.DB
}

// NewProceduralRepo constructs a ProceduralRepo backed by db.
func NewProceduralRepo(db *sql.DB) *ProceduralRepo {
	return &ProceduralRepo{db: db}
}

// Insert persists mem and keeps the FTS5 index in sync.
func (r *ProceduralRepo) Insert(ctx context.Context, mem *domain.ProceduralMemory) error {
	tags := mem.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, _ := json.Marshal(tags)
	meta, _ := json.Marshal(mem.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("procedural insert begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		INSERT INTO procedural_memory
			(id, project_id, task_type, content,
			 embedding, importance, confidence,
			 success_count, failure_count,
			 token_count, tags, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mem.ID.String(), uuidToNullString(mem.ProjectID),
		mem.TaskType, mem.Content,
		EncodeEmbedding(mem.Embedding),
		mem.Importance, mem.Confidence,
		mem.SuccessCount, mem.FailureCount,
		mem.TokenCount, string(tagsJSON), string(meta),
	)
	if err != nil {
		return fmt.Errorf("procedural insert: %w", err)
	}

	rowID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("procedural insert last id: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO procedural_fts(rowid, content) VALUES (?, ?)`,
		rowID, mem.Content,
	); err != nil {
		return fmt.Errorf("procedural fts insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("procedural insert commit: %w", err)
	}
	return nil
}

// GetByID returns the procedural memory with the given id, or domain.ErrNotFound.
func (r *ProceduralRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.ProceduralMemory, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, project_id, task_type, content,
		       importance, confidence, success_count, failure_count,
		       last_used, token_count, tags, metadata,
		       created_at, updated_at
		FROM procedural_memory
		WHERE id = ?`, id.String())

	return scanProcedural(row)
}

// SearchByTaskType returns the top limit results ordered by cosine similarity to
// embedding within the given project (or across all projects when projectID is nil).
// Uses brute-force: fetches up to 500 candidates by importance and re-ranks.
func (r *ProceduralRepo) SearchByTaskType(ctx context.Context, embedding []float32, projectID *uuid.UUID, limit int) ([]*domain.ProceduralMemory, error) {
	fetch := limit * 10
	if fetch < 500 {
		fetch = 500
	}

	var rows *sql.Rows
	var err error
	if projectID != nil {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, project_id, task_type, content,
			       embedding, importance, confidence,
			       success_count, failure_count,
			       last_used, token_count, tags, metadata,
			       created_at, updated_at
			FROM procedural_memory
			WHERE project_id = ?
			ORDER BY importance DESC
			LIMIT ?`, projectID.String(), fetch)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, project_id, task_type, content,
			       embedding, importance, confidence,
			       success_count, failure_count,
			       last_used, token_count, tags, metadata,
			       created_at, updated_at
			FROM procedural_memory
			ORDER BY importance DESC
			LIMIT ?`, fetch)
	}
	if err != nil {
		return nil, fmt.Errorf("procedural search by task type query: %w", err)
	}
	defer rows.Close()

	candidates, err := collectProceduralWithBlob(rows)
	if err != nil {
		return nil, err
	}
	return rankByCosineProc(candidates, embedding, limit), nil
}

// IncrementSuccess increments success_count and updates last_used.
func (r *ProceduralRepo) IncrementSuccess(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE procedural_memory
		SET success_count = success_count + 1,
		    last_used     = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
		    updated_at    = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`, id.String())
	if err != nil {
		return fmt.Errorf("increment success: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("increment success rows: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// IncrementFailure increments failure_count and updates last_used.
func (r *ProceduralRepo) IncrementFailure(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE procedural_memory
		SET failure_count = failure_count + 1,
		    last_used     = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
		    updated_at    = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`, id.String())
	if err != nil {
		return fmt.Errorf("increment failure: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("increment failure rows: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Delete removes the memory and its FTS5 entry. Returns domain.ErrNotFound when
// no row matches.
func (r *ProceduralRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("procedural delete begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var rowID int64
	err = tx.QueryRowContext(ctx,
		`SELECT rowid FROM procedural_memory WHERE id = ?`, id.String(),
	).Scan(&rowID)
	if err == sql.ErrNoRows {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("procedural delete rowid: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM procedural_fts WHERE rowid = ?`, rowID,
	); err != nil {
		return fmt.Errorf("procedural delete fts: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM procedural_memory WHERE id = ?`, id.String(),
	); err != nil {
		return fmt.Errorf("procedural delete: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("procedural delete commit: %w", err)
	}
	return nil
}

// Count returns the number of procedural memories, optionally scoped to a project.
func (r *ProceduralRepo) Count(ctx context.Context, projectID *uuid.UUID) (int, error) {
	var count int
	var err error
	if projectID != nil {
		err = r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM procedural_memory WHERE project_id = ?`,
			projectID.String(),
		).Scan(&count)
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM procedural_memory`,
		).Scan(&count)
	}
	if err != nil {
		return 0, fmt.Errorf("count procedural: %w", err)
	}
	return count, nil
}

// --- scan helpers ---

// proceduralWithBlob is a transient struct used during brute-force cosine search.
type proceduralWithBlob struct {
	mem  domain.ProceduralMemory
	blob []byte
}

func scanProcedural(row *sql.Row) (*domain.ProceduralMemory, error) {
	var m domain.ProceduralMemory
	var projectIDStr sql.NullString
	var metaJSON, tagsJSON []byte
	var lastUsed sql.NullString
	var createdAt, updatedAt string

	err := row.Scan(
		&m.ID, &projectIDStr, &m.TaskType, &m.Content,
		&m.Importance, &m.Confidence, &m.SuccessCount, &m.FailureCount,
		&lastUsed, &m.TokenCount, &tagsJSON, &metaJSON,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan procedural: %w", err)
	}

	if err := applyProcedural(&m, projectIDStr, metaJSON, tagsJSON, lastUsed, createdAt, updatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

func collectProceduralWithBlob(rows *sql.Rows) ([]*proceduralWithBlob, error) {
	var result []*proceduralWithBlob
	for rows.Next() {
		var pwb proceduralWithBlob
		var projectIDStr sql.NullString
		var metaJSON, tagsJSON, embBlob []byte
		var lastUsed sql.NullString
		var createdAt, updatedAt string

		err := rows.Scan(
			&pwb.mem.ID, &projectIDStr, &pwb.mem.TaskType, &pwb.mem.Content,
			&embBlob, &pwb.mem.Importance, &pwb.mem.Confidence,
			&pwb.mem.SuccessCount, &pwb.mem.FailureCount,
			&lastUsed, &pwb.mem.TokenCount, &tagsJSON, &metaJSON,
			&createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan procedural+blob row: %w", err)
		}
		if err := applyProcedural(&pwb.mem, projectIDStr, metaJSON, tagsJSON, lastUsed, createdAt, updatedAt); err != nil {
			return nil, err
		}
		pwb.blob = embBlob
		result = append(result, &pwb)
	}
	return result, rows.Err()
}

// applyProcedural populates derived fields from raw column values.
func applyProcedural(m *domain.ProceduralMemory, projectIDStr sql.NullString, metaJSON, tagsJSON []byte, lastUsed sql.NullString, createdAt, updatedAt string) error {
	m.Tier = domain.TierProcedural

	if projectIDStr.Valid && projectIDStr.String != "" {
		id, err := uuid.Parse(projectIDStr.String)
		if err != nil {
			return fmt.Errorf("parse procedural project_id %q: %w", projectIDStr.String, err)
		}
		m.ProjectID = &id
	}

	if lastUsed.Valid && lastUsed.String != "" {
		if t, err := parseTimestamp(lastUsed.String); err == nil {
			m.LastAccessed = t
		}
	}
	if t, err := parseTimestamp(createdAt); err == nil {
		m.CreatedAt = t
	}
	if t, err := parseTimestamp(updatedAt); err == nil {
		m.UpdatedAt = t
	}

	if len(tagsJSON) > 0 {
		_ = json.Unmarshal(tagsJSON, &m.Tags)
	}
	if len(metaJSON) > 0 {
		_ = json.Unmarshal(metaJSON, &m.Metadata)
	}
	return nil
}

// rankByCosineProc decodes embeddings, computes cosine similarity, sorts
// descending, and returns the top limit results with Similarity set.
func rankByCosineProc(candidates []*proceduralWithBlob, query []float32, limit int) []*domain.ProceduralMemory {
	type scored struct {
		mem   *domain.ProceduralMemory
		score float64
	}

	results := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		emb := DecodeEmbedding(c.blob)
		sim := vecutil.CosineSimilarity(emb, query)
		m := c.mem
		m.Similarity = sim
		results = append(results, scored{mem: &m, score: sim})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if limit > len(results) {
		limit = len(results)
	}
	out := make([]*domain.ProceduralMemory, limit)
	for i := range out {
		out[i] = results[i].mem
	}
	return out
}
