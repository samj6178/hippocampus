package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/pkg/vecutil"
)

// Compile-time interface assertion.
var _ domain.SemanticRepo = (*SemanticRepo)(nil)

// SemanticRepo implements domain.SemanticRepo against a SQLite database.
type SemanticRepo struct {
	db *sql.DB
}

// NewSemanticRepo constructs a SemanticRepo backed by db.
func NewSemanticRepo(db *sql.DB) *SemanticRepo {
	return &SemanticRepo{db: db}
}

// Insert persists mem and keeps the FTS5 index in sync.
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
	srcJSON, _ := json.Marshal(srcEpisodes)

	tagsJSON, _ := json.Marshal(tags)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("semantic insert begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		INSERT INTO semantic_memory
			(id, project_id, entity_type, content, summary,
			 embedding, importance, confidence, source_episodes,
			 token_count, tags, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mem.ID.String(), uuidToNullString(mem.ProjectID),
		mem.EntityType, mem.Content, mem.Summary,
		EncodeEmbedding(mem.Embedding),
		mem.Importance, mem.Confidence,
		string(srcJSON),
		mem.TokenCount, string(tagsJSON), string(meta),
	)
	if err != nil {
		return fmt.Errorf("semantic insert: %w", err)
	}

	rowID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("semantic insert last id: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO semantic_fts(rowid, content) VALUES (?, ?)`,
		rowID, mem.Content,
	); err != nil {
		return fmt.Errorf("semantic fts insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("semantic insert commit: %w", err)
	}
	return nil
}

// GetByID returns the semantic memory with the given id, or domain.ErrNotFound.
func (r *SemanticRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.SemanticMemory, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, project_id, entity_type, content, summary,
		       importance, confidence, source_episodes,
		       access_count, last_accessed, token_count, tags, metadata,
		       created_at, updated_at
		FROM semantic_memory
		WHERE id = ?`, id.String())

	m, err := scanSemantic(row)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// SearchSimilar returns the top limit results ordered by cosine similarity to
// embedding within the given project (or across all projects when projectID is nil).
// Uses brute-force: fetches up to 500 candidates by importance and re-ranks.
func (r *SemanticRepo) SearchSimilar(ctx context.Context, embedding []float32, projectID *uuid.UUID, limit int) ([]*domain.SemanticMemory, error) {
	fetch := limit * 10
	if fetch < 500 {
		fetch = 500
	}

	var rows *sql.Rows
	var err error
	if projectID != nil {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, project_id, entity_type, content, summary,
			       embedding, importance, confidence, source_episodes,
			       access_count, last_accessed, token_count, tags, metadata,
			       created_at, updated_at
			FROM semantic_memory
			WHERE project_id = ?
			ORDER BY importance DESC
			LIMIT ?`, projectID.String(), fetch)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, project_id, entity_type, content, summary,
			       embedding, importance, confidence, source_episodes,
			       access_count, last_accessed, token_count, tags, metadata,
			       created_at, updated_at
			FROM semantic_memory
			ORDER BY importance DESC
			LIMIT ?`, fetch)
	}
	if err != nil {
		return nil, fmt.Errorf("semantic search similar query: %w", err)
	}
	defer rows.Close()

	mems, err := collectSemanticWithBlob(rows)
	if err != nil {
		return nil, err
	}
	return rankByCosineSemantic(mems, embedding, limit), nil
}

// SearchBM25 performs a full-text search using the FTS5 index.
func (r *SemanticRepo) SearchBM25(ctx context.Context, query string, projectID *uuid.UUID, limit int) ([]*domain.SemanticMemory, error) {
	var rows *sql.Rows
	var err error

	if projectID != nil {
		rows, err = r.db.QueryContext(ctx, `
			SELECT sm.id, sm.project_id, sm.entity_type, sm.content, sm.summary,
			       sm.importance, sm.confidence, sm.source_episodes,
			       sm.access_count, sm.last_accessed, sm.token_count, sm.tags, sm.metadata,
			       sm.created_at, sm.updated_at
			FROM semantic_memory sm
			JOIN semantic_fts fts ON fts.rowid = sm.rowid
			WHERE fts.content MATCH ? AND sm.project_id = ?
			ORDER BY sm.importance DESC
			LIMIT ?`, query, projectID.String(), limit)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT sm.id, sm.project_id, sm.entity_type, sm.content, sm.summary,
			       sm.importance, sm.confidence, sm.source_episodes,
			       sm.access_count, sm.last_accessed, sm.token_count, sm.tags, sm.metadata,
			       sm.created_at, sm.updated_at
			FROM semantic_memory sm
			JOIN semantic_fts fts ON fts.rowid = sm.rowid
			WHERE fts.content MATCH ?
			ORDER BY sm.importance DESC
			LIMIT ?`, query, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("semantic bm25 search: %w", err)
	}
	defer rows.Close()

	return collectSemantic(rows)
}

