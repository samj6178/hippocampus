package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"My Cool Project", "my-cool-project"},
		{"hello_world", "hello-world"},
		{"already-slug", "already-slug"},
		{"  spaces  ", "spaces"},
		{"CamelCase", "camelcase"},
		{"with@special#chars!", "with-specialchars"},
		{"---triple-dash---", "triple-dash"},
		{"", "unnamed"},
		{"   ", "unnamed"},
		{"github.com/user/repo", "github-com-user-repo"},
		{"a", "a"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := slugify(tt.input)
			if got != tt.expected {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractGoModule(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{"standard", "module github.com/user/repo\n\ngo 1.21", "github.com/user/repo"},
		{"with_version", "module github.com/user/repo/v2\n", "github.com/user/repo/v2"},
		{"empty", "", ""},
		{"no_module", "go 1.21\nrequire (\n)", ""},
		{"whitespace", "  module  github.com/user/repo  \n", "github.com/user/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGoModule(tt.content)
			if got != tt.expected {
				t.Errorf("extractGoModule = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractTomlValue(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		key      string
		expected string
	}{
		{"quoted", `name = "my-project"`, "name", "my-project"},
		{"single_quoted", `name = 'my-project'`, "name", "my-project"},
		{"unquoted", `name = my-project`, "name", "my-project"},
		{"with_spaces", `name = "my-project"`, "name", "my-project"},
		{"missing_key", `version = "1.0"`, "name", ""},
		{"empty_content", "", "name", ""},
		{"multiline", "version = \"1.0\"\nname = \"found\"", "name", "found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTomlValue(tt.content, tt.key)
			if got != tt.expected {
				t.Errorf("extractTomlValue(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestDetectProject_GoProject(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/test/project\n\ngo 1.21\n"), 0644)

	result := DetectProject(dir)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Slug == "" {
		t.Error("slug should not be empty")
	}
	if result.Language != "go" {
		t.Errorf("expected language=go, got %q", result.Language)
	}
}

func TestDetectProject_NodeProject(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name": "my-node-app", "description": "A test app"}`), 0644)

	result := DetectProject(dir)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Language != "typescript" && result.Language != "javascript" {
		t.Errorf("expected language=typescript or javascript, got %q", result.Language)
	}
}

func TestDetectProject_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	result := DetectProject(dir)
	if result == nil {
		t.Fatal("expected non-nil result for empty dir")
	}
	if result.Language != "" {
		t.Errorf("expected empty language for unknown project, got %q", result.Language)
	}
}

func TestDetectProject_EmptyPath(t *testing.T) {
	result := DetectProject("")
	if result != nil {
		t.Error("expected nil for empty path")
	}
}
