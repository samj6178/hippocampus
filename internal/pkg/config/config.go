package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Config struct {
	Server   ServerConfig   `json:"server"`
	Database DatabaseConfig `json:"database"`
	OpenAI   OpenAIConfig   `json:"openai"`
	LLM      LLMConfig      `json:"llm"`
	Memory   MemoryConfig   `json:"memory"`
	Research ResearchConfig `json:"research"`
	Log      LogConfig      `json:"log"`
}

type ServerConfig struct {
	MCPPort  int `json:"mcp_port"`
	GRPCPort int `json:"grpc_port"`
	RESTPort int `json:"rest_port"`
}

type DatabaseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DBName   string `json:"db_name"`
	User     string `json:"user"`
	Password string `json:"password"`
	MaxConns int    `json:"max_conns"`
	RawDSN   string `json:"-"` // set via DATABASE_URL env, bypasses field-based DSN
}

func (d *DatabaseConfig) DSN() string {
	if d.RawDSN != "" {
		return d.RawDSN
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		d.User, d.Password, d.Host, d.Port, d.DBName)
}

type OpenAIConfig struct {
	APIKey     string `json:"api_key"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
	MaxBatch   int    `json:"max_batch"`
}

type MemoryConfig struct {
	WorkingMemoryCapacity int     `json:"working_memory_capacity"`
	DefaultTokenBudget    int     `json:"default_token_budget"`
	ConsolidationIntervalStr string  `json:"consolidation_interval"`
	ConsolidationInterval time.Duration `json:"-"`
	DecayHalfLifeDays     float64 `json:"decay_half_life_days"`
	GateThreshold         float64 `json:"gate_threshold"`
	EmbeddingCacheSize    int     `json:"embedding_cache_size"`
}

type LLMConfig struct {
	Provider      string `json:"provider"`       // "openai-compat"
	BaseURL       string `json:"base_url"`       // default: "http://localhost:11434/v1" (Ollama)
	APIKey        string `json:"api_key"`        // empty for Ollama
	Model         string `json:"model"`          // default: "qwen2.5:7b"
	MaxRPM        int    `json:"max_rpm"`        // rate limit, default 60
	MaxConcurrent int    `json:"max_concurrent"` // max parallel LLM calls, default 2
}

type ResearchConfig struct {
	SchedulerInterval   string `json:"scheduler_interval"`    // default "4h"
	MaxConcurrentAgents int    `json:"max_concurrent_agents"` // default 3
	RerankEnabled       bool   `json:"rerank_enabled"`        // default true
	RerankTopK          int    `json:"rerank_top_k"`          // default 10
}

type LogConfig struct {
	Level  string `json:"level"`
	Format string `json:"format"` // "json" or "text"
}

func Load(path string) (*Config, error) {
	cfg := defaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("read config %s: %w", path, err)
			}
		} else {
			if err := json.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parse config: %w", err)
			}
		}
	}

	overrideFromEnv(cfg)

	if cfg.Memory.ConsolidationIntervalStr != "" {
		d, err := time.ParseDuration(cfg.Memory.ConsolidationIntervalStr)
		if err != nil {
			return nil, fmt.Errorf("parse consolidation_interval %q: %w", cfg.Memory.ConsolidationIntervalStr, err)
		}
		cfg.Memory.ConsolidationInterval = d
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	var errs []string

	// Database
	if c.Database.Host == "" {
		errs = append(errs, "database.host is required")
	}
	if c.Database.Port < 1 || c.Database.Port > 65535 {
		errs = append(errs, fmt.Sprintf("database.port invalid: %d", c.Database.Port))
	}
	if c.Database.DBName == "" {
		errs = append(errs, "database.db_name is required")
	}
	if c.Database.MaxConns < 1 {
		errs = append(errs, fmt.Sprintf("database.max_conns must be >= 1, got %d", c.Database.MaxConns))
	}

	// Server ports
	for _, p := range []struct {
		name string
		val  int
	}{
		{"server.mcp_port", c.Server.MCPPort},
		{"server.rest_port", c.Server.RESTPort},
		{"server.grpc_port", c.Server.GRPCPort},
	} {
		if p.val < 1 || p.val > 65535 {
			errs = append(errs, fmt.Sprintf("%s invalid: %d", p.name, p.val))
		}
	}

	// Embedding
	if c.OpenAI.BaseURL == "" {
		errs = append(errs, "openai.base_url is required")
	}
	if c.OpenAI.Model == "" {
		errs = append(errs, "openai.model is required")
	}
	if c.OpenAI.Dimensions < 1 {
		errs = append(errs, fmt.Sprintf("openai.dimensions must be >= 1, got %d", c.OpenAI.Dimensions))
	}
	if c.OpenAI.MaxBatch < 1 {
		errs = append(errs, fmt.Sprintf("openai.max_batch must be >= 1, got %d", c.OpenAI.MaxBatch))
	}

	// LLM
	if c.LLM.BaseURL == "" {
		errs = append(errs, "llm.base_url is required")
	}
	if c.LLM.Model == "" {
		errs = append(errs, "llm.model is required")
	}
	if c.LLM.MaxRPM < 1 {
		errs = append(errs, fmt.Sprintf("llm.max_rpm must be >= 1, got %d", c.LLM.MaxRPM))
	}

	// Memory
	if c.Memory.DecayHalfLifeDays <= 0 {
		errs = append(errs, fmt.Sprintf("memory.decay_half_life_days must be > 0, got %f", c.Memory.DecayHalfLifeDays))
	}
	if c.Memory.WorkingMemoryCapacity < 1 {
		errs = append(errs, fmt.Sprintf("memory.working_memory_capacity must be >= 1, got %d", c.Memory.WorkingMemoryCapacity))
	}
	if c.Memory.GateThreshold < 0 || c.Memory.GateThreshold > 1 {
		errs = append(errs, fmt.Sprintf("memory.gate_threshold must be in [0, 1], got %f", c.Memory.GateThreshold))
	}
	if c.Memory.EmbeddingCacheSize < 0 {
		errs = append(errs, fmt.Sprintf("memory.embedding_cache_size must be >= 0, got %d", c.Memory.EmbeddingCacheSize))
	}
	if c.Memory.DefaultTokenBudget < 1 {
		errs = append(errs, fmt.Sprintf("memory.default_token_budget must be >= 1, got %d", c.Memory.DefaultTokenBudget))
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", joinErrors(errs))
	}
	return nil
}

func joinErrors(errs []string) string {
	if len(errs) == 1 {
		return errs[0]
	}
	result := fmt.Sprintf("%d validation errors:", len(errs))
	for _, e := range errs {
		result += "\n  - " + e
	}
	return result
}

func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			MCPPort:  3000,
			GRPCPort: 9090,
			RESTPort: 8080,
		},
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			DBName:   "hippocampus",
			User:     "mos",
			Password: "mos",
			MaxConns: 25,
		},
		OpenAI: OpenAIConfig{
			BaseURL:    "http://localhost:11434/v1",
			Model:      "nomic-embed-text",
			Dimensions: 768,
			MaxBatch:   32,
		},
		Memory: MemoryConfig{
			WorkingMemoryCapacity:    50,
			DefaultTokenBudget:       4096,
			ConsolidationIntervalStr: "6h",
			ConsolidationInterval:    6 * time.Hour,
			DecayHalfLifeDays:        7.0,
			GateThreshold:            0.3,
			EmbeddingCacheSize:       10000,
		},
		LLM: LLMConfig{
			Provider:      "openai-compat",
			BaseURL:       "http://localhost:11434/v1",
			Model:         "qwen2.5:7b",
			MaxRPM:        60,
			MaxConcurrent: 2,
		},
		Research: ResearchConfig{
			SchedulerInterval:   "4h",
			MaxConcurrentAgents: 3,
			RerankEnabled:       true,
			RerankTopK:          10,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

func overrideFromEnv(cfg *Config) {
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.OpenAI.APIKey = v
	}
	if v := os.Getenv("EMBEDDING_BASE_URL"); v != "" {
		cfg.OpenAI.BaseURL = v
	}
	if v := os.Getenv("EMBEDDING_MODEL"); v != "" {
		cfg.OpenAI.Model = v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		cfg.Database.RawDSN = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		cfg.Database.Password = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("LLM_BASE_URL"); v != "" {
		cfg.LLM.BaseURL = v
	}
	if v := os.Getenv("LLM_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}
	if v := os.Getenv("LLM_MODEL"); v != "" {
		cfg.LLM.Model = v
	}
}
