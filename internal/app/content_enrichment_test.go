package app

import (
	"context"
	"strings"
	"testing"
)

func TestExtractSummary_ShortContent(t *testing.T) {
	content := "BUG: X is broken"
	got := extractSummary(content)
	if got != content {
		t.Errorf("short content should be returned as-is, got %q", got)
	}
}

func TestExtractSummary_LongContent(t *testing.T) {
	content := "ERROR: The database connection pool exhausts all connections under load. " +
		"This happens because each goroutine opens a new connection without checking the pool limit. " +
		"ROOT CAUSE: Missing maxConns configuration in pgxpool.Config. " +
		"FIX: Set MaxConns to 20 in the pool configuration. " +
		"PREVENTION: Always validate pool settings in config validation. " +
		"This was discovered during load testing with 100 concurrent requests."

	got := extractSummary(content)
	if len(got) > maxSummaryLen+10 {
		t.Errorf("summary too long: %d chars (max %d)", len(got), maxSummaryLen)
	}
	if len(got) == 0 {
		t.Error("summary should not be empty")
	}
}

func TestExtractSummary_PreservesFirstSentence(t *testing.T) {
	content := "Critical bug in authentication middleware. " +
		"Users can bypass login by sending empty tokens. " +
		"The middleware checks token length but not validity. " +
		"Fixed by adding proper JWT validation. " +
		"Deployed to production successfully."

	got := extractSummary(content)
	if !strings.Contains(got, "Critical bug") {
		t.Errorf("summary should contain first sentence (highest score), got %q", got)
	}
}

func TestExtractSummary_Empty(t *testing.T) {
	if got := extractSummary(""); got != "" {
		t.Errorf("empty content should return empty summary, got %q", got)
	}
}

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		minWant int
	}{
		{"single", "Hello world", 1},
		{"period_split", "First sentence. Second sentence.", 2},
		{"newline_split", "First part\n\nSecond part", 2},
		{"mixed", "Error found. Fix applied.\n\nDeployed.", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitSentences(tt.input)
			if len(got) < tt.minWant {
				t.Errorf("splitSentences(%q) = %d sentences, want >= %d: %v", tt.input, len(got), tt.minWant, got)
			}
		})
	}
}

func TestTruncateAt(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 100, "short"},
		{"hello world foo bar", 12, "hello..."},
	}
	for _, tt := range tests {
		got := truncateAt(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateAt(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
		if len(got) > tt.maxLen {
			t.Errorf("result too long: %d > %d", len(got), tt.maxLen)
		}
	}
}

func TestContentQualityScore(t *testing.T) {
	tests := []struct {
		name    string
		content string
		minWant float64
		maxWant float64
	}{
		{"empty", "", 0.0, 0.01},
		{"too_short", "hi", 0.0, 0.01},
		{"junk", "a b c", 0.0, 0.2},
		{"stop_words_only", "the a an is are was were in on at to for of", 0.0, 0.05},
		{"good_technical", "BUG: database connection pool exhausts MaxConns under goroutine load", 0.5, 1.0},
		{"verbose_noise", "I think that maybe we should probably consider possibly looking into it", 0.3, 0.7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contentQualityScore(tt.content)
			if got < tt.minWant || got > tt.maxWant {
				t.Errorf("contentQualityScore(%q) = %f, want [%f, %f]", tt.content, got, tt.minWant, tt.maxWant)
			}
		})
	}
}

func TestExtractAutoTags(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			"go_code",
			"Fixed bug in handler.go where goroutine leaks memory",
			[]string{"go"},
		},
		{
			"error_pattern",
			"ERROR: panic in production caused data loss",
			[]string{"error"},
		},
		{
			"decision_architecture",
			"Decided to refactor the architecture using clean patterns",
			[]string{"decision", "architecture"},
		},
		{
			"postgres",
			"Added pgx pool configuration for postgres connection",
			[]string{"postgresql"},
		},
		{
			"bugfix",
			"Fixed the root cause of the authentication bug",
			[]string{"bugfix", "security"},
		},
		{
			"empty",
			"hello world",
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAutoTags(tt.content)
			for _, want := range tt.want {
				found := false
				for _, g := range got {
					if g == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("extractAutoTags(%q) missing tag %q, got %v", tt.content, want, got)
				}
			}
		})
	}
}

func TestMergeTagSets(t *testing.T) {
	got := mergeTagSets([]string{"go", "error"}, []string{"error", "bugfix", "go"})
	if len(got) != 3 {
		t.Errorf("expected 3 unique tags, got %d: %v", len(got), got)
	}
	if got[0] != "go" || got[1] != "error" || got[2] != "bugfix" {
		t.Errorf("unexpected order: %v", got)
	}
}

func TestMergeTagSets_NilInputs(t *testing.T) {
	got := mergeTagSets(nil, []string{"auto"})
	if len(got) != 1 || got[0] != "auto" {
		t.Errorf("expected [auto], got %v", got)
	}

	got2 := mergeTagSets([]string{"manual"}, nil)
	if len(got2) != 1 || got2[0] != "manual" {
		t.Errorf("expected [manual], got %v", got2)
	}
}

func TestScoreSentence_PositionBonus(t *testing.T) {
	s := "Some regular sentence without keywords"
	first := scoreSentence(s, 0, 5)
	middle := scoreSentence(s, 2, 5)
	last := scoreSentence(s, 4, 5)

	if first <= middle {
		t.Errorf("first sentence (%f) should score higher than middle (%f)", first, middle)
	}
	if last <= middle {
		t.Errorf("last sentence (%f) should score higher than middle (%f)", last, middle)
	}
}

func TestScoreSentence_TechnicalBonus(t *testing.T) {
	plain := scoreSentence("The cat sat on the mat quietly", 1, 5)
	tech := scoreSentence("Fixed critical error in handler.go function", 1, 5)

	if tech <= plain {
		t.Errorf("technical sentence (%f) should score higher than plain (%f)", tech, plain)
	}
}

func TestIsStopWord(t *testing.T) {
	if !isStopWord("the") {
		t.Error("'the' should be a stop word")
	}
	if isStopWord("database") {
		t.Error("'database' should not be a stop word")
	}
}

func TestEncode_Integration_SummaryAndTags(t *testing.T) {
	ep := &encodeEpisodicRepo{}
	svc := newTestEncodeService(
		ep, nil,
		&mockEmbedding{embeddings: [][]float32{{0.1, 0.2}}},
		0.3,
	)

	resp, err := svc.Encode(testCtx(), &EncodeRequest{
		Content:    "ERROR: goroutine leak in handler.go causes memory exhaustion under load. Fixed by adding context cancellation to all spawned goroutines. The root cause was missing defer cancel() in the request handler.",
		Importance: 0.8,
		Tags:       []string{"manual-tag"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Encoded {
		t.Fatal("expected Encoded=true")
	}

	mem := ep.inserted[0]
	if mem.Summary == "" {
		t.Error("Summary should be populated")
	}
	if len(mem.Summary) > maxSummaryLen+10 {
		t.Errorf("Summary too long: %d", len(mem.Summary))
	}

	hasManual := false
	hasAuto := false
	for _, tag := range mem.Tags {
		if tag == "manual-tag" {
			hasManual = true
		}
		if tag == "go" || tag == "error" || tag == "bugfix" {
			hasAuto = true
		}
	}
	if !hasManual {
		t.Error("manual tag should be preserved")
	}
	if !hasAuto {
		t.Errorf("auto tags should be added, got %v", mem.Tags)
	}
}

func testCtx() context.Context {
	return context.Background()
}
