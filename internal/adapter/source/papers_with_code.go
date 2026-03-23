package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// PapersWithCodeAdapter queries Papers With Code for SOTA benchmarks and methods.
type PapersWithCodeAdapter struct {
	client *http.Client
}

func NewPapersWithCodeAdapter() *PapersWithCodeAdapter {
	return &PapersWithCodeAdapter{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *PapersWithCodeAdapter) Name() string { return "papers_with_code" }

func (p *PapersWithCodeAdapter) Search(ctx context.Context, query string, maxResults int) ([]RawResult, error) {
	apiURL := fmt.Sprintf(
		"https://paperswithcode.com/api/v1/papers/?q=%s&items_per_page=%d&ordering=-stars",
		url.QueryEscape(query), maxResults,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("papers_with_code: create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("papers_with_code: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("papers_with_code: status %d", resp.StatusCode)
	}

	var pwcResp struct {
		Results []struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			Abstract string `json:"abstract"`
			URLAbsPDF string `json:"url_abs"`
			Authors  []string `json:"authors"`
			Published string `json:"published"`
			ArxivID  string `json:"arxiv_id"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pwcResp); err != nil {
		return nil, fmt.Errorf("papers_with_code: decode: %w", err)
	}

	var results []RawResult
	for _, paper := range pwcResp.Results {
		if paper.Title == "" {
			continue
		}

		abstract := paper.Abstract
		if len(abstract) > 800 {
			abstract = abstract[:800] + "..."
		}

		paperURL := paper.URLAbsPDF
		if paperURL == "" && paper.ArxivID != "" {
			paperURL = fmt.Sprintf("https://arxiv.org/abs/%s", paper.ArxivID)
		}
		if paperURL == "" {
			paperURL = fmt.Sprintf("https://paperswithcode.com/paper/%s", paper.ID)
		}

		results = append(results, RawResult{
			Source:  "papers_with_code",
			Title:   paper.Title,
			URL:     paperURL,
			Authors: paper.Authors,
			Abstract: abstract,
		})
	}

	return results, nil
}
