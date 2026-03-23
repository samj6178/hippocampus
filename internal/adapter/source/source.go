package source

import (
	"context"
	"time"
)

// Adapter is the common interface for all knowledge source adapters.
type Adapter interface {
	Name() string
	Search(ctx context.Context, query string, maxResults int) ([]RawResult, error)
}

// RawResult is a single result from a knowledge source.
type RawResult struct {
	Source      string   `json:"source"`
	Title       string   `json:"title"`
	URL         string   `json:"url,omitempty"`
	DOI         string   `json:"doi,omitempty"`
	Authors     []string `json:"authors,omitempty"`
	Abstract    string   `json:"abstract"`
	Year        int      `json:"year,omitempty"`
	Citations   int      `json:"citations,omitempty"`
	Venue       string   `json:"venue,omitempty"`
	Categories  []string `json:"categories,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
}
