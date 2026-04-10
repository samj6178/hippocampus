package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

var _ domain.KnowledgeGraphRepo = (*KnowledgeGraphRepo)(nil)

type KnowledgeGraphRepo struct {
	db *sql.DB
}

func NewKnowledgeGraphRepo(db *sql.DB) *KnowledgeGraphRepo {
	return &KnowledgeGraphRepo{db: db}
}

func (r *KnowledgeGraphRepo) Insert(ctx context.Context, triple *domain.KnowledgeTriple) error {
	var validTo *string
	if triple.ValidTo != nil {
		s := formatTime(*triple.ValidTo)
		validTo = &s
	}

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO knowledge_triples
			(id, project_id, subject, predicate, object,
			 valid_from, valid_to, confidence, source_session, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		triple.ID.String(),
		uuidToNullString(triple.ProjectID),
		triple.Subject,
		triple.Predicate,
		triple.Object,
		formatTime(triple.ValidFrom),
		validTo,
		triple.Confidence,
		triple.SourceSession,
		formatTime(triple.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("kg insert: %w", err)
	}
	return nil
}

func (r *KnowledgeGraphRepo) QueryBySubject(ctx context.Context, projectID *uuid.UUID, subject string, asOf *time.Time) ([]*domain.KnowledgeTriple, error) {
	var rows *sql.Rows
	var err error

	if asOf != nil {
		at := formatTime(*asOf)
		if projectID != nil {
			rows, err = r.db.QueryContext(ctx, `
				SELECT id, project_id, subject, predicate, object,
				       valid_from, valid_to, confidence, source_session, created_at
				FROM knowledge_triples
				WHERE project_id = ? AND subject = ?
				  AND valid_from <= ?
				  AND (valid_to IS NULL OR valid_to > ?)
				ORDER BY valid_from DESC
				LIMIT 100`, projectID.String(), subject, at, at)
		} else {
			rows, err = r.db.QueryContext(ctx, `
				SELECT id, project_id, subject, predicate, object,
				       valid_from, valid_to, confidence, source_session, created_at
				FROM knowledge_triples
				WHERE project_id IS NULL AND subject = ?
				  AND valid_from <= ?
				  AND (valid_to IS NULL OR valid_to > ?)
				ORDER BY valid_from DESC
				LIMIT 100`, subject, at, at)
		}
	} else {
		// Current: only valid triples
		if projectID != nil {
			rows, err = r.db.QueryContext(ctx, `
				SELECT id, project_id, subject, predicate, object,
				       valid_from, valid_to, confidence, source_session, created_at
				FROM knowledge_triples
				WHERE project_id = ? AND subject = ?
				  AND valid_to IS NULL
				ORDER BY valid_from DESC
				LIMIT 100`, projectID.String(), subject)
		} else {
			rows, err = r.db.QueryContext(ctx, `
				SELECT id, project_id, subject, predicate, object,
				       valid_from, valid_to, confidence, source_session, created_at
				FROM knowledge_triples
				WHERE project_id IS NULL AND subject = ?
				  AND valid_to IS NULL
				ORDER BY valid_from DESC
				LIMIT 100`, subject)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("kg query by subject: %w", err)
	}
	defer rows.Close()

	return collectTriples(rows)
}

func (r *KnowledgeGraphRepo) QueryByObject(ctx context.Context, projectID *uuid.UUID, object string, asOf *time.Time) ([]*domain.KnowledgeTriple, error) {
	var rows *sql.Rows
	var err error

	if asOf != nil {
		at := formatTime(*asOf)
		if projectID != nil {
			rows, err = r.db.QueryContext(ctx, `
				SELECT id, project_id, subject, predicate, object,
				       valid_from, valid_to, confidence, source_session, created_at
				FROM knowledge_triples
				WHERE project_id = ? AND object = ?
				  AND valid_from <= ?
				  AND (valid_to IS NULL OR valid_to > ?)
				ORDER BY valid_from DESC
				LIMIT 100`, projectID.String(), object, at, at)
		} else {
			rows, err = r.db.QueryContext(ctx, `
				SELECT id, project_id, subject, predicate, object,
				       valid_from, valid_to, confidence, source_session, created_at
				FROM knowledge_triples
				WHERE project_id IS NULL AND object = ?
				  AND valid_from <= ?
				  AND (valid_to IS NULL OR valid_to > ?)
				ORDER BY valid_from DESC
				LIMIT 100`, object, at, at)
		}
	} else {
		if projectID != nil {
			rows, err = r.db.QueryContext(ctx, `
				SELECT id, project_id, subject, predicate, object,
				       valid_from, valid_to, confidence, source_session, created_at
				FROM knowledge_triples
				WHERE project_id = ? AND object = ?
				  AND valid_to IS NULL
				ORDER BY valid_from DESC
				LIMIT 100`, projectID.String(), object)
		} else {
			rows, err = r.db.QueryContext(ctx, `
				SELECT id, project_id, subject, predicate, object,
				       valid_from, valid_to, confidence, source_session, created_at
				FROM knowledge_triples
				WHERE project_id IS NULL AND object = ?
				  AND valid_to IS NULL
				ORDER BY valid_from DESC
				LIMIT 100`, object)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("kg query by object: %w", err)
	}
	defer rows.Close()

	return collectTriples(rows)
}

func (r *KnowledgeGraphRepo) Invalidate(ctx context.Context, projectID *uuid.UUID, subject, predicate, object string, endedAt time.Time) (int, error) {
	ended := formatTime(endedAt)
	var res sql.Result
	var err error

	if projectID != nil {
		res, err = r.db.ExecContext(ctx, `
			UPDATE knowledge_triples
			SET valid_to = ?
			WHERE project_id = ? AND subject = ? AND predicate = ? AND object = ?
			  AND valid_to IS NULL`,
			ended, projectID.String(), subject, predicate, object)
	} else {
		res, err = r.db.ExecContext(ctx, `
			UPDATE knowledge_triples
			SET valid_to = ?
			WHERE project_id IS NULL AND subject = ? AND predicate = ? AND object = ?
			  AND valid_to IS NULL`,
			ended, subject, predicate, object)
	}
	if err != nil {
		return 0, fmt.Errorf("kg invalidate: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (r *KnowledgeGraphRepo) Timeline(ctx context.Context, projectID *uuid.UUID, subject string) ([]*domain.KnowledgeTriple, error) {
	var rows *sql.Rows
	var err error

	if projectID != nil {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, project_id, subject, predicate, object,
			       valid_from, valid_to, confidence, source_session, created_at
			FROM knowledge_triples
			WHERE project_id = ? AND subject = ?
			ORDER BY valid_from ASC
			LIMIT 500`, projectID.String(), subject)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT id, project_id, subject, predicate, object,
			       valid_from, valid_to, confidence, source_session, created_at
			FROM knowledge_triples
			WHERE project_id IS NULL AND subject = ?
			ORDER BY valid_from ASC
			LIMIT 500`, subject)
	}
	if err != nil {
		return nil, fmt.Errorf("kg timeline: %w", err)
	}
	defer rows.Close()

	return collectTriples(rows)
}

func (r *KnowledgeGraphRepo) Stats(ctx context.Context, projectID *uuid.UUID) (int, int, error) {
	var active, expired int

	if projectID != nil {
		err := r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM knowledge_triples WHERE project_id = ? AND valid_to IS NULL`,
			projectID.String()).Scan(&active)
		if err != nil {
			return 0, 0, fmt.Errorf("kg stats active: %w", err)
		}
		err = r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM knowledge_triples WHERE project_id = ? AND valid_to IS NOT NULL`,
			projectID.String()).Scan(&expired)
		if err != nil {
			return 0, 0, fmt.Errorf("kg stats expired: %w", err)
		}
	} else {
		err := r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM knowledge_triples WHERE valid_to IS NULL`).Scan(&active)
		if err != nil {
			return 0, 0, fmt.Errorf("kg stats active: %w", err)
		}
		err = r.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM knowledge_triples WHERE valid_to IS NOT NULL`).Scan(&expired)
		if err != nil {
			return 0, 0, fmt.Errorf("kg stats expired: %w", err)
		}
	}

	return active, expired, nil
}

func collectTriples(rows *sql.Rows) ([]*domain.KnowledgeTriple, error) {
	var result []*domain.KnowledgeTriple
	for rows.Next() {
		var t domain.KnowledgeTriple
		var projectStr sql.NullString
		var validFromStr, createdAtStr string
		var validToStr sql.NullString

		err := rows.Scan(
			&t.ID, &projectStr, &t.Subject, &t.Predicate, &t.Object,
			&validFromStr, &validToStr, &t.Confidence, &t.SourceSession, &createdAtStr,
		)
		if err != nil {
			return nil, fmt.Errorf("scan triple: %w", err)
		}

		if projectStr.Valid && projectStr.String != "" {
			id, err := uuid.Parse(projectStr.String)
			if err != nil {
				return nil, fmt.Errorf("parse triple project_id: %w", err)
			}
			t.ProjectID = &id
		}

		t.ValidFrom, _ = time.Parse(time.RFC3339Nano, validFromStr)
		t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
		if validToStr.Valid {
			vt, _ := time.Parse(time.RFC3339Nano, validToStr.String)
			t.ValidTo = &vt
		}

		result = append(result, &t)
	}
	return result, rows.Err()
}
