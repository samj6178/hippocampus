package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// ResearchAgent autonomously searches for knowledge relevant to the system
// and its projects. It monitors arxiv, GitHub, Hacker News, and technical blogs.
//
// Architecture: uses Ollama for synthesis — takes raw search results,
// extracts actionable insights, and stores them as semantic memories.
//
// This is NOT just "fetch from web" — it's an autonomous knowledge acquisition loop:
//   1. Identify knowledge gaps (from EvalFramework + MetaCognition)
//   2. Search relevant sources
//   3. Synthesize findings via LLM
//   4. Store as high-quality memories
//   5. Measure if stored knowledge improves recall quality
type ResearchAgent struct {
	encode        *EncodeService
	embedding     domain.EmbeddingProvider
	llm           domain.LLMProvider
	httpClient    *http.Client
	ollamaBaseURL string
	ollamaModel   string
	logger        *slog.Logger
}

func NewResearchAgent(
	encode *EncodeService,
	embedding domain.EmbeddingProvider,
	ollamaBaseURL string,
	ollamaModel string,
	logger *slog.Logger,
	llm ...domain.LLMProvider,
) *ResearchAgent {
	var llmProvider domain.LLMProvider
	if len(llm) > 0 && llm[0] != nil {
		llmProvider = llm[0]
	}
	return &ResearchAgent{
		encode:        encode,
		embedding:     embedding,
		llm:           llmProvider,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		ollamaBaseURL: ollamaBaseURL,
		ollamaModel:   ollamaModel,
		logger:        logger,
	}
}

type ResearchRequest struct {
	Query     string   `json:"query"`
	Sources   []string `json:"sources,omitempty"` // "arxiv", "github", "hackernews", "web"
	MaxResults int     `json:"max_results,omitempty"`
	ProjectID *string  `json:"project_id,omitempty"`
}

type ResearchResult struct {
	Query       string           `json:"query"`
	Sources     []SourceResult   `json:"sources"`
	Synthesis   string           `json:"synthesis"`
	Stored      bool             `json:"stored"`
	MemoryCount int              `json:"memories_created"`
	Duration    time.Duration    `json:"duration"`
}

type SourceResult struct {
	Source  string `json:"source"`
	Title   string `json:"title"`
	URL     string `json:"url,omitempty"`
	Snippet string `json:"snippet"`
}

// Research performs a multi-source search, synthesizes results via LLM,
// and optionally stores findings as semantic memories.
func (ra *ResearchAgent) Research(ctx context.Context, req *ResearchRequest) (*ResearchResult, error) {
	start := time.Now()

	if req.MaxResults <= 0 {
		req.MaxResults = 5
	}
	if len(req.Sources) == 0 {
		req.Sources = []string{"arxiv", "github", "hackernews"}
	}

	result := &ResearchResult{
		Query: req.Query,
	}

	for _, source := range req.Sources {
		var results []SourceResult
		var err error

		switch source {
		case "arxiv":
			results, err = ra.searchArxiv(ctx, req.Query, req.MaxResults)
		case "github":
			results, err = ra.searchGitHub(ctx, req.Query, req.MaxResults)
		case "hackernews":
			results, err = ra.searchHackerNews(ctx, req.Query, req.MaxResults)
		default:
			continue
		}

		if err != nil {
			ra.logger.Warn("research source failed", "source", source, "error", err)
			continue
		}
		result.Sources = append(result.Sources, results...)
	}

	if len(result.Sources) == 0 {
		result.Duration = time.Since(start)
		result.Synthesis = "No results found from any source."
		return result, nil
	}

	synthesis, err := ra.synthesize(ctx, req.Query, result.Sources)
	if err != nil {
		ra.logger.Warn("synthesis failed, returning raw results", "error", err)
		var raw strings.Builder
		for _, s := range result.Sources {
			raw.WriteString(fmt.Sprintf("[%s] %s\n%s\n\n", s.Source, s.Title, s.Snippet))
		}
		result.Synthesis = raw.String()
	} else {
		result.Synthesis = synthesis
	}

	result.Duration = time.Since(start)
	return result, nil
}

