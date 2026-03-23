package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// ProjectService manages project namespaces for memory isolation.
// Each project has its own episodic and procedural memory space,
// while semantic memory can be either project-scoped or global.
type ProjectService struct {
	repo   domain.ProjectRepo
	logger *slog.Logger
}

func NewProjectService(repo domain.ProjectRepo, logger *slog.Logger) *ProjectService {
	return &ProjectService{repo: repo, logger: logger}
}

func (s *ProjectService) Create(ctx context.Context, slug, displayName, description, rootPath string) (*domain.Project, error) {
	if slug == "" {
		return nil, fmt.Errorf("project slug is required")
	}

	existing, err := s.repo.GetBySlug(ctx, slug)
	if err == nil && existing != nil {
		return nil, fmt.Errorf("project with slug %q already exists", slug)
	}

	project := &domain.Project{
		ID:          uuid.New(),
		Slug:        slug,
		DisplayName: displayName,
		Description: description,
		RootPath:    rootPath,
		IsActive:    true,
	}

	if err := s.repo.Create(ctx, project); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}

	s.logger.Info("project created", "slug", slug, "id", project.ID)
	return project, nil
}

func (s *ProjectService) GetBySlug(ctx context.Context, slug string) (*domain.Project, error) {
	return s.repo.GetBySlug(ctx, slug)
}

func (s *ProjectService) List(ctx context.Context) ([]*domain.Project, error) {
	return s.repo.List(ctx)
}

func (s *ProjectService) GetStats(ctx context.Context, id uuid.UUID) (*domain.ProjectStats, error) {
	return s.repo.GetStats(ctx, id)
}

func (s *ProjectService) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	s.logger.Info("project deleted", "id", id)
	return nil
}

// FindOrCreate finds an existing project by slug, or creates one from auto-detected metadata.
// Used for automatic project detection on MCP initialize and tool calls.
func (s *ProjectService) FindOrCreate(ctx context.Context, identity *ProjectIdentity) (*domain.Project, error) {
	if identity == nil || identity.Slug == "" {
		return nil, fmt.Errorf("project identity is nil or has empty slug")
	}

	existing, err := s.repo.GetBySlug(ctx, identity.Slug)
	if err == nil && existing != nil {
		if existing.RootPath == "" && identity.RootPath != "" {
			existing.RootPath = identity.RootPath
		}
		s.logger.Debug("project found", "slug", identity.Slug, "id", existing.ID)
		return existing, nil
	}

	project := &domain.Project{
		ID:          uuid.New(),
		Slug:        identity.Slug,
		DisplayName: identity.Name,
		Description: identity.Description,
		RootPath:    identity.RootPath,
		IsActive:    true,
	}

	if err := s.repo.Create(ctx, project); err != nil {
		return nil, fmt.Errorf("auto-create project: %w", err)
	}

	s.logger.Info("project auto-created", "slug", identity.Slug, "id", project.ID, "language", identity.Language)
	return project, nil
}