// SearchGlobal returns the top limit global (project_id IS NULL) memories
// ordered by cosine similarity to embedding.
func (r *SemanticRepo) SearchGlobal(ctx context.Context, embedding []float32, limit int) ([]*domain.SemanticMemory, error) {
	fetch := limit * 10
	if fetch < 500 {
		fetch = 500
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, project_id, entity_type, content, summary,
		       embedding, importance, confidence, source_episodes,
		       access_count, last_accessed, token_count, tags, metadata,
		       created_at, updated_at
		FROM semantic_memory
		WHERE project_id IS NULL
		ORDER BY importance DESC
		LIMIT ?`, fetch)
	if err != nil {
		return nil, fmt.Errorf("semantic global search query: %w", err)
	}
	defer rows.Close()

	mems, err := collectSemanticWithBlob(rows)
	if err != nil {
		return nil, err
	}
	return rankByCosineSemantic(mems, embedding, limit), nil
}

// ListByProject returns up to limit memories for the given project ordered by
// importance DESC. When projectID is nil, returns all memories regardless of project.
func (r *SemanticRepo) ListByProject(ctx context.Context, projectID *uuid.UUID, limit int) ([]*domain.SemanticMemory, error) {
	var rows *sql.Rows
	var err error

	if projectID != nil {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, project_id, entity_type, content, summary,
			       importance, confidence, source_episodes,
			       access_count, last_accessed, token_count, tags, metadata,
			       created_at, updated_at
			FROM semantic_memory
			WHERE project_id = ?
			ORDER BY importance DESC
			LIMIT ?`, projectID.String(), limit)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, project_id, entity_type, content, summary,
			       importance, confidence, source_episodes,
			       access_count, last_accessed, token_count, tags, metadata,
			       created_at, updated_at
			FROM semantic_memory
			ORDER BY importance DESC
			LIMIT ?`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list semantic by project: %w", err)
	}
	defer rows.Close()

	return collectSemantic(rows)
}

// ListGlobal returns up to limit global memories (project_id IS NULL) ordered
// by importance DESC.
func (r *SemanticRepo) ListGlobal(ctx context.Context, limit int) ([]*domain.SemanticMemory, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, project_id, entity_type, content, summary,
		       importance, confidence, source_episodes,
		       access_count, last_accessed, token_count, tags, metadata,
		       created_at, updated_at
		FROM semantic_memory
		WHERE project_id IS NULL
		ORDER BY importance DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list semantic global: %w", err)
	}
	defer rows.Close()

	return collectSemantic(rows)
}

// ListByEntityType returns up to limit memories of the given entityType within
// the project, ordered by importance DESC. When projectID is nil, searches globally.
func (r *SemanticRepo) ListByEntityType(ctx context.Context, projectID *uuid.UUID, entityType string, limit int) ([]*domain.SemanticMemory, error) {
	var rows *sql.Rows
	var err error

	if projectID != nil {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, project_id, entity_type, content, summary,
			       importance, confidence, source_episodes,
			       access_count, last_accessed, token_count, tags, metadata,
			       created_at, updated_at
			FROM semantic_memory
			WHERE project_id = ? AND entity_type = ?
			ORDER BY importance DESC
			LIMIT ?`, projectID.String(), entityType, limit)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, project_id, entity_type, content, summary,
			       importance, confidence, source_episodes,
			       access_count, last_accessed, token_count, tags, metadata,
			       created_at, updated_at
			FROM semantic_memory
			WHERE entity_type = ?
			ORDER BY importance DESC
			LIMIT ?`, entityType, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list semantic by entity type: %w", err)
	}
	defer rows.Close()

	return collectSemantic(rows)
}

// Update replaces all mutable fields of the memory and syncs the FTS5 index.
func (r *SemanticRepo) Update(ctx context.Context, mem *domain.SemanticMemory) error {
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
		return fmt.Errorf("semantic update begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.ExecContext(ctx, `
		UPDATE semantic_memory SET
			content = ?, summary = ?, importance = ?, confidence = ?,
			entity_type = ?, tags = ?, metadata = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`,
		mem.Content, mem.Summary, mem.Importance, mem.Confidence,
		mem.EntityType, string(tagsJSON), string(meta),
		mem.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("semantic update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("semantic update rows affected: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}

	// Sync FTS5: delete old entry and re-insert with new content.
	var rowID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT rowid FROM semantic_memory WHERE id = ?`, mem.ID.String(),
	).Scan(&rowID); err != nil {
		return fmt.Errorf("semantic update fts rowid: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM semantic_fts WHERE rowid = ?`, rowID,
	); err != nil {
		return fmt.Errorf("semantic update fts delete: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO semantic_fts(rowid, content) VALUES (?, ?)`, rowID, mem.Content,
	); err != nil {
		return fmt.Errorf("semantic update fts insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("semantic update commit: %w", err)
	}
	return nil
}

// UpdateImportance sets the importance score and bumps access metadata.
func (r *SemanticRepo) UpdateImportance(ctx context.Context, id uuid.UUID, importance float64) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE semantic_memory
		SET importance = ?,
		    last_accessed = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
		    access_count  = access_count + 1,
		    updated_at    = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`, importance, id.String())
	if err != nil {
		return fmt.Errorf("semantic update importance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("semantic update importance rows: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DecayImportance multiplies importance by factor (floored at floor) for all
// memories whose last_accessed is older than olderThan.
// Returns the number of rows updated.
func (r *SemanticRepo) DecayImportance(ctx context.Context, olderThan time.Duration, factor float64, floor float64) (int, error) {
	threshold := time.Now().Add(-olderThan).UTC().Format(time.RFC3339Nano)

	res, err := r.db.ExecContext(ctx, `
		UPDATE semantic_memory
		SET importance  = MAX(?, importance * ?),
		    updated_at  = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE last_accessed < ?
		  AND importance > ?`,
		floor, factor, threshold, floor,
	)
	if err != nil {
		return 0, fmt.Errorf("decay semantic importance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("decay semantic importance rows: %w", err)
	}
	return int(n), nil
}

// Delete removes the memory and its FTS5 entry. Returns domain.ErrNotFound when
// no row matches.
func (r *SemanticRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("semantic delete begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var rowID int64
	err = tx.QueryRowContext(ctx,
		`SELECT rowid FROM semantic_memory WHERE id = ?`, id.String(),
	).Scan(&rowID)
	if err == sql.ErrNoRows {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("semantic delete rowid: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM semantic_fts WHERE rowid = ?`, rowID,
	); err != nil {
		return fmt.Errorf("semantic delete fts: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM semantic_memory WHERE id = ?`, id.String(),
	); err != nil {
		return fmt.Errorf("semantic delete: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("semantic delete commit: %w", err)
	}
	return nil
}

// Count returns the number of semantic memories, optionally scoped to a project.
func (r *SemanticRepo) Count(ctx context.Context, projectID *uuid.UUID) (int, error) {
	var count int
	var err error
	if projectID != nil {
		err = r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM semantic_memory WHERE project_id = ?`,
			projectID.String(),
		).Scan(&count)
	} else {
		err = r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM semantic_memory`,
		).Scan(&count)
	}
	if err != nil {
		return 0, fmt.Errorf("count semantic: %w", err)
	}
	return count, nil
}

// --- scan helpers ---

// semanticWithBlob is a transient struct used during brute-force cosine search.
type semanticWithBlob struct {
	mem  domain.SemanticMemory
	blob []byte
}

func scanSemantic(row *sql.Row) (*domain.SemanticMemory, error) {
	var m domain.SemanticMemory
	var projectIDStr sql.NullString
	var metaJSON, tagsJSON, srcJSON []byte
	var lastAccessed, createdAt, updatedAt string

	err := row.Scan(
		&m.ID, &projectIDStr, &m.EntityType, &m.Content, &m.Summary,
		&m.Importance, &m.Confidence, &srcJSON,
		&m.AccessCount, &lastAccessed, &m.TokenCount, &tagsJSON, &metaJSON,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan semantic: %w", err)
	}

	if err := applySemantic(&m, projectIDStr, metaJSON, tagsJSON, srcJSON, lastAccessed, createdAt, updatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

func collectSemantic(rows *sql.Rows) ([]*domain.SemanticMemory, error) {
	var result []*domain.SemanticMemory
	for rows.Next() {
		var m domain.SemanticMemory
		var projectIDStr sql.NullString
		var metaJSON, tagsJSON, srcJSON []byte
		var lastAccessed, createdAt, updatedAt string

		err := rows.Scan(
			&m.ID, &projectIDStr, &m.EntityType, &m.Content, &m.Summary,
			&m.Importance, &m.Confidence, &srcJSON,
			&m.AccessCount, &lastAccessed, &m.TokenCount, &tagsJSON, &metaJSON,
			&createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan semantic row: %w", err)
		}
		if err := applySemantic(&m, projectIDStr, metaJSON, tagsJSON, srcJSON, lastAccessed, createdAt, updatedAt); err != nil {
			return nil, err
		}
		result = append(result, &m)
	}
	return result, rows.Err()
}

// collectSemanticWithBlob scans rows that include an embedding BLOB column.
func collectSemanticWithBlob(rows *sql.Rows) ([]*semanticWithBlob, error) {
	var result []*semanticWithBlob
	for rows.Next() {
		var swb semanticWithBlob
		var projectIDStr sql.NullString
		var metaJSON, tagsJSON, srcJSON, embBlob []byte
		var lastAccessed, createdAt, updatedAt string

		err := rows.Scan(
			&swb.mem.ID, &projectIDStr, &swb.mem.EntityType, &swb.mem.Content, &swb.mem.Summary,
			&embBlob, &swb.mem.Importance, &swb.mem.Confidence, &srcJSON,
			&swb.mem.AccessCount, &lastAccessed, &swb.mem.TokenCount, &tagsJSON, &metaJSON,
			&createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan semantic+blob row: %w", err)
		}
		if err := applySemantic(&swb.mem, projectIDStr, metaJSON, tagsJSON, srcJSON, lastAccessed, createdAt, updatedAt); err != nil {
			return nil, err
		}
		swb.blob = embBlob
		result = append(result, &swb)
	}
	return result, rows.Err()
}

// applySemantic populates derived fields from raw column values.
func applySemantic(m *domain.SemanticMemory, projectIDStr sql.NullString, metaJSON, tagsJSON, srcJSON []byte, lastAccessed, createdAt, updatedAt string) error {
	m.Tier = domain.TierSemantic

	if projectIDStr.Valid && projectIDStr.String != "" {
		id, err := uuid.Parse(projectIDStr.String)
		if err != nil {
			return fmt.Errorf("parse semantic project_id %q: %w", projectIDStr.String, err)
		}
		m.ProjectID = &id
	}

	if t, err := time.Parse(time.RFC3339Nano, lastAccessed); err == nil {
		m.LastAccessed = t
	}
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		m.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
		m.UpdatedAt = t
	}

	if len(tagsJSON) > 0 {
		_ = json.Unmarshal(tagsJSON, &m.Tags)
	}
	if len(metaJSON) > 0 {
		_ = json.Unmarshal(metaJSON, &m.Metadata)
	}
	if len(srcJSON) > 0 {
		_ = json.Unmarshal(srcJSON, &m.SourceEpisodes)
	}
	return nil
}

// rankByCosineSemantic decodes embeddings, computes cosine similarity, sorts
// descending, and returns the top limit results with Similarity set.
func rankByCosineSemantic(candidates []*semanticWithBlob, query []float32, limit int) []*domain.SemanticMemory {
	type scored struct {
		mem   *domain.SemanticMemory
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
	out := make([]*domain.SemanticMemory, limit)
	for i := range out {
		out[i] = results[i].mem
	}
	return out
}

// uuidToNullString converts a *uuid.UUID to a SQL-compatible nullable string.
func uuidToNullString(id *uuid.UUID) sql.NullString {
	if id == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: id.String(), Valid: true}
}