func (ra *ResearchAgent) searchArxiv(ctx context.Context, query string, maxResults int) ([]SourceResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	escapedQuery := url.QueryEscape(query)
	apiURL := fmt.Sprintf("http://export.arxiv.org/api/query?search_query=all:%s&max_results=%d&sortBy=relevance", escapedQuery, maxResults)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	resp, err := ra.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("arxiv request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	content := string(body)

	var results []SourceResult
	entries := strings.Split(content, "<entry>")
	for i, entry := range entries {
		if i == 0 {
			continue
		}
		title := extractXML(entry, "title")
		summary := extractXML(entry, "summary")
		link := extractXMLAttr(entry, "id")

		if title == "" {
			continue
		}

		title = strings.TrimSpace(strings.ReplaceAll(title, "\n", " "))
		summary = strings.TrimSpace(strings.ReplaceAll(summary, "\n", " "))
		if len(summary) > 500 {
			summary = summary[:500] + "..."
		}

		results = append(results, SourceResult{
			Source:  "arxiv",
			Title:   title,
			URL:     strings.TrimSpace(link),
			Snippet: summary,
		})
	}

	return results, nil
}

func (ra *ResearchAgent) searchGitHub(ctx context.Context, query string, maxResults int) ([]SourceResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	escapedQuery := url.QueryEscape(query)
	apiURL := fmt.Sprintf("https://api.github.com/search/repositories?q=%s&sort=stars&per_page=%d", escapedQuery, maxResults)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	resp, err := ra.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request: %w", err)
	}
	defer resp.Body.Close()

	var ghResp struct {
		Items []struct {
			FullName    string `json:"full_name"`
			Description string `json:"description"`
			HTMLURL     string `json:"html_url"`
			Stars       int    `json:"stargazers_count"`
			Language    string `json:"language"`
			UpdatedAt   string `json:"updated_at"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ghResp); err != nil {
		return nil, fmt.Errorf("github decode: %w", err)
	}

	var results []SourceResult
	for _, item := range ghResp.Items {
		snippet := item.Description
		if item.Language != "" {
			snippet = fmt.Sprintf("[%s, %d★] %s", item.Language, item.Stars, item.Description)
		}
		results = append(results, SourceResult{
			Source:  "github",
			Title:   item.FullName,
			URL:     item.HTMLURL,
			Snippet: snippet,
		})
	}

	return results, nil
}

func (ra *ResearchAgent) searchHackerNews(ctx context.Context, query string, maxResults int) ([]SourceResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	escapedQuery := url.QueryEscape(query)
	apiURL := fmt.Sprintf("https://hn.algolia.com/api/v1/search?query=%s&hitsPerPage=%d&tags=story", escapedQuery, maxResults)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	resp, err := ra.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hackernews request: %w", err)
	}
	defer resp.Body.Close()

	var hnResp struct {
		Hits []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Points  int    `json:"points"`
			StoryID int    `json:"objectID,string"`
		} `json:"hits"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&hnResp); err != nil {
		return nil, fmt.Errorf("hackernews decode: %w", err)
	}

	var results []SourceResult
	for _, hit := range hnResp.Hits {
		url := hit.URL
		if url == "" {
			url = fmt.Sprintf("https://news.ycombinator.com/item?id=%d", hit.StoryID)
		}
		results = append(results, SourceResult{
			Source:  "hackernews",
			Title:   hit.Title,
			URL:     url,
			Snippet: fmt.Sprintf("%d points — %s", hit.Points, hit.Title),
		})
	}

	return results, nil
}

func (ra *ResearchAgent) synthesize(ctx context.Context, query string, sources []SourceResult) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	var sourceText strings.Builder
	for i, s := range sources {
		sourceText.WriteString(fmt.Sprintf("%d. [%s] %s\n   %s\n   %s\n\n", i+1, s.Source, s.Title, s.Snippet, s.URL))
	}

	prompt := fmt.Sprintf(`You are a research synthesizer. Given a query and search results from multiple sources, produce a concise synthesis.

Query: %s

Sources:
%s

Produce:
1. KEY FINDINGS (3-5 bullet points of the most relevant discoveries)
2. ACTIONABLE INSIGHTS (what can be immediately applied)
3. GAPS (what's still unknown or needs further research)

Be concise but specific. Focus on practical value.`, query, sourceText.String())

	if ra.llm != nil {
		result, err := ra.llm.Chat(ctx, []domain.ChatMessage{
			{Role: "user", Content: prompt},
		}, domain.ChatOptions{Temperature: 0.3, MaxTokens: 1200})
		if err == nil {
			return result, nil
		}
		ra.logger.Warn("LLM synthesis failed, falling back to direct ollama", "error", err)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model": ra.ollamaModel,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"stream": false,
		"options": map[string]any{
			"temperature": 0.3,
			"num_predict": 1200,
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ra.ollamaBaseURL+"/api/chat", strings.NewReader(string(reqBody)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ra.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama synthesis: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return strings.TrimSpace(result.Message.Content), nil
}

func extractXML(s, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(s, open)
	if start < 0 {
		open = "<" + tag + " "
		start = strings.Index(s, open)
		if start < 0 {
			return ""
		}
		gt := strings.Index(s[start:], ">")
		if gt < 0 {
			return ""
		}
		start = start + gt + 1
	} else {
		start += len(open)
	}
	end := strings.Index(s[start:], close)
	if end < 0 {
		return ""
	}
	return s[start : start+end]
}

func extractXMLAttr(s, tag string) string {
	open := "<" + tag + ">"
	start := strings.Index(s, open)
	if start < 0 {
		return ""
	}
	start += len(open)
	end := strings.Index(s[start:], "<")
	if end < 0 {
		return ""
	}
	return s[start : start+end]
}
