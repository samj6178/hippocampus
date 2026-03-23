package llm

import (
	"context"
	"log/slog"
	"sync"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// SwitchableProvider wraps a domain.LLMProvider and allows runtime hot-swap
// without restarting the server. All services hold a reference to one
// SwitchableProvider; when the user changes LLM settings from the
// frontend, Switch() replaces the underlying provider instantly.
//
// Implements domain.LLMProvider.
type SwitchableProvider struct {
	mu      sync.RWMutex
	current domain.LLMProvider
	logger  *slog.Logger
}

func NewSwitchableProvider(initial domain.LLMProvider, logger *slog.Logger) *SwitchableProvider {
	return &SwitchableProvider{
		current: initial,
		logger:  logger,
	}
}

func (sp *SwitchableProvider) Chat(ctx context.Context, messages []domain.ChatMessage, opts domain.ChatOptions) (string, error) {
	sp.mu.RLock()
	p := sp.current
	sp.mu.RUnlock()
	return p.Chat(ctx, messages, opts)
}

func (sp *SwitchableProvider) Name() string {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.current.Name()
}

func (sp *SwitchableProvider) IsAvailable(ctx context.Context) bool {
	sp.mu.RLock()
	p := sp.current
	sp.mu.RUnlock()
	return p.IsAvailable(ctx)
}

// Switch replaces the underlying LLM provider at runtime.
// Thread-safe: all in-flight requests on the old provider will complete;
// new requests will use the new provider.
func (sp *SwitchableProvider) Switch(newProvider domain.LLMProvider) {
	sp.mu.Lock()
	old := sp.current
	sp.current = newProvider
	sp.mu.Unlock()
	sp.logger.Info("LLM provider switched", "from", old.Name(), "to", newProvider.Name())
}

// Current returns the active provider for inspection.
func (sp *SwitchableProvider) Current() domain.LLMProvider {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.current
}

// LLMStatus is returned by the REST API to show current provider state.
type LLMStatus struct {
	ProviderName string `json:"provider_name"`
	BaseURL      string `json:"base_url"`
	Model        string `json:"model"`
	APIKeySet    bool   `json:"api_key_set"`
	Available    bool   `json:"available"`
}

func (sp *SwitchableProvider) Status(ctx context.Context) LLMStatus {
	sp.mu.RLock()
	p := sp.current
	sp.mu.RUnlock()

	status := LLMStatus{
		ProviderName: p.Name(),
		Available:    p.IsAvailable(ctx),
	}

	if oai, ok := p.(*OpenAICompatProvider); ok {
		cfg := oai.Config()
		status.BaseURL = cfg.BaseURL
		status.Model = cfg.Model
		status.APIKeySet = cfg.APIKey != "" && cfg.APIKey != "ollama"
	}

	return status
}
