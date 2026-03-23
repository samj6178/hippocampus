package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// GitHubAdapter searches GitHub repositories by relevance and star count.
type GitHubAdapter struct {
	client *http.Client
}

func NewGitHubAdapter() *GitHubAdapter {
	return &GitHubAdapter{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (g *GitHubAdapter) Name() string { return "github" }

func (g *GitHubAdapter) Search(ctx context.Context, query string, maxResults int) ([]RawResult, error) {
	apiURL := fmt.Sprintf(
		"https://api.github.com/search/repositories?q=%s&sort=stars&per_page=%d",
		url.QueryEscape(query), maxResults,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("github: create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: request: %w", err)
	}
	defer resp.Body.Close()

	var ghResp struct {
		Items []struct {
			FullName    string `json:"full_name"`
			Description string `json:"description"`
			HTMLURL     string `json:"html_url"`
			Stars       int    `json:"stargazers_count"`
			Language    string `json:"language"`
			Topics      []string `json:"topics"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ghResp); err != nil {
		return nil, fmt.Errorf("github: decode: %w", err)
	}

	var results []RawResult
	for _, repo := range ghResp.Items {
		desc := repo.Description
		if len(desc) > 500 {
			desc = desc[:500] + "..."
		}
		results = append(results, RawResult{
			Source:     "github",
			Title:      repo.FullName,
			URL:        repo.HTMLURL,
			Abstract:   fmt.Sprintf("[%d stars, %s] %s", repo.Stars, repo.Language, desc),
			Citations:  repo.Stars,
			Categories: repo.Topics,
		})
	}

	return results, nil
}
