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

// CausalRepo implements domain.CausalRepo against a SQLite database.
type CausalRepo struct {
	db *sql.DB
}

// NewCausalRepo constructs a CausalRepo backed by db.
func NewCausalRepo(db *sql.DB) *CausalRepo {
	return &CausalRepo{db: db}
}

// Insert stores a new causal link. Evidence and CounterEvidence are serialised
// as JSON arrays of UUID strings.
func (r *CausalRepo) Insert(ctx context.Context, link *domain.CausalLink) error {
	evidence, err := marshalUUIDs(link.Evidence)
	if err != nil {
		return fmt.Errorf("causal insert marshal evidence: %w", err)
	}
	counter, err := marshalUUIDs(link.CounterEvidence)
	if err != nil {
		return fmt.Errorf("causal insert marshal counter evidence: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO causal_links
			(id, cause_id, effect_id, relation_type, confidence,
			 evidence, counter_evidence, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		link.ID.String(), link.CauseID.String(), link.EffectID.String(),
		string(link.Relation), link.Confidence,
		evidence, counter, now, now,
	)
	if err != nil {
		return fmt.Errorf("causal insert: %w", err)
	}
	return nil
}

// GetByID returns the causal link with the given ID or domain.ErrNotFound.
func (r *CausalRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.CausalLink, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, cause_id, effect_id, relation_type, confidence,
		       evidence, counter_evidence, created_at, updated_at
		FROM causal_links
		WHERE id = ?
		LIMIT 1`, id.String())

	link, err := scanCausal(row)
	if err != nil {
		return nil, fmt.Errorf("causal get by id: %w", err)
	}
	return link, nil
}

// GetCauses returns all causal links where the given ID is the effect.
func (r *CausalRepo) GetCauses(ctx context.Context, effectID uuid.UUID) ([]*domain.CausalLink, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, cause_id, effect_id, relation_type, confidence,
		       evidence, counter_evidence, created_at, updated_at
		FROM causal_links
		WHERE effect_id = ?
		ORDER BY confidence DESC`, effectID.String())
	if err != nil {
		return nil, fmt.Errorf("causal get causes: %w", err)
	}
	defer rows.Close()

	return collectCausal(rows)
}

// GetEffects returns all causal links where the given ID is the cause.
func (r *CausalRepo) GetEffects(ctx context.Context, causeID uuid.UUID) ([]*domain.CausalLink, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, cause_id, effect_id, relation_type, confidence,
		       evidence, counter_evidence, created_at, updated_at
		FROM causal_links
		WHERE cause_id = ?
		ORDER BY confidence DESC`, causeID.String())
	if err != nil {
		return nil, fmt.Errorf("causal get effects: %w", err)
	}
	defer rows.Close()

	return collectCausal(rows)
}

// AddEvidence appends episodeID to the evidence array of the link and raises
// confidence by 0.05 (capped at 1.0).
func (r *CausalRepo) AddEvidence(ctx context.Context, id uuid.UUID, episodeID uuid.UUID) error {
	return r.appendToArray(ctx, id, episodeID, "evidence", +0.05)
}

// AddCounterEvidence appends episodeID to the counter_evidence array and
// lowers confidence by 0.10 (floored at 0.0).
func (r *CausalRepo) AddCounterEvidence(ctx context.Context, id uuid.UUID, episodeID uuid.UUID) error {
	return r.appendToArray(ctx, id, episodeID, "counter_evidence", -0.10)
}

// Delete removes a causal link. Returns domain.ErrNotFound if no row was deleted.
func (r *CausalRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM causal_links WHERE id = ?`, id.String())
	if err != nil {
		return fmt.Errorf("causal delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("causal delete rows affected: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// appendToArray reads the current JSON UUID array in column col, appends
// episodeID, writes it back, and adjusts confidence by delta.
// This is a read-modify-write under the serialised SQLite writer connection.
func (r *CausalRepo) appendToArray(ctx context.Context, id uuid.UUID, episodeID uuid.UUID, col string, delta float64) error {
	// SQLite has no array_append — we read, modify, write.
	// The single-writer WAL connection makes this safe against concurrent writes.
	var raw string
	var confidence float64

	// col is an internal constant, never user-supplied — safe to interpolate.
	row := r.db.QueryRowContext(ctx,
		`SELECT `+col+`, confidence FROM causal_links WHERE id = ? LIMIT 1`,
		id.String())
	if err := row.Scan(&raw, &confidence); err != nil {
		if err == sql.ErrNoRows {
			return domain.ErrNotFound
		}
		return fmt.Errorf("causal append read %s: %w", col, err)
	}

	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		ids = []string{} // tolerate corrupt/empty
	}
	ids = append(ids, episodeID.String())

	updated, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("causal append marshal %s: %w", col, err)
	}

	newConf := confidence + delta
	if newConf > 1.0 {
		newConf = 1.0
	} else if newConf < 0.0 {
		newConf = 0.0
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = r.db.ExecContext(ctx,
		`UPDATE causal_links SET `+col+` = ?, confidence = ?, updated_at = ? WHERE id = ?`,
		string(updated), newConf, now, id.String())
	if err != nil {
		return fmt.Errorf("causal append write %s: %w", col, err)
	}
	return nil
}

// scanCausal scans a single causal_links row from a *sql.Row.
func scanCausal(row *sql.Row) (*domain.CausalLink, error) {
	var (
		link              domain.CausalLink
		idStr, causeStr, effectStr string
		relStr            string
		evidenceJSON      string
		counterJSON       string
		createdStr        string
		updatedStr        string
	)

	err := row.Scan(
		&idStr, &causeStr, &effectStr,
		&relStr, &link.Confidence,
		&evidenceJSON, &counterJSON,
		&createdStr, &updatedStr,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan causal: %w", err)
	}

	return parseCausalFields(&link, idStr, causeStr, effectStr, relStr,
		evidenceJSON, counterJSON, createdStr, updatedStr)
}

// collectCausal iterates rows and collects causal links.
func collectCausal(rows *sql.Rows) ([]*domain.CausalLink, error) {
	var result []*domain.CausalLink
	for rows.Next() {
		var (
			link              domain.CausalLink
			idStr, causeStr, effectStr string
			relStr            string
			evidenceJSON      string
			counterJSON       string
			createdStr        string
			updatedStr        string
		)

		err := rows.Scan(
			&idStr, &causeStr, &effectStr,
			&relStr, &link.Confidence,
			&evidenceJSON, &counterJSON,
			&createdStr, &updatedStr,
		)
		if err != nil {
			return nil, fmt.Errorf("scan causal row: %w", err)
		}

		parsed, err := parseCausalFields(&link, idStr, causeStr, effectStr, relStr,
			evidenceJSON, counterJSON, createdStr, updatedStr)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, rows.Err()
}

// parseCausalFields fills link from raw string values after scanning.
func parseCausalFields(
	link *domain.CausalLink,
	idStr, causeStr, effectStr, relStr,
	evidenceJSON, counterJSON,
	createdStr, updatedStr string,
) (*domain.CausalLink, error) {
	var err error

	link.ID, err = uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("parse causal id: %w", err)
	}
	link.CauseID, err = uuid.Parse(causeStr)
	if err != nil {
		return nil, fmt.Errorf("parse cause id: %w", err)
	}
	link.EffectID, err = uuid.Parse(effectStr)
	if err != nil {
		return nil, fmt.Errorf("parse effect id: %w", err)
	}
	link.Relation = domain.CausalRelation(relStr)

	link.Evidence, err = unmarshalUUIDs(evidenceJSON)
	if err != nil {
		return nil, fmt.Errorf("unmarshal evidence: %w", err)
	}
	link.CounterEvidence, err = unmarshalUUIDs(counterJSON)
	if err != nil {
		return nil, fmt.Errorf("unmarshal counter evidence: %w", err)
	}

	link.CreatedAt, err = time.Parse(time.RFC3339Nano, createdStr)
	if err != nil {
		link.CreatedAt, err = time.Parse(time.RFC3339, createdStr)
		if err != nil {
			return nil, fmt.Errorf("parse causal created_at: %w", err)
		}
	}
	link.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedStr)
	if err != nil {
		link.UpdatedAt, err = time.Parse(time.RFC3339, updatedStr)
		if err != nil {
			return nil, fmt.Errorf("parse causal updated_at: %w", err)
		}
	}

	return link, nil
}

// marshalUUIDs serialises a UUID slice to a JSON array of strings.
// A nil slice is normalised to an empty array.
func marshalUUIDs(ids []uuid.UUID) (string, error) {
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = id.String()
	}
	b, err := json.Marshal(strs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// unmarshalUUIDs parses a JSON array of UUID strings.
func unmarshalUUIDs(raw string) ([]uuid.UUID, error) {
	if raw == "" || raw == "null" {
		return []uuid.UUID{}, nil
	}
	var strs []string
	if err := json.Unmarshal([]byte(raw), &strs); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(strs))
	for _, s := range strs {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("parse uuid %q: %w", s, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
