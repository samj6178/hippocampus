package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectEnvFromClientName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected AgentEnvironment
	}{
		{"cursor_lower", "cursor", EnvCursor},
		{"cursor_cap", "Cursor", EnvCursor},
		{"claude_code", "claude-code", EnvClaudeCode},
		{"claude_code_caps", "Claude Code", EnvClaudeCode},
		{"claude_only", "claude", EnvClaudeCode},
		{"vscode", "vscode", EnvVSCode},
		{"vscode_caps", "Visual Studio Code", EnvVSCode},
		{"windsurf", "windsurf", EnvWindsurf},
		{"windsurf_caps", "Windsurf", EnvWindsurf},
		{"unknown", "emacs", EnvUnknown},
		{"empty", "", EnvUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectEnvFromClientName(tt.input)
			if got != tt.expected {
				t.Errorf("DetectEnvFromClientName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestDetectEnvironments(t *testing.T) {
	t.Run("empty_path", func(t *testing.T) {
		result := DetectEnvironments("")
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result.Environments) != 1 || result.Environments[0] != EnvUnknown {
			t.Errorf("expected [unknown], got %v", result.Environments)
		}
	})

	t.Run("cursor_env", func(t *testing.T) {
		dir := t.TempDir()
		os.Mkdir(filepath.Join(dir, ".cursor"), 0755)

		result := DetectEnvironments(dir)
		found := false
		for _, e := range result.Environments {
			if e == EnvCursor {
				found = true
			}
		}
		if !found {
			t.Errorf("expected EnvCursor in %v", result.Environments)
		}
	})

	t.Run("claude_env", func(t *testing.T) {
		dir := t.TempDir()
		os.Mkdir(filepath.Join(dir, ".claude"), 0755)

		result := DetectEnvironments(dir)
		found := false
		for _, e := range result.Environments {
			if e == EnvClaudeCode {
				found = true
			}
		}
		if !found {
			t.Errorf("expected EnvClaudeCode in %v", result.Environments)
		}
	})

	t.Run("claude_md_file", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Rules"), 0644)

		result := DetectEnvironments(dir)
		found := false
		for _, e := range result.Environments {
			if e == EnvClaudeCode {
				found = true
			}
		}
		if !found {
			t.Errorf("expected EnvClaudeCode for CLAUDE.md, got %v", result.Environments)
		}
	})

	t.Run("multiple_envs", func(t *testing.T) {
		dir := t.TempDir()
		os.Mkdir(filepath.Join(dir, ".cursor"), 0755)
		os.Mkdir(filepath.Join(dir, ".vscode"), 0755)

		result := DetectEnvironments(dir)
		if len(result.Environments) < 2 {
			t.Errorf("expected at least 2 envs, got %v", result.Environments)
		}
	})
}

func TestContainsEnv(t *testing.T) {
	envs := []AgentEnvironment{EnvCursor, EnvVSCode}
	if !containsEnv(envs, EnvCursor) {
		t.Error("should contain EnvCursor")
	}
	if containsEnv(envs, EnvClaudeCode) {
		t.Error("should not contain EnvClaudeCode")
	}
	if containsEnv(nil, EnvCursor) {
		t.Error("nil slice should not contain anything")
	}
}
