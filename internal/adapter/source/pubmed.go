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

// PubMedAdapter searches NCBI PubMed for biomedical literature.
type PubMedAdapter struct {
	client *http.Client
}

func NewPubMedAdapter() *PubMedAdapter {
	return &PubMedAdapter{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (p *PubMedAdapter) Name() string { return "pubmed" }

func (p *PubMedAdapter) Search(ctx context.Context, query string, maxResults int) ([]RawResult, error) {
	searchURL := fmt.Sprintf(
		"https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi?db=pubmed&term=%s&retmax=%d&retmode=json&sort=relevance",
		url.QueryEscape(query), maxResults,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("pubmed search: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pubmed search request: %w", err)
	}
	defer resp.Body.Close()

	var searchResp struct {
		ESearchResult struct {
			IDList []string `json:"idlist"`
		} `json:"esearchresult"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("pubmed search decode: %w", err)
	}

	if len(searchResp.ESearchResult.IDList) == 0 {
		return nil, nil
	}

	ids := strings.Join(searchResp.ESearchResult.IDList, ",")
	summaryURL := fmt.Sprintf(
		"https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esummary.fcgi?db=pubmed&id=%s&retmode=json",
		ids,
	)

	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, summaryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("pubmed summary: %w", err)
	}

	resp2, err := p.client.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("pubmed summary request: %w", err)
	}
	defer resp2.Body.Close()

	var summaryResp struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&summaryResp); err != nil {
		return nil, fmt.Errorf("pubmed summary decode: %w", err)
	}

	var results []RawResult
	for _, id := range searchResp.ESearchResult.IDList {
		raw, ok := summaryResp.Result[id]
		if !ok {
			continue
		}

		var article struct {
			Title   string `json:"title"`
			Source  string `json:"source"`
			PubDate string `json:"pubdate"`
			Authors []struct {
				Name string `json:"name"`
			} `json:"authors"`
			DOI string `json:"elocationid"`
		}
		if err := json.Unmarshal(raw, &article); err != nil {
			continue
		}

		if article.Title == "" {
			continue
		}

		authors := make([]string, 0, len(article.Authors))
		for _, a := range article.Authors {
			authors = append(authors, a.Name)
		}

		doi := article.DOI
		if strings.HasPrefix(doi, "doi: ") {
			doi = doi[5:]
		}

		results = append(results, RawResult{
			Source:  "pubmed",
			Title:   article.Title,
			URL:     fmt.Sprintf("https://pubmed.ncbi.nlm.nih.gov/%s/", id),
			DOI:     doi,
			Authors: authors,
			Venue:   article.Source,
		})
	}

	return results, nil
}
