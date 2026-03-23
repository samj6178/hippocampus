package app

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
)

func TestWarningMatcher_FileMatch(t *testing.T) {
	wm := NewWarningMatcher(nil, nil, slog.Default())
	wm.cache = []CachedRule{
		{ID: uuid.New(), Content: "rule1", FilePaths: []string{"internal/repo/user_repo.go"}},
		{ID: uuid.New(), Content: "rule2", FilePaths: []string{"internal/app/service.go"}},
	}

	matches := wm.Match(context.Background(), MatchSignals{
		FilePath: "internal/repo/user_repo.go",
	})

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Signal != "file_match" {
		t.Errorf("expected file_match signal, got %s", matches[0].Signal)
	}
	if matches[0].Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", matches[0].Confidence)
	}
}

func TestWarningMatcher_FileMatchSuffix(t *testing.T) {
	wm := NewWarningMatcher(nil, nil, slog.Default())
	wm.cache = []CachedRule{
		{ID: uuid.New(), Content: "rule1", FilePaths: []string{"repo/user_repo.go"}},
	}

	matches := wm.Match(context.Background(), MatchSignals{
		FilePath: "D:\\go\\project\\internal\\repo\\user_repo.go",
	})

	if len(matches) != 1 {
		t.Fatalf("expected 1 match via suffix, got %d", len(matches))
	}
}

func TestWarningMatcher_KeywordMatch(t *testing.T) {
	wm := NewWarningMatcher(nil, nil, slog.Default())
	wm.cache = []CachedRule{
		{ID: uuid.New(), Content: "rule1", WhenKeywords: []string{"pgx", "connection", "pool"}},
		{ID: uuid.New(), Content: "rule2", WhenKeywords: []string{"redis", "cache", "timeout"}},
	}

	matches := wm.Match(context.Background(), MatchSignals{
		Query: "pgx connection pool acquire release",
	})

	if len(matches) != 1 {
		t.Fatalf("expected 1 keyword match, got %d", len(matches))
	}
	if matches[0].Signal != "keyword_match" {
		t.Errorf("expected keyword_match signal, got %s", matches[0].Signal)
	}
	if matches[0].Confidence < 0.5 {
		t.Errorf("expected confidence >= 0.5, got %f", matches[0].Confidence)
	}
}

func TestWarningMatcher_EmbeddingFallback(t *testing.T) {
	wm := NewWarningMatcher(nil, nil, slog.Default())
	// Create embeddings that will have high cosine similarity
	emb := make([]float32, 10)
	for i := range emb {
		emb[i] = 0.5
	}
	wm.cache = []CachedRule{
		{ID: uuid.New(), Content: "rule1", Embedding: emb},
	}

	matches := wm.Match(context.Background(), MatchSignals{
		QueryEmb: emb, // identical = sim 1.0
	})

	if len(matches) != 1 {
		t.Fatalf("expected 1 embedding match, got %d", len(matches))
	}
	if matches[0].Signal != "embedding_match" {
		t.Errorf("expected embedding_match signal, got %s", matches[0].Signal)
	}
}

func TestWarningMatcher_Dedup(t *testing.T) {
	wm := NewWarningMatcher(nil, nil, slog.Default())
	id := uuid.New()
	emb := make([]float32, 10)
	for i := range emb {
		emb[i] = 0.5
	}
	wm.cache = []CachedRule{
		{
			ID:           id,
			Content:      "rule1",
			FilePaths:    []string{"internal/repo/user_repo.go"},
			WhenKeywords: []string{"pgx", "pool"},
			Embedding:    emb,
		},
	}

	// All 3 levels should match, but result should be deduped to 1
	matches := wm.Match(context.Background(), MatchSignals{
		FilePath: "internal/repo/user_repo.go",
		Query:    "pgx pool connection",
		QueryEmb: emb,
	})

	if len(matches) != 1 {
		t.Fatalf("expected 1 deduped match, got %d", len(matches))
	}
	if matches[0].Signal != "file_match" {
		t.Errorf("file_match should win (highest confidence), got %s", matches[0].Signal)
	}
}

func TestWarningMatcher_MaxResults(t *testing.T) {
	wm := NewWarningMatcher(nil, nil, slog.Default())
	rules := make([]CachedRule, 10)
	for i := range rules {
		rules[i] = CachedRule{
			ID:           uuid.New(),
			Content:      "rule",
			WhenKeywords: []string{"common", "keyword"},
		}
	}
	wm.cache = rules

	matches := wm.Match(context.Background(), MatchSignals{
		Query: "common keyword match test",
	})

	if len(matches) > 3 {
		t.Errorf("expected max 3 results, got %d", len(matches))
	}
}

func TestWarningMatcher_FileMatchPathBoundary(t *testing.T) {
	wm := NewWarningMatcher(nil, nil, slog.Default())
	wm.cache = []CachedRule{
		{ID: uuid.New(), Content: "rule1", FilePaths: []string{"user_repo.go"}},
	}

	// "test_user_repo.go" ends with "user_repo.go" but the preceding char is '_', not '/'
	// pathSuffixMatch should reject this
	noMatch := wm.Match(context.Background(), MatchSignals{
		FilePath: "internal/repo/test_user_repo.go",
	})
	if len(noMatch) != 0 {
		t.Errorf("expected no match for test_user_repo.go, got %d matches", len(noMatch))
	}

	// "user_repo.go" is preceded by '/' — boundary match must succeed
	yesMatch := wm.Match(context.Background(), MatchSignals{
		FilePath: "internal/repo/user_repo.go",
	})
	if len(yesMatch) != 1 {
		t.Fatalf("expected 1 match for user_repo.go, got %d", len(yesMatch))
	}
	if yesMatch[0].Signal != "file_match" {
		t.Errorf("expected file_match signal, got %s", yesMatch[0].Signal)
	}
}

