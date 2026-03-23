package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hippocampus-mcp/hippocampus/internal/app"
)

func TestNewlineDelimited_SingleMessage(t *testing.T) {
	msg := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"
	scanner := bufio.NewScanner(strings.NewReader(msg))

	if !scanner.Scan() {
		t.Fatal("expected a line")
	}
	line := strings.TrimSpace(scanner.Text())
	var req jsonRPCRequest
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if req.Method != "initialize" {
		t.Errorf("expected method initialize, got %s", req.Method)
	}
}

func TestNewlineDelimited_MultipleMessages(t *testing.T) {
	msgs := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n"

	scanner := bufio.NewScanner(strings.NewReader(msgs))
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var methods []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		methods = append(methods, req.Method)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}
	if len(methods) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(methods))
	}
	if methods[0] != "initialize" || methods[1] != "tools/list" {
		t.Errorf("unexpected methods: %v", methods)
	}
}

func TestNewlineDelimited_SkipEmptyLines(t *testing.T) {
	msgs := "\n\n" + `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n\n"
	scanner := bufio.NewScanner(strings.NewReader(msgs))

	var count int
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 message after skipping blanks, got %d", count)
	}
}

func TestSend_NewlineDelimited(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{writer: &buf}

	s.send(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      float64(1),
		Result:  map[string]string{"status": "ok"},
	})

	output := buf.String()

	if !strings.HasSuffix(output, "\n") {
		t.Fatalf("response must end with newline, got: %q", output)
	}

	if strings.Count(output, "\n") != 1 {
		t.Fatalf("response must be a single line, got %d newlines in: %q", strings.Count(output, "\n"), output)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &resp); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}

	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}
}

func TestToolsDefinitions(t *testing.T) {
	allTools := tools()

	if len(allTools) < 20 {
		t.Errorf("expected at least 20 tools, got %d", len(allTools))
	}

	names := make(map[string]bool)
	for _, tool := range allTools {
		name, ok := tool["name"].(string)
		if !ok || name == "" {
			t.Error("tool missing name")
			continue
		}
		if names[name] {
			t.Errorf("duplicate tool name: %s", name)
		}
		names[name] = true

		if _, ok := tool["description"]; !ok {
			t.Errorf("tool %s missing description", name)
		}
		if _, ok := tool["inputSchema"]; !ok {
			t.Errorf("tool %s missing inputSchema", name)
		}
	}

	required := []string{
		"mos_init", "mos_remember", "mos_recall", "mos_learn_error",
		"mos_analogize", "mos_meta", "mos_track_outcome",
		"mos_study_project", "mos_benchmark", "mos_research",
		"mos_predict", "mos_resolve", "mos_file_context",
	}
	for _, name := range required {
		if !names[name] {
			t.Errorf("required tool %s not found", name)
		}
	}
}

func TestSend_UnicodeContent(t *testing.T) {
	var buf bytes.Buffer
	s := &Server{writer: &buf}

	s.send(jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      float64(1),
		Result:  map[string]string{"text": "Привет мир 🧠"},
	})

	output := buf.String()
	line := strings.TrimSpace(output)

	var resp jsonRPCResponse
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("failed to unmarshal unicode response: %v", err)
	}
}

func TestExtractFilePaths(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"go error", "error in /internal/app/service.go: undefined", 1},
		{"windows path", `error at D:\go\project\main.go:45`, 1},
		{"stack trace", "panic at internal/repo/user_repo.go:123\ncalled from internal/app/handler.go:45", 2},
		{"no paths", "something went wrong with the connection", 0},
		{"python", "File internal/scripts/process.py, line 12", 1},
		{"multiple same", "error in ./file.go and ./file.go again", 1}, // dedup
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFilePaths(tt.input)
			if len(got) != tt.want {
				t.Errorf("extractFilePaths(%q) returned %d paths %v, want %d", tt.input, len(got), got, tt.want)
			}
		})
	}
}

func TestLooksLikeError(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"real error", "ERROR: connection refused\nROOT CAUSE: database not running", true},
		{"panic+failed", "panic: nil pointer dereference\nbuild failed", true},
		{"normal text", "I completed the task successfully and everything works", false},
		{"single indicator", "there was an error: something", false}, // only 1 indicator
		{"test failure", "test failed with error: expected 5 got 3", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeError(tt.text)
			if got != tt.want {
				t.Errorf("looksLikeError(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestFormatWarnings_Empty(t *testing.T) {
	result := formatWarnings(nil)
	if result != "" {
		t.Errorf("expected empty string for nil warnings, got %q", result)
	}
}

func TestFormatWarnings_SingleWarning(t *testing.T) {
	warnings := []app.MatchedWarning{
		{
			Rule: app.CachedRule{
				WhenText:  "using pgx connection pool",
				WatchText: "Acquire() without defer Release()",
				DoText:    "add defer conn.Release()",
				FilePaths: []string{"internal/repo/user_repo.go"},
			},
			Signal:     "file_match",
			Confidence: 1.0,
		},
	}

	result := formatWarnings(warnings)
	if !strings.Contains(result, "Known Issues") {
		t.Error("expected 'Known Issues' header")
	}
	if !strings.Contains(result, "file:internal/repo/user_repo.go") {
		t.Error("expected file path label")
	}
	if !strings.Contains(result, "WHEN: using pgx connection pool") {
		t.Error("expected WHEN clause")
	}
	if !strings.Contains(result, "WATCH: Acquire() without defer Release()") {
		t.Error("expected WATCH clause")
	}
	if !strings.Contains(result, "DO: add defer conn.Release()") {
		t.Error("expected DO clause")
	}
}

func TestFormatWarnings_FallbackContent(t *testing.T) {
	warnings := []app.MatchedWarning{
		{
			Rule: app.CachedRule{
				Content: "Always close connections\nMore details here",
			},
			Signal:     "embedding_match",
			Confidence: 0.6,
		},
	}

	result := formatWarnings(warnings)
	if !strings.Contains(result, "WHEN: Always close connections") {
		t.Errorf("expected first line of content as WHEN fallback, got:\n%s", result)
	}
}

func TestTrackExposure(t *testing.T) {
	s := &Server{}
	warnings := []app.MatchedWarning{
		{Signal: "file_match", Confidence: 1.0},
		{Signal: "keyword_match", Confidence: 0.7},
	}

	s.trackExposure(warnings)

	s.exposedMu.Lock()
	count := len(s.exposedWarnings)
	s.exposedMu.Unlock()

	if count != 2 {
		t.Errorf("expected 2 exposed warnings, got %d", count)
	}
}

func TestTrackExposure_Empty(t *testing.T) {
	s := &Server{}
	s.trackExposure(nil)

	s.exposedMu.Lock()
	count := len(s.exposedWarnings)
	s.exposedMu.Unlock()

	if count != 0 {
		t.Errorf("expected 0 exposed warnings for nil input, got %d", count)
	}
}
