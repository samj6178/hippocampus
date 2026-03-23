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

type ProjectRepo struct {
	pool *pgxpool.Pool
}

func NewProjectRepo(pool *pgxpool.Pool) *ProjectRepo {
	return &ProjectRepo{pool: pool}
}

func (r *ProjectRepo) Create(ctx context.Context, project *domain.Project) error {
	meta, _ := json.Marshal(project.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO projects (id, slug, display_name, description, root_path,
			is_active, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		project.ID, project.Slug, project.DisplayName, project.Description,
		project.RootPath, project.IsActive, meta,
	)
	if err != nil {
		return fmt.Errorf("project create: %w", err)
	}
	return nil
}

func (r *ProjectRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, slug, display_name, description, root_path,
			is_active, metadata, created_at, updated_at
		FROM projects
		WHERE id = $1`, id)

	return scanProject(row)
}

func (r *ProjectRepo) GetBySlug(ctx context.Context, slug string) (*domain.Project, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, slug, display_name, description, root_path,
			is_active, metadata, created_at, updated_at
		FROM projects
		WHERE slug = $1`, slug)

	return scanProject(row)
}

func (r *ProjectRepo) List(ctx context.Context) ([]*domain.Project, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, slug, display_name, description, root_path,
			is_active, metadata, created_at, updated_at
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
	meta, _ := json.Marshal(project.Metadata)

	tag, err := r.pool.Exec(ctx, `
		UPDATE projects SET
			display_name = $2, description = $3, root_path = $4,
			is_active = $5, metadata = $6, updated_at = NOW()
		WHERE id = $1`,
		project.ID, project.DisplayName, project.Description,
		project.RootPath, project.IsActive, meta,
	)
	if err != nil {
		return fmt.Errorf("project update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ProjectRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("project delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *ProjectRepo) GetStats(ctx context.Context, id uuid.UUID) (*domain.ProjectStats, error) {
	var stats domain.ProjectStats
	stats.ProjectID = id
	stats.ByTier = make(map[domain.MemoryTier]int)

	err := r.pool.QueryRow(ctx,
		`SELECT slug FROM projects WHERE id = $1`, id).Scan(&stats.Slug)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get project slug: %w", err)
	}

	var epCount int
	r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM episodic_memory WHERE project_id = $1`, id).Scan(&epCount)
	stats.ByTier[domain.TierEpisodic] = epCount

	var semCount int
	r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM semantic_memory WHERE project_id = $1`, id).Scan(&semCount)
	stats.ByTier[domain.TierSemantic] = semCount

	var procCount int
	r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM procedural_memory WHERE project_id = $1`, id).Scan(&procCount)
	stats.ByTier[domain.TierProcedural] = procCount

	r.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(time), '1970-01-01') FROM episodic_memory WHERE project_id = $1`,
		id).Scan(&stats.LastActive)

	return &stats, nil
}

func scanProject(row pgx.Row) (*domain.Project, error) {
	var p domain.Project
	var metaJSON []byte

	err := row.Scan(
		&p.ID, &p.Slug, &p.DisplayName, &p.Description, &p.RootPath,
		&p.IsActive, &metaJSON, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan project: %w", err)
	}
	if metaJSON != nil {
		json.Unmarshal(metaJSON, &p.Metadata)
	}
	return &p, nil
}

func scanProjectRow(rows pgx.Rows) (*domain.Project, error) {
	var p domain.Project
	var metaJSON []byte

	err := rows.Scan(
		&p.ID, &p.Slug, &p.DisplayName, &p.Description, &p.RootPath,
		&p.IsActive, &metaJSON, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
	if metaJSON != nil {
		json.Unmarshal(metaJSON, &p.Metadata)
	}
	return &p, nil
}
