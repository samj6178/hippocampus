package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// EmotionalTagRepo implements domain.EmotionalTagRepo against a SQLite database.
type EmotionalTagRepo struct {
	db *sql.DB
}

// NewEmotionalTagRepo constructs an EmotionalTagRepo backed by db.
func NewEmotionalTagRepo(db *sql.DB) *EmotionalTagRepo {
	return &EmotionalTagRepo{db: db}
}

// Insert stores a new emotional tag. Signals (Metadata) are serialised as JSON.
func (r *EmotionalTagRepo) Insert(ctx context.Context, tag *domain.EmotionalTag) error {
	signals, err := json.Marshal(tag.Signals)
	if err != nil || signals == nil {
		signals = []byte("{}")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO emotional_tags
			(id, memory_id, valence, intensity, source, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.New().String(), tag.MemoryID.String(),
		string(tag.Valence), tag.Intensity,
		// source column maps to the signals JSON — preserves detection evidence
		string(signals), now,
	)
	if err != nil {
		return fmt.Errorf("emotional tag insert: %w", err)
	}
	return nil
}

// GetByMemory returns all emotional tags attached to the given memory ID,
// ordered by descending intensity.
func (r *EmotionalTagRepo) GetByMemory(ctx context.Context, memoryID uuid.UUID) ([]*domain.EmotionalTag, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT memory_id, valence, intensity, source, created_at
		FROM emotional_tags
		WHERE memory_id = ?
		ORDER BY intensity DESC`, memoryID.String())
	if err != nil {
		return nil, fmt.Errorf("emotional get by memory: %w", err)
	}
	defer rows.Close()

	return collectEmotionalTags(rows)
}

// GetHighPriority returns the limit highest-intensity emotional tags whose
// memory_id is present in either episodic_memory or semantic_memory for the
// given project. When projectID is nil all tags are eligible.
func (r *EmotionalTagRepo) GetHighPriority(ctx context.Context, projectID *uuid.UUID, limit int) ([]*domain.EmotionalTag, error) {
	var rows *sql.Rows
	var err error

	if projectID == nil {
		rows, err = r.db.QueryContext(ctx, `
			SELECT memory_id, valence, intensity, source, created_at
			FROM emotional_tags
			ORDER BY intensity DESC
			LIMIT ?`, limit)
	} else {
		pidStr := projectID.String()
		rows, err = r.db.QueryContext(ctx, `
			SELECT et.memory_id, et.valence, et.intensity, et.source, et.created_at
			FROM emotional_tags et
			WHERE et.memory_id IN (
				SELECT id FROM episodic_memory WHERE project_id = ?
				UNION
				SELECT id FROM semantic_memory  WHERE project_id = ?
			)
			ORDER BY et.intensity DESC
			LIMIT ?`, pidStr, pidStr, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("emotional get high priority: %w", err)
	}
	defer rows.Close()

	return collectEmotionalTags(rows)
}

// collectEmotionalTags iterates rows and builds EmotionalTag slices.
// The source column holds a JSON object (signals evidence map).
func collectEmotionalTags(rows *sql.Rows) ([]*domain.EmotionalTag, error) {
	var result []*domain.EmotionalTag
	for rows.Next() {
		var (
			tag        domain.EmotionalTag
			memIDStr   string
			valStr     string
			sourceJSON string
			createdStr string
		)
		if err := rows.Scan(&memIDStr, &valStr, &tag.Intensity, &sourceJSON, &createdStr); err != nil {
			return nil, fmt.Errorf("scan emotional tag: %w", err)
		}

		var err error
		tag.MemoryID, err = uuid.Parse(memIDStr)
		if err != nil {
			return nil, fmt.Errorf("parse emotional tag memory_id: %w", err)
		}
		tag.Valence = domain.Valence(valStr)

		if sourceJSON != "" && sourceJSON != "{}" && sourceJSON != "null" {
			if err := json.Unmarshal([]byte(sourceJSON), &tag.Signals); err != nil {
				tag.Signals = nil // tolerate malformed JSON
			}
		}

		tag.CreatedAt, err = parseTimestamp(createdStr)
		if err != nil {
			return nil, fmt.Errorf("parse emotional tag created_at: %w", err)
		}

		result = append(result, &tag)
	}
	return result, rows.Err()
}
