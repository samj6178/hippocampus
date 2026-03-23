package domain

import (
	"time"

	"github.com/google/uuid"
)

// Project represents a namespaced memory scope.
// Global memories have ProjectID = nil.
// Switching projects changes the active filter for RECALL.
type Project struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	DisplayName string    `json:"display_name"`
	Description string    `json:"description,omitempty"`
	RootPath    string    `json:"root_path,omitempty"` // filesystem path for code ingestion
	IsActive    bool      `json:"is_active"`
	Metadata    Metadata  `json:"metadata,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectStats holds aggregate counts for a project's memory tiers.
type ProjectStats struct {
	ProjectID  uuid.UUID      `json:"project_id"`
	Slug       string         `json:"slug"`
	ByTier     map[MemoryTier]int `json:"by_tier"`
	LastActive time.Time      `json:"last_active"`
}