func TestWarningMatcher_EmbeddingThresholdRaised(t *testing.T) {
	wm := NewWarningMatcher(nil, nil, slog.Default())
	// Rule embedding: unit vector along first axis — cosine(query, rule) = query[0]
	ruleEmb := []float32{1, 0, 0, 0}
	wm.cache = []CachedRule{
		{ID: uuid.New(), Content: "rule1", Embedding: ruleEmb},
	}

	// cosine([0.6, 0.8, 0, 0], [1, 0, 0, 0]) = 0.6 / (1.0 * 1.0) = 0.60
	// Above old threshold (0.55) but below new threshold (0.65) → NO match
	queryLow := []float32{0.6, 0.8, 0, 0}
	noMatch := wm.Match(context.Background(), MatchSignals{QueryEmb: queryLow})
	if len(noMatch) != 0 {
		t.Errorf("expected no match at sim ~0.60 (below new threshold 0.65), got %d", len(noMatch))
	}

	// cosine([0.7, 0.714, 0, 0], [1, 0, 0, 0]) ≈ 0.70 → above 0.65 threshold → 1 match
	queryHigh := []float32{0.7, 0.714, 0, 0}
	yesMatch := wm.Match(context.Background(), MatchSignals{QueryEmb: queryHigh})
	if len(yesMatch) != 1 {
		t.Fatalf("expected 1 match at sim ~0.70, got %d", len(yesMatch))
	}
	if yesMatch[0].Signal != "embedding_match" {
		t.Errorf("expected embedding_match signal, got %s", yesMatch[0].Signal)
	}
}

func TestWarningMatcher_ConfidenceGapFilter(t *testing.T) {
	wm := NewWarningMatcher(nil, nil, slog.Default())

	// Rule A: file_match fires at confidence 1.0
	ruleAID := uuid.New()
	// Rule B: embedding would give ~0.56 — below new threshold (0.65), so it never registers.
	// Gap filter condition (conf < topConf*0.6 && gap > 0.25) would also drop it if it did fire,
	// but the threshold gate is the first barrier.
	ruleBID := uuid.New()
	ruleEmb := []float32{1, 0, 0, 0}
	// Query embedding: cosine ≈ 0.56 (below 0.65 threshold)
	queryEmb := []float32{0.56, 0.828, 0, 0}

	wm.cache = []CachedRule{
		{
			ID:        ruleAID,
			Content:   "ruleA",
			FilePaths: []string{"internal/repo/user_repo.go"},
		},
		{
			ID:        ruleBID,
			Content:   "ruleB",
			Embedding: ruleEmb,
		},
	}

	matches := wm.Match(context.Background(), MatchSignals{
		FilePath: "internal/repo/user_repo.go",
		QueryEmb: queryEmb,
	})

	if len(matches) != 1 {
		t.Fatalf("expected only Rule A, got %d matches", len(matches))
	}
	if matches[0].Rule.ID != ruleAID {
		t.Errorf("expected Rule A (file_match), got signal %s", matches[0].Signal)
	}
}

func TestWarningMatcher_NoRules(t *testing.T) {
	wm := NewWarningMatcher(nil, nil, slog.Default())
	matches := wm.Match(context.Background(), MatchSignals{Query: "anything"})
	if matches != nil {
		t.Errorf("expected nil for no rules, got %v", matches)
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct{ input, want string }{
		{"internal/repo/file.go", "internal/repo/file.go"},
		{"internal\\repo\\file.go", "internal/repo/file.go"},
		{"./internal/repo/file.go", "internal/repo/file.go"},
		{"D:\\go\\project\\file.go", "d:/go/project/file.go"},
	}
	for _, tt := range tests {
		got := normalizePath(tt.input)
		if got != tt.want {
			t.Errorf("normalizePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestKwOverlap(t *testing.T) {
	tests := []struct {
		queryKW, ruleKW []string
		wantMin         float64
	}{
		{[]string{"pgx", "pool"}, []string{"pgx", "connection", "pool"}, 0.6},
		{[]string{"redis", "cache"}, []string{"pgx", "connection", "pool"}, 0.0},
		{[]string{}, []string{"pgx"}, 0.0},
	}
	for _, tt := range tests {
		got := kwOverlap(tt.queryKW, tt.ruleKW)
		if got < tt.wantMin {
			t.Errorf("kwOverlap(%v, %v) = %f, want >= %f", tt.queryKW, tt.ruleKW, got, tt.wantMin)
		}
	}
}

func TestParseRuleMetadata(t *testing.T) {
	content := `WHEN: using pgx connection pool
WATCH: pool.Acquire() without defer Release()
BECAUSE: connection leak 3 times
DO: add defer conn.Release()
ANTIPATTERN: Acquire\(.*\)`

	meta := parseRuleMetadata(content, nil)

	if meta["rule_when"] != "using pgx connection pool" {
		t.Errorf("rule_when = %v", meta["rule_when"])
	}
	if meta["rule_watch"] != "pool.Acquire() without defer Release()" {
		t.Errorf("rule_watch = %v", meta["rule_watch"])
	}
	if meta["rule_antipattern"] != `Acquire\(.*\)` {
		t.Errorf("rule_antipattern = %v", meta["rule_antipattern"])
	}
}
