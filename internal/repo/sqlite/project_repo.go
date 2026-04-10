package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// ProjectRepo implements domain.ProjectRepo using SQLite.
type ProjectRepo struct {
	db *sql.DB
}

// NewProjectRepo creates a ProjectRepo backed by the given *sql.DB.
func NewProjectRepo(db *sql.DB) *ProjectRepo {
	return &ProjectRepo{db: db}
}

func (r *ProjectRepo) Create(ctx context.Context, project *domain.Project) error {
	meta, err := marshalJSON(project.Metadata)
	if err != nil {
		return fmt.Errorf("project create marshal metadata: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO projects (id, slug, display_name, description, root_path, is_active, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		project.ID.String(),
		project.Slug,
		project.DisplayName,
		project.Description,
		project.RootPath,
		boolToInt(project.IsActive),
		meta,
		formatTime(project.CreatedAt),
		formatTime(project.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("project create: %w", err)
	}
	return nil
}

func (r *ProjectRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, slug, display_name, description, root_path, is_active, metadata, created_at, updated_at
		FROM projects
		WHERE id = ?
		LIMIT 1`, id.String())

	p, err := scanProject(row)
	if err != nil {
		return nil, fmt.Errorf("project get by id: %w", err)
	}
	return p, nil
}

func (r *ProjectRepo) GetBySlug(ctx context.Context, slug string) (*domain.Project, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, slug, display_name, description, root_path, is_active, metadata, created_at, updated_at
		FROM projects
		WHERE slug = ?
		LIMIT 1`, slug)

	p, err := scanProject(row)
	if err != nil {
		return nil, fmt.Errorf("project get by slug: %w", err)
	}
	return p, nil
}

func (r *ProjectRepo) List(ctx context.Context) ([]*domain.Project, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, slug, display_name, description, root_path, is_active, metadata, created_at, updated_at
		FROM projects
		ORDER BY slug ASC`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var result []*domain.Project
	for rows.Next() {
		p, err := scanProjectRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *ProjectRepo) Update(ctx context.Context, project *domain.Project) error {
	meta, err := marshalJSON(project.Metadata)
	if err != nil {
		return fmt.Errorf("project update marshal metadata: %w", err)
	}

	res, err := r.db.ExecContext(ctx, `
		UPDATE projects SET
			display_name = ?, description = ?, root_path = ?,
			is_active = ?, metadata = ?, updated_at = ?
		WHERE id = ?`,
		project.DisplayName,
		project.Description,
		project.RootPath,
		boolToInt(project.IsActive),
		meta,
		formatTime(time.Now()),
		project.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("project update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("project update rows affected: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ProjectRepo) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id.String())
	if err != nil {
		return fmt.Errorf("project delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("project delete rows affected: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ProjectRepo) GetStats(ctx context.Context, id uuid.UUID) (*domain.ProjectStats, error) {
	var stats domain.ProjectStats
	stats.ProjectID = id
	stats.ByTier = make(map[domain.MemoryTier]int)

	var slug string
	err := r.db.QueryRowContext(ctx, `SELECT slug FROM projects WHERE id = ? LIMIT 1`, id.String()).Scan(&slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get project slug: %w", err)
	}
	stats.Slug = slug

	var epCount int
	r.db.QueryRowContext(ctx, //nolint:errcheck // count always returns a row
		`SELECT COUNT(*) FROM episodic_memory WHERE project_id = ?`, id.String()).Scan(&epCount)
	stats.ByTier[domain.TierEpisodic] = epCount

	var semCount int
	r.db.QueryRowContext(ctx, //nolint:errcheck // count always returns a row
		`SELECT COUNT(*) FROM semantic_memory WHERE project_id = ?`, id.String()).Scan(&semCount)
	stats.ByTier[domain.TierSemantic] = semCount

	var procCount int
	r.db.QueryRowContext(ctx, //nolint:errcheck // count always returns a row
		`SELECT COUNT(*) FROM procedural_memory WHERE project_id = ?`, id.String()).Scan(&procCount)
	stats.ByTier[domain.TierProcedural] = procCount

	// last_active: newest episodic row for this project, or zero time if none.
	var lastActiveStr sql.NullString
	r.db.QueryRowContext(ctx, //nolint:errcheck // MAX always returns a row (may be NULL)
		`SELECT MAX(time) FROM episodic_memory WHERE project_id = ?`, id.String()).Scan(&lastActiveStr)
	if lastActiveStr.Valid && lastActiveStr.String != "" {
		t, err := time.Parse(time.RFC3339Nano, lastActiveStr.String)
		if err == nil {
			stats.LastActive = t
		}
	}

	return &stats, nil
}

// scanProject reads a single project from a *sql.Row.
func scanProject(row *sql.Row) (*domain.Project, error) {
	var p domain.Project
	var idStr, createdAtStr, updatedAtStr, metaStr string
	var isActive int

	err := row.Scan(&idStr, &p.Slug, &p.DisplayName, &p.Description,
		&p.RootPath, &isActive, &metaStr, &createdAtStr, &updatedAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan project: %w", err)
	}

	if err := fillProject(&p, idStr, isActive, metaStr, createdAtStr, updatedAtStr); err != nil {
		return nil, err
	}
	return &p, nil
}

// scanProjectRow reads a single project from *sql.Rows (multi-row query).
func scanProjectRow(rows *sql.Rows) (*domain.Project, error) {
	var p domain.Project
	var idStr, createdAtStr, updatedAtStr, metaStr string
	var isActive int

	err := rows.Scan(&idStr, &p.Slug, &p.DisplayName, &p.Description,
		&p.RootPath, &isActive, &metaStr, &createdAtStr, &updatedAtStr)
	if err != nil {
		return nil, fmt.Errorf("scan project row: %w", err)
	}

	if err := fillProject(&p, idStr, isActive, metaStr, createdAtStr, updatedAtStr); err != nil {
		return nil, err
	}
	return &p, nil
}

func fillProject(p *domain.Project, idStr string, isActive int, metaStr, createdAtStr, updatedAtStr string) error {
	id, err := uuid.Parse(idStr)
	if err != nil {
		return fmt.Errorf("parse project id: %w", err)
	}
	p.ID = id
	p.IsActive = isActive != 0

	if metaStr != "" && metaStr != "{}" {
		_ = json.Unmarshal([]byte(metaStr), &p.Metadata)
	}

	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtStr)
	return nil
}

// marshalJSON marshals v to a JSON string. Returns "{}" on nil/empty map.
func marshalJSON(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// formatTime formats t as RFC3339Nano for storage.
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// boolToInt converts a bool to SQLite's integer representation (0/1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
