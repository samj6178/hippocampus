package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// HackerNewsAdapter searches Hacker News via the Algolia API.
type HackerNewsAdapter struct {
	client *http.Client
}

func NewHackerNewsAdapter() *HackerNewsAdapter {
	return &HackerNewsAdapter{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (h *HackerNewsAdapter) Name() string { return "hackernews" }

func (h *HackerNewsAdapter) Search(ctx context.Context, query string, maxResults int) ([]RawResult, error) {
	apiURL := fmt.Sprintf(
		"https://hn.algolia.com/api/v1/search?query=%s&hitsPerPage=%d&tags=story",
		url.QueryEscape(query), maxResults,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("hackernews: create request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hackernews: request: %w", err)
	}
	defer resp.Body.Close()

	var hnResp struct {
		Hits []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Points  int    `json:"points"`
			StoryID int    `json:"story_id"`
			Author  string `json:"author"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&hnResp); err != nil {
		return nil, fmt.Errorf("hackernews: decode: %w", err)
	}

	var results []RawResult
	for _, hit := range hnResp.Hits {
		hitURL := hit.URL
		if hitURL == "" {
			hitURL = fmt.Sprintf("https://news.ycombinator.com/item?id=%d", hit.StoryID)
		}
		results = append(results, RawResult{
			Source:   "hackernews",
			Title:    hit.Title,
			URL:      hitURL,
			Abstract: fmt.Sprintf("[%d points] %s (by %s)", hit.Points, hit.Title, hit.Author),
			Citations: hit.Points,
		})
	}

	return results, nil
}
