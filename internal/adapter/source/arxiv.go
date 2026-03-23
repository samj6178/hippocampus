package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ArxivAdapter searches arXiv papers with optional category filtering.
type ArxivAdapter struct {
	categories []string // e.g. ["cs.AI", "math.OC", "stat.ML"]
	client     *http.Client
}

func NewArxivAdapter(categories ...string) *ArxivAdapter {
	return &ArxivAdapter{
		categories: categories,
		client:     &http.Client{Timeout: 20 * time.Second},
	}
}

func (a *ArxivAdapter) Name() string {
	if len(a.categories) > 0 {
		return fmt.Sprintf("arxiv(%s)", strings.Join(a.categories, ","))
	}
	return "arxiv"
}

func (a *ArxivAdapter) Search(ctx context.Context, query string, maxResults int) ([]RawResult, error) {
	searchQuery := buildArxivQuery(query, a.categories)
	apiURL := fmt.Sprintf("http://export.arxiv.org/api/query?search_query=%s&max_results=%d&sortBy=relevance",
		url.QueryEscape(searchQuery), maxResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("arxiv: create request: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("arxiv: request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return parseArxivXML(string(body)), nil
}

func buildArxivQuery(query string, categories []string) string {
	if len(categories) == 0 {
		return "all:" + query
	}
	catFilter := make([]string, len(categories))
	for i, c := range categories {
		catFilter[i] = "cat:" + c
	}
	return fmt.Sprintf("(%s) AND (%s)", "all:"+query, strings.Join(catFilter, " OR "))
}

func parseArxivXML(content string) []RawResult {
	var results []RawResult
	entries := strings.Split(content, "<entry>")
	for i, entry := range entries {
		if i == 0 {
			continue
		}
		title := extractXMLTag(entry, "title")
		summary := extractXMLTag(entry, "summary")
		link := extractXMLID(entry)

		if title == "" {
			continue
		}
		title = strings.TrimSpace(strings.ReplaceAll(title, "\n", " "))
		summary = strings.TrimSpace(strings.ReplaceAll(summary, "\n", " "))
		if len(summary) > 800 {
			summary = summary[:800] + "..."
		}

		var authors []string
		authorEntries := strings.Split(entry, "<author>")
		for j, ae := range authorEntries {
			if j == 0 {
				continue
			}
			name := extractXMLTag(ae, "name")
			if name != "" {
				authors = append(authors, strings.TrimSpace(name))
			}
		}

		var categories []string
		catEntries := strings.Split(entry, "<category term=\"")
		for j, ce := range catEntries {
			if j == 0 {
				continue
			}
			idx := strings.Index(ce, "\"")
			if idx > 0 {
				categories = append(categories, ce[:idx])
			}
		}

		results = append(results, RawResult{
			Source:     "arxiv",
			Title:      title,
			URL:        strings.TrimSpace(link),
			Authors:    authors,
			Abstract:   summary,
			Categories: categories,
		})
	}
	return results
}

func extractXMLTag(s, tag string) string {
	start := strings.Index(s, "<"+tag+">")
	if start < 0 {
		start = strings.Index(s, "<"+tag+" ")
		if start < 0 {
			return ""
		}
		closeBracket := strings.Index(s[start:], ">")
		if closeBracket < 0 {
			return ""
		}
		start = start + closeBracket + 1
	} else {
		start += len(tag) + 2
	}
	end := strings.Index(s[start:], "</"+tag+">")
	if end < 0 {
		return ""
	}
	return s[start : start+end]
}

func extractXMLID(entry string) string {
	start := strings.Index(entry, "<id>")
	if start < 0 {
		return ""
	}
	start += 4
	end := strings.Index(entry[start:], "</id>")
	if end < 0 {
		return ""
	}
	return entry[start : start+end]
}
