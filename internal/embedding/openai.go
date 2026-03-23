package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/hippocampus-mcp/hippocampus/internal/metrics"
	"golang.org/x/sync/semaphore"
)

const (
	defaultBaseURL   = "https://api.openai.com/v1"
	maxRetries       = 3
	initialBackoff   = 500 * time.Millisecond
	requestTimeout   = 30 * time.Second
)

type openAIRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type openAIResponse struct {
	Data  []openAIEmbedding `json:"data"`
	Usage openAIUsage       `json:"usage"`
	Error *openAIError      `json:"error,omitempty"`
}

type openAIEmbedding struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type openAIUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// OpenAIProvider implements domain.EmbeddingProvider using any
// OpenAI-compatible embedding API (OpenAI, Ollama, vLLM, LiteLLM, etc.)
// with LRU caching, automatic batching, and exponential backoff retry.
type OpenAIProvider struct {
	apiKey   string
	baseURL  string
	model    string
	maxBatch int
	dims     int
	client   *http.Client
	cache    *LRUCache
	sem      *semaphore.Weighted
	logger   *slog.Logger
}

type ProviderOption func(*OpenAIProvider)

func WithHTTPClient(c *http.Client) ProviderOption {
	return func(p *OpenAIProvider) { p.client = c }
}

func WithLogger(l *slog.Logger) ProviderOption {
	return func(p *OpenAIProvider) { p.logger = l }
}

func WithBaseURL(url string) ProviderOption {
	return func(p *OpenAIProvider) { p.baseURL = strings.TrimRight(url, "/") }
}

func WithDimensions(dims int) ProviderOption {
	return func(p *OpenAIProvider) { p.dims = dims }
}

func WithMaxConcurrent(n int) ProviderOption {
	return func(p *OpenAIProvider) {
		if n > 0 {
			p.sem = semaphore.NewWeighted(int64(n))
		}
	}
}

func NewOpenAIProvider(apiKey, model string, maxBatch, cacheSize int, opts ...ProviderOption) *OpenAIProvider {
	p := &OpenAIProvider{
		apiKey:   apiKey,
		baseURL:  defaultBaseURL,
		model:    model,
		maxBatch: maxBatch,
		dims:     768,
		client: &http.Client{
			Timeout: requestTimeout,
		},
		cache:  NewLRUCache(cacheSize),
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(p)
	}
	if p.sem == nil {
		p.sem = semaphore.NewWeighted(3) // default max 3 concurrent embed calls
	}
	return p
}

func (p *OpenAIProvider) embeddingsURL() string {
	return p.baseURL + "/embeddings"
}

func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	text = normalizeText(text)
	if text == "" {
		return make([]float32, p.dims), nil
	}

	if cached, ok := p.cache.Get(p.model, text); ok {
		return cached, nil
	}

	// Acquire semaphore only for actual API calls (after cache check)
	if err := p.sem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("concurrency limit: %w", err)
	}
	defer p.sem.Release(1)

	start := time.Now()
	results, err := p.callAPI(ctx, []string{text})
	metrics.EmbeddingLatency.Observe(time.Since(start).Seconds())
	if err != nil {
		metrics.EmbeddingErrors.Inc()
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("openai: empty response for single embedding")
	}

	p.cache.Put(p.model, text, results[0])
	return results[0], nil
}

func (p *OpenAIProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if err := p.sem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("concurrency limit: %w", err)
	}
	defer p.sem.Release(1)

	normalized := make([]string, len(texts))
	for i, t := range texts {
		normalized[i] = normalizeText(t)
	}

	results := make([][]float32, len(texts))
	var uncachedIndices []int
	var uncachedTexts []string

	for i, t := range normalized {
		if t == "" {
			results[i] = make([]float32, p.dims)
			continue
		}
		if cached, ok := p.cache.Get(p.model, t); ok {
			results[i] = cached
		} else {
			uncachedIndices = append(uncachedIndices, i)
			uncachedTexts = append(uncachedTexts, t)
		}
	}

	if len(uncachedTexts) == 0 {
		return results, nil
	}

	for start := 0; start < len(uncachedTexts); start += p.maxBatch {
		end := start + p.maxBatch
		if end > len(uncachedTexts) {
			end = len(uncachedTexts)
		}
		batch := uncachedTexts[start:end]

		embeddings, err := p.callAPI(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("batch [%d:%d]: %w", start, end, err)
		}

		for j, emb := range embeddings {
			idx := uncachedIndices[start+j]
			results[idx] = emb
			p.cache.Put(p.model, normalized[idx], emb)
		}
	}

	return results, nil
}

func (p *OpenAIProvider) Dimensions() int { return p.dims }
func (p *OpenAIProvider) ModelID() string  { return p.model }

func (p *OpenAIProvider) CacheStats() (hits, misses int64, size int) {
	return p.cache.Stats()
}

func (p *OpenAIProvider) callAPI(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := openAIRequest{
		Input: texts,
		Model: p.model,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(float64(initialBackoff) * math.Pow(2, float64(attempt-1)))
			p.logger.Warn("retrying OpenAI embedding request",
				"attempt", attempt,
				"backoff", backoff,
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		result, err := p.doRequest(ctx, body)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("openai: exhausted %d retries: %w", maxRetries, lastErr)
}

func (p *OpenAIProvider) doRequest(ctx context.Context, body []byte) ([][]float32, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.embeddingsURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, &RetryableError{Err: fmt.Errorf("http: %w", err)}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &RetryableError{Err: fmt.Errorf("read response: %w", err)}
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, &RetryableError{
			Err: fmt.Errorf("openai: status %d: %s", resp.StatusCode, string(respBody)),
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result openAIResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("openai: %s (%s)", result.Error.Message, result.Error.Type)
	}

	embeddings := make([][]float32, len(result.Data))
	for _, d := range result.Data {
		if d.Index >= len(embeddings) {
			return nil, fmt.Errorf("openai: invalid index %d in response", d.Index)
		}
		embeddings[d.Index] = d.Embedding
	}

	p.logger.Debug("embedding request completed",
		"count", len(result.Data),
		"tokens", result.Usage.TotalTokens,
	)

	return embeddings, nil
}

type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string { return e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }

func isRetryable(err error) bool {
	_, ok := err.(*RetryableError)
	return ok
}

func normalizeText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}
