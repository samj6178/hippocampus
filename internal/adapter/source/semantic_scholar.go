package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SemanticScholarAdapter queries the Semantic Scholar API for academic papers.
// Supports venue filtering to prioritize top-tier conferences (NeurIPS, ICML, ICLR, etc.)
type SemanticScholarAdapter struct {
	venues []string
	client *http.Client
}

func NewSemanticScholarAdapter(venues ...string) *SemanticScholarAdapter {
	return &SemanticScholarAdapter{
		venues: venues,
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (s *SemanticScholarAdapter) Name() string {
	return "semantic_scholar"
}

func (s *SemanticScholarAdapter) Search(ctx context.Context, query string, maxResults int) ([]RawResult, error) {
	apiURL := fmt.Sprintf(
		"https://api.semanticscholar.org/graph/v1/paper/search?query=%s&limit=%d&fields=title,abstract,authors,year,citationCount,venue,externalIds,url",
		url.QueryEscape(query), maxResults,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("semantic scholar: create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("semantic scholar: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("semantic scholar: status %d", resp.StatusCode)
	}

	var ssResp struct {
		Data []struct {
			PaperID     string `json:"paperId"`
			Title       string `json:"title"`
			Abstract    string `json:"abstract"`
			Year        int    `json:"year"`
			Citations   int    `json:"citationCount"`
			Venue       string `json:"venue"`
			URL         string `json:"url"`
			ExternalIDs struct {
				DOI string `json:"DOI"`
			} `json:"externalIds"`
			Authors []struct {
				Name string `json:"name"`
			} `json:"authors"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ssResp); err != nil {
		return nil, fmt.Errorf("semantic scholar: decode: %w", err)
	}

	var results []RawResult
	for _, paper := range ssResp.Data {
		if paper.Title == "" {
			continue
		}
		if len(s.venues) > 0 && !matchesVenue(paper.Venue, s.venues) {
			continue
		}

		authors := make([]string, 0, len(paper.Authors))
		for _, a := range paper.Authors {
			authors = append(authors, a.Name)
		}

		abstract := paper.Abstract
		if len(abstract) > 800 {
			abstract = abstract[:800] + "..."
		}

		paperURL := paper.URL
		if paperURL == "" {
			paperURL = fmt.Sprintf("https://www.semanticscholar.org/paper/%s", paper.PaperID)
		}

		results = append(results, RawResult{
			Source:    "semantic_scholar",
			Title:     paper.Title,
			URL:       paperURL,
			DOI:       paper.ExternalIDs.DOI,
			Authors:   authors,
			Abstract:  abstract,
			Year:      paper.Year,
			Citations: paper.Citations,
			Venue:     paper.Venue,
		})
	}

	return results, nil
}

func matchesVenue(venue string, allowed []string) bool {
	if venue == "" || len(allowed) == 0 {
		return true
	}
	lower := strings.ToLower(venue)
	for _, v := range allowed {
		if strings.Contains(lower, strings.ToLower(v)) {
			return true
		}
	}
	return false
}
