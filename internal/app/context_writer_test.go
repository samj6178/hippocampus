package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteWithMarkers_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if err := writeWithMarkers(path, "auto content"); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	content := string(got)

	if !strings.Contains(content, markerBegin) {
		t.Error("missing begin marker")
	}
	if !strings.Contains(content, markerEnd) {
		t.Error("missing end marker")
	}
	if !strings.Contains(content, "auto content") {
		t.Error("missing generated content")
	}
}

func TestWriteWithMarkers_PreservesHandWritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	initial := "# My Project Rules\n\n- Always use gofmt\n- No globals\n\n" +
		markerBegin + "\nold auto content\n" + markerEnd + "\n\n## Notes\n\nKeep this too.\n"

	os.WriteFile(path, []byte(initial), 0644)

	if err := writeWithMarkers(path, "NEW auto content"); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	content := string(got)

	if !strings.Contains(content, "# My Project Rules") {
		t.Error("lost header")
	}
	if !strings.Contains(content, "- Always use gofmt") {
		t.Error("lost hand-written rules")
	}
	if !strings.Contains(content, "## Notes") {
		t.Error("lost trailing section")
	}
	if !strings.Contains(content, "Keep this too.") {
		t.Error("lost trailing content")
	}
	if !strings.Contains(content, "NEW auto content") {
		t.Error("missing new auto content")
	}
	if strings.Contains(content, "old auto content") {
		t.Error("old auto content should be replaced")
	}
}

func TestWriteWithMarkers_MigratesOldFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	old := "# Hippocampus MOS — Learned Knowledge\n\n*Auto-generated: 2026-03-19*\n\nOld stuff\n"
	os.WriteFile(path, []byte(old), 0644)

	if err := writeWithMarkers(path, "migrated content"); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	content := string(got)

	if strings.Contains(content, "Old stuff") {
		t.Error("old format content should be replaced")
	}
	if !strings.Contains(content, markerBegin) {
		t.Error("missing markers after migration")
	}
	if !strings.Contains(content, "migrated content") {
		t.Error("missing migrated content")
	}
}

func TestWriteWithMarkers_AppendsToUserFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	userContent := "# My Custom CLAUDE.md\n\nThis is my project.\n"
	os.WriteFile(path, []byte(userContent), 0644)

	if err := writeWithMarkers(path, "appended auto"); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	content := string(got)

	if !strings.Contains(content, "# My Custom CLAUDE.md") {
		t.Error("lost user content")
	}
	if !strings.Contains(content, "This is my project.") {
		t.Error("lost user content body")
	}
	if !strings.Contains(content, markerBegin) {
		t.Error("missing markers")
	}
	if !strings.Contains(content, "appended auto") {
		t.Error("missing auto content")
	}

	// Verify markers come after user content
	userIdx := strings.Index(content, "This is my project.")
	markerIdx := strings.Index(content, markerBegin)
	if markerIdx < userIdx {
		t.Error("markers should be after user content")
	}
}

func TestDedupKey(t *testing.T) {
	tests := []struct {
		a, b string
		same bool
	}{
		{"ERROR: foo bar", "error: foo bar", true},
		{"ERROR: foo bar", "ERROR: foo bar  ", true},
		{"ERROR: foo bar baz", "ERROR: completely different", false},
		{"short", "short", true},
		{strings.Repeat("x", 200), strings.Repeat("x", 200) + " extra", true},
	}
	for _, tt := range tests {
		ka, kb := dedupKey(tt.a), dedupKey(tt.b)
		if (ka == kb) != tt.same {
			t.Errorf("dedupKey(%q) == dedupKey(%q): got %v, want %v", tt.a, tt.b, ka == kb, tt.same)
		}
	}
}

func TestWriteWithMarkers_UpdateExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// First write
	if err := writeWithMarkers(path, "v1 content"); err != nil {
		t.Fatal(err)
	}
	// Second write — should replace, not duplicate
	if err := writeWithMarkers(path, "v2 content"); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	content := string(got)

	if strings.Contains(content, "v1 content") {
		t.Error("v1 should be replaced")
	}
	if !strings.Contains(content, "v2 content") {
		t.Error("v2 should be present")
	}
	if strings.Count(content, markerBegin) != 1 {
		t.Errorf("expected 1 begin marker, got %d", strings.Count(content, markerBegin))
	}
	if strings.Count(content, markerEnd) != 1 {
		t.Errorf("expected 1 end marker, got %d", strings.Count(content, markerEnd))
	}
}
