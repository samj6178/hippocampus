package config

import (
	"strings"
	"testing"
)

func validConfig() *Config {
	return defaultConfig()
}

func TestValidate_DefaultConfig(t *testing.T) {
	cfg := validConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid, got: %v", err)
	}
}

func TestValidate_Database(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		errMsg string
	}{
		{"empty_host", func(c *Config) { c.Database.Host = "" }, "database.host"},
		{"zero_port", func(c *Config) { c.Database.Port = 0 }, "database.port"},
		{"port_too_high", func(c *Config) { c.Database.Port = 70000 }, "database.port"},
		{"empty_dbname", func(c *Config) { c.Database.DBName = "" }, "database.db_name"},
		{"zero_maxconns", func(c *Config) { c.Database.MaxConns = 0 }, "database.max_conns"},
		{"negative_maxconns", func(c *Config) { c.Database.MaxConns = -1 }, "database.max_conns"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidate_ServerPorts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		errMsg string
	}{
		{"mcp_port_zero", func(c *Config) { c.Server.MCPPort = 0 }, "server.mcp_port"},
		{"rest_port_negative", func(c *Config) { c.Server.RESTPort = -1 }, "server.rest_port"},
		{"grpc_port_too_high", func(c *Config) { c.Server.GRPCPort = 100000 }, "server.grpc_port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidate_Embedding(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		errMsg string
	}{
		{"empty_base_url", func(c *Config) { c.OpenAI.BaseURL = "" }, "openai.base_url"},
		{"empty_model", func(c *Config) { c.OpenAI.Model = "" }, "openai.model"},
		{"zero_dimensions", func(c *Config) { c.OpenAI.Dimensions = 0 }, "openai.dimensions"},
		{"zero_max_batch", func(c *Config) { c.OpenAI.MaxBatch = 0 }, "openai.max_batch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidate_LLM(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		errMsg string
	}{
		{"empty_base_url", func(c *Config) { c.LLM.BaseURL = "" }, "llm.base_url"},
		{"empty_model", func(c *Config) { c.LLM.Model = "" }, "llm.model"},
		{"zero_max_rpm", func(c *Config) { c.LLM.MaxRPM = 0 }, "llm.max_rpm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidate_Memory(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		errMsg string
	}{
		{"zero_decay", func(c *Config) { c.Memory.DecayHalfLifeDays = 0 }, "decay_half_life_days"},
		{"negative_decay", func(c *Config) { c.Memory.DecayHalfLifeDays = -1 }, "decay_half_life_days"},
		{"zero_capacity", func(c *Config) { c.Memory.WorkingMemoryCapacity = 0 }, "working_memory_capacity"},
		{"gate_below_zero", func(c *Config) { c.Memory.GateThreshold = -0.1 }, "gate_threshold"},
		{"gate_above_one", func(c *Config) { c.Memory.GateThreshold = 1.5 }, "gate_threshold"},
		{"negative_cache", func(c *Config) { c.Memory.EmbeddingCacheSize = -1 }, "embedding_cache_size"},
		{"zero_budget", func(c *Config) { c.Memory.DefaultTokenBudget = 0 }, "default_token_budget"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := validConfig()
	cfg.Database.Host = ""
	cfg.Database.DBName = ""
	cfg.OpenAI.Model = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "3 validation errors") {
		t.Errorf("expected 3 errors, got: %v", err)
	}
}

func TestValidate_EdgeCases(t *testing.T) {
	t.Run("gate_threshold_zero_valid", func(t *testing.T) {
		cfg := validConfig()
		cfg.Memory.GateThreshold = 0
		if err := cfg.Validate(); err != nil {
			t.Errorf("gate_threshold=0 should be valid: %v", err)
		}
	})

	t.Run("gate_threshold_one_valid", func(t *testing.T) {
		cfg := validConfig()
		cfg.Memory.GateThreshold = 1.0
		if err := cfg.Validate(); err != nil {
			t.Errorf("gate_threshold=1.0 should be valid: %v", err)
		}
	})

	t.Run("cache_size_zero_valid", func(t *testing.T) {
		cfg := validConfig()
		cfg.Memory.EmbeddingCacheSize = 0
		if err := cfg.Validate(); err != nil {
			t.Errorf("cache_size=0 should be valid (disabled): %v", err)
		}
	})

	t.Run("port_boundaries_valid", func(t *testing.T) {
		cfg := validConfig()
		cfg.Server.MCPPort = 1
		cfg.Server.RESTPort = 65535
		if err := cfg.Validate(); err != nil {
			t.Errorf("boundary ports should be valid: %v", err)
		}
	})
}

func TestDSN(t *testing.T) {
	cfg := DatabaseConfig{
		Host: "localhost", Port: 5432, DBName: "testdb",
		User: "admin", Password: "secret",
	}
	dsn := cfg.DSN()
	expected := "postgres://admin:secret@localhost:5432/testdb?sslmode=disable"
	if dsn != expected {
		t.Errorf("DSN() = %q, want %q", dsn, expected)
	}
}

func TestDSN_RawOverride(t *testing.T) {
	cfg := DatabaseConfig{
		Host: "localhost", Port: 5432, DBName: "testdb",
		User: "admin", Password: "secret",
		RawDSN: "postgres://prod:prodpass@db.example.com:5432/hippocampus?sslmode=require",
	}
	dsn := cfg.DSN()
	if dsn != cfg.RawDSN {
		t.Errorf("DSN() with RawDSN should return RawDSN, got %q", dsn)
	}
}

func TestDSN_RawEmptyFallsBack(t *testing.T) {
	cfg := DatabaseConfig{
		Host: "localhost", Port: 5432, DBName: "testdb",
		User: "admin", Password: "secret",
		RawDSN: "",
	}
	dsn := cfg.DSN()
	if dsn != "postgres://admin:secret@localhost:5432/testdb?sslmode=disable" {
		t.Errorf("empty RawDSN should fall back to field-based DSN, got %q", dsn)
	}
}
