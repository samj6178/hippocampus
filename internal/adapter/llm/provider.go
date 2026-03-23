package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/metrics"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
)

// OpenAICompatProvider works with any OpenAI-compatible chat completions API:
// OpenAI, DeepSeek, Qwen, Ollama /v1/chat/completions, OpenRouter, Together, etc.
// Implements domain.LLMProvider.
type OpenAICompatProvider struct {
	baseURL  string
	apiKey   string
	model    string
	client   *http.Client
	limiter  *rate.Limiter
	sem      *semaphore.Weighted
	logger   *slog.Logger
}

type ProviderConfig struct {
	BaseURL       string `json:"base_url"`
	APIKey        string `json:"api_key"`
	Model         string `json:"model"`
	MaxRPM        int    `json:"max_rpm"`
	MaxConcurrent int    `json:"max_concurrent"` // max parallel LLM calls, default 2
}

func NewOpenAICompatProvider(cfg ProviderConfig, logger *slog.Logger) *OpenAICompatProvider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	model := cfg.Model
	if model == "" {
		model = "qwen2.5:7b"
	}
	maxRPM := cfg.MaxRPM
	if maxRPM <= 0 {
		maxRPM = 60
	}
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}

	return &OpenAICompatProvider{
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		model:   model,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		limiter: rate.NewLimiter(rate.Limit(float64(maxRPM)/60.0), maxRPM/6+1),
		sem:     semaphore.NewWeighted(int64(maxConcurrent)),
		logger:  logger,
	}
}

func (p *OpenAICompatProvider) Name() string {
	return fmt.Sprintf("openai-compat(%s, %s)", p.baseURL, p.model)
}

func (p *OpenAICompatProvider) IsAvailable(ctx context.Context) bool {
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := p.Chat(testCtx, []domain.ChatMessage{{Role: "user", Content: "ping"}}, domain.ChatOptions{MaxTokens: 5})
	return err == nil
}

func (p *OpenAICompatProvider) Chat(ctx context.Context, messages []domain.ChatMessage, opts domain.ChatOptions) (string, error) {
	if err := p.sem.Acquire(ctx, 1); err != nil {
		return "", fmt.Errorf("concurrency limit: %w", err)
	}
	defer p.sem.Release(1)

	if err := p.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limit: %w", err)
	}

	start := time.Now()
	defer func() {
		metrics.LLMCallLatency.WithLabelValues("chat").Observe(time.Since(start).Seconds())
	}()
	metrics.LLMCallsTotal.WithLabelValues("chat").Inc()

	model := opts.Model
	if model == "" {
		model = p.model
	}
	temp := opts.Temperature
	if temp <= 0 {
		temp = 0.3
	}
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1200
	}

	reqBody := chatRequest{
		Model:       model,
		Messages:    messages,
		Temperature: temp,
		MaxTokens:   maxTokens,
		Stream:      false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
		}

		result, err := p.doRequest(ctx, bodyBytes)
		if err == nil {
			return result, nil
		}
		lastErr = err

		if isRetryable(err) {
			p.logger.Warn("LLM request failed, retrying", "attempt", attempt+1, "error", err)
			continue
		}
		return "", err
	}
	metrics.LLMCallErrors.WithLabelValues("chat").Inc()
	return "", fmt.Errorf("all retries exhausted: %w", lastErr)
}

func (p *OpenAICompatProvider) doRequest(ctx context.Context, body []byte) (string, error) {
	endpoint := p.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" && p.apiKey != "ollama" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("decode response: %w (body: %s)", err, truncate(string(respBody), 200))
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("empty choices in response")
	}

	return strings.TrimSpace(chatResp.Choices[0].Message.Content), nil
}

func (p *OpenAICompatProvider) Config() ProviderConfig {
	return ProviderConfig{
		BaseURL: p.baseURL,
		APIKey:  p.apiKey,
		Model:   p.model,
	}
}

type chatRequest struct {
	Model       string               `json:"model"`
	Messages    []domain.ChatMessage `json:"messages"`
	Temperature float64              `json:"temperature"`
	MaxTokens   int                  `json:"max_tokens"`
	Stream      bool                 `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// APIError represents a non-200 response from the LLM API.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("LLM API error %d: %s", e.StatusCode, truncate(e.Body, 200))
}

func isRetryable(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == 429 || apiErr.StatusCode >= 500
	}
	return true
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
