package app

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/pkg/vecutil"
)

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		query string
		minKW int
	}{
		{"how to configure nginx", 1},
		{"React useEffect cleanup function", 2},
		{"pgx/v5 database driver for Go", 2},
		{"", 0},
		{"the a an is", 0},
	}
	for _, tt := range tests {
		got := extractKeywords(tt.query)
		if len(got) < tt.minKW {
			t.Errorf("extractKeywords(%q) = %v (len=%d), want at least %d",
				tt.query, got, len(got), tt.minKW)
		}
	}
}

func TestExtractDomainKeywords(t *testing.T) {
	t.Run("React query has domain keywords", func(t *testing.T) {
		got := extractDomainKeywords("React useState hook rendering optimization in Next.js")
		hasDomain := false
		for _, kw := range got {
			if kw == "react" || kw == "usestate" || kw == "next.js" {
				hasDomain = true
			}
		}
		if !hasDomain {
			t.Errorf("expected React/useState/Next.js as domain keywords, got %v", got)
		}
	})

	t.Run("python django has domain keywords", func(t *testing.T) {
		got := extractDomainKeywords("python django web framework best practices")
		found := map[string]bool{}
		for _, kw := range got {
			found[kw] = true
		}
		if !found["python"] || !found["django"] {
			t.Errorf("expected python/django as domain keywords, got %v", got)
		}
	})

	t.Run("kubernetes has domain keywords", func(t *testing.T) {
		got := extractDomainKeywords("how to configure kubernetes pod autoscaling")
		found := map[string]bool{}
		for _, kw := range got {
			found[kw] = true
		}
		if !found["kubernetes"] {
			t.Errorf("expected kubernetes as domain keyword, got %v", got)
		}
	})

	t.Run("all generic words produce empty", func(t *testing.T) {
		got := extractDomainKeywords("database query optimization and performance")
		if len(got) != 0 {
			t.Errorf("expected empty for all-generic query, got %v", got)
		}
	})
}

func TestExactWordMatch(t *testing.T) {
	tests := []struct {
		content  string
		keyword  string
		expected bool
	}{
		{"the react component renders data", "react", true},
		{"interfaces are important in go", "face", false},
		{"the django framework is popular", "django", true},
		{"this is a kubernetes cluster", "kubernetes", true},
		{"postgresql driver pgx/v5", "pgx", true},
		{"the word preact is not react", "react", true},
		{"", "test", false},
		{"prefixreactpostfix", "react", false},
	}
	for _, tt := range tests {
		got := exactWordMatch(tt.content, tt.keyword)
		if got != tt.expected {
			t.Errorf("exactWordMatch(%q, %q) = %v, want %v",
				tt.content, tt.keyword, got, tt.expected)
		}
	}
}

func TestSimpleStem(t *testing.T) {
	tests := []struct {
		word     string
		expected string
	}{
		{"optimization", "optimiza"},
		{"rendering", "render"},
		{"dependencies", "dependenc"},
		{"configured", "configur"},
		{"handlers", "handl"},
		{"go", "go"},
		{"test", "test"},
		{"consolidation", "consolida"},
		{"running", "runn"},
	}
	for _, tt := range tests {
		got := simpleStem(tt.word)
		if got != tt.expected {
			t.Errorf("simpleStem(%q) = %q, want %q", tt.word, got, tt.expected)
		}
	}
}

func TestStemmedContains(t *testing.T) {
	tests := []struct {
		content  string
		keyword  string
		expected bool
	}{
		{"architecture follows clean pattern", "architecture", true},
		{"no match here at all", "kubernetes", false},
		{"interfaces are important", "face", false},
		{"the code handles errors", "error", true},
		{"the system uses consolidation", "consolidation", true},
		{"running the server process", "running", true},
	}
	for _, tt := range tests {
		got := stemmedContains(tt.content, tt.keyword)
		if got != tt.expected {
			t.Errorf("stemmedContains(%q, %q) = %v, want %v",
				tt.content, tt.keyword, got, tt.expected)
		}
	}
}

func TestNormalizedEntropy(t *testing.T) {
	uniform := []float64{0.5, 0.5, 0.5, 0.5, 0.5}
	h := normalizedEntropy(uniform)
	if h < 0.99 {
		t.Errorf("normalizedEntropy(uniform) = %.3f, want ~1.0", h)
	}

	peaked := []float64{0.9, 0.1, 0.05, 0.02, 0.01}
	h2 := normalizedEntropy(peaked)
	if h2 > 0.8 {
		t.Errorf("normalizedEntropy(peaked) = %.3f, want < 0.8", h2)
	}

	single := []float64{0.8}
	h3 := normalizedEntropy(single)
	if h3 != 0 {
		t.Errorf("normalizedEntropy(single) = %.3f, want 0", h3)
	}
}

func TestDomainSpecificRejection(t *testing.T) {
	projID := uuid.New()
	makeScored := func(contents ...string) []*domain.ScoredMemory {
		var result []*domain.ScoredMemory
		for _, c := range contents {
			result = append(result, &domain.ScoredMemory{
				Memory: &domain.MemoryItem{
					ID:        uuid.New(),
					ProjectID: &projID,
					Content:   c,
					Tier:      domain.TierEpisodic,
				},
				Score: domain.ImportanceScore{
					SemanticSimilarity: 0.55,
					Composite:         0.6,
				},
			})
		}
		return result
	}

	t.Run("reject React query against Go project", func(t *testing.T) {
		scored := makeScored(
			"ERROR: Go compilation failed with undefined filepath",
			"DECISION: Use pgx/v5 for PostgreSQL driver",
			"ARCHITECTURE: Clean architecture with domain layer",
			"CODE CHANGE: Added consolidation service",
			"CONFIGURATION: Ollama nomic-embed-text 768 dimensions",
		)
		rejected, reason := domainSpecificRejection(scored, "React useState hook rendering optimization in Next.js", len(scored))
		if !rejected {
			t.Errorf("expected React query to be rejected against Go project, got accepted")
		}
		if reason == "" {
			t.Errorf("expected rejection reason, got empty")
		}
	})

	t.Run("reject Django query against Go project", func(t *testing.T) {
		scored := makeScored(
			"ERROR: Go compilation failed",
			"DECISION: Use pgx/v5 driver",
			"ARCHITECTURE: Clean architecture",
			"CODE CHANGE: Added REST API handler",
			"BUG: MCP stdio transport issue",
		)
		rejected, _ := domainSpecificRejection(scored, "python django web framework best practices", len(scored))
		if !rejected {
			t.Errorf("expected Django query to be rejected against Go project")
		}
	})

	t.Run("accept matching query", func(t *testing.T) {
		scored := makeScored(
			"ERROR: pgx driver returns nil when connection pool exhausted",
			"DECISION: Use pgx/v5 for PostgreSQL",
			"ARCHITECTURE: Clean architecture pattern",
		)
		rejected, _ := domainSpecificRejection(scored, "pgx connection pool error handling", len(scored))
		if rejected {
			t.Errorf("expected pgx query to be accepted against content containing pgx")
		}
	})

	t.Run("accept when all keywords are generic", func(t *testing.T) {
		scored := makeScored(
			"ERROR: database query optimization failed",
		)
		rejected, _ := domainSpecificRejection(scored, "how does the database query work", len(scored))
		if rejected {
			t.Errorf("expected all-generic query to pass (no domain keywords to check)")
		}
	})

	t.Run("reject Kubernetes against memory project", func(t *testing.T) {
		scored := makeScored(
			"Memory consolidation runs every 6 hours",
			"Working memory capacity is 50 items",
			"Episodic memory stores code changes",
		)
		rejected, _ := domainSpecificRejection(scored, "kubernetes pod autoscaling with horizontal pod autoscaler", len(scored))
		if !rejected {
			t.Errorf("expected kubernetes query to be rejected against memory project")
		}
	})

	t.Run("reject Redis against memory project no overlap", func(t *testing.T) {
		scored := makeScored(
			"Memory consolidation uses two-pass approach",
			"Importance scoring formula with semantic similarity",
			"Token budget for recall assembly",
		)
		rejected, _ := domainSpecificRejection(scored, "Redis cluster sentinel failover and replication lag monitoring", len(scored))
		if !rejected {
			t.Errorf("expected Redis query to be rejected against non-matching content")
		}
	})
}

func TestFilterWeakCandidates(t *testing.T) {
	svc := &RecallService{}

	t.Run("filters below relative threshold", func(t *testing.T) {
		scored := []*domain.ScoredMemory{
			{Memory: &domain.MemoryItem{Tier: domain.TierEpisodic}, Score: domain.ImportanceScore{SemanticSimilarity: 0.8}},
			{Memory: &domain.MemoryItem{Tier: domain.TierEpisodic}, Score: domain.ImportanceScore{SemanticSimilarity: 0.6}},
			{Memory: &domain.MemoryItem{Tier: domain.TierEpisodic}, Score: domain.ImportanceScore{SemanticSimilarity: 0.3}},
			{Memory: &domain.MemoryItem{Tier: domain.TierEpisodic}, Score: domain.ImportanceScore{SemanticSimilarity: 0.1}},
		}
		result := svc.filterWeakCandidates(scored)
		if len(result) > 3 {
			t.Errorf("expected weak candidate (0.1) to be filtered, got %d results", len(result))
		}
	})

	t.Run("working memory exempt from filter", func(t *testing.T) {
		scored := []*domain.ScoredMemory{
			{Memory: &domain.MemoryItem{Tier: domain.TierEpisodic}, Score: domain.ImportanceScore{SemanticSimilarity: 0.8}},
			{Memory: &domain.MemoryItem{Tier: domain.TierWorking}, Score: domain.ImportanceScore{SemanticSimilarity: 0.0}},
		}
		result := svc.filterWeakCandidates(scored)
		if len(result) != 2 {
			t.Errorf("expected working memory to survive filter, got %d results", len(result))
		}
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		result := svc.filterWeakCandidates(nil)
		if len(result) != 0 {
			t.Errorf("expected empty result for nil input")
		}
	})
}

func TestSubmodularSelect(t *testing.T) {
	svc := &RecallService{}

	t.Run("respects total budget", func(t *testing.T) {
		scored := []*domain.ScoredMemory{
			{Memory: &domain.MemoryItem{ID: uuid.New(), Tier: domain.TierEpisodic, TokenCount: 300, Embedding: make([]float32, 10)}, Score: domain.ImportanceScore{Composite: 0.9}},
			{Memory: &domain.MemoryItem{ID: uuid.New(), Tier: domain.TierEpisodic, TokenCount: 300, Embedding: make([]float32, 10)}, Score: domain.ImportanceScore{Composite: 0.8}},
			{Memory: &domain.MemoryItem{ID: uuid.New(), Tier: domain.TierEpisodic, TokenCount: 300, Embedding: make([]float32, 10)}, Score: domain.ImportanceScore{Composite: 0.7}},
		}
		budget := domain.TokenBudget{Total: 500, Episodic: 500}
		result := svc.submodularSelect(scored, budget)
		totalTokens := 0
		for _, sm := range result {
			totalTokens += sm.Memory.TokenCount
		}
		if totalTokens > 500 {
			t.Errorf("submodularSelect exceeded budget: %d > 500", totalTokens)
		}
	})

	t.Run("respects tier limits", func(t *testing.T) {
		scored := []*domain.ScoredMemory{
			{Memory: &domain.MemoryItem{ID: uuid.New(), Tier: domain.TierEpisodic, TokenCount: 400, Embedding: make([]float32, 10)}, Score: domain.ImportanceScore{Composite: 0.9}},
			{Memory: &domain.MemoryItem{ID: uuid.New(), Tier: domain.TierEpisodic, TokenCount: 400, Embedding: make([]float32, 10)}, Score: domain.ImportanceScore{Composite: 0.8}},
			{Memory: &domain.MemoryItem{ID: uuid.New(), Tier: domain.TierSemantic, TokenCount: 200, Embedding: make([]float32, 10)}, Score: domain.ImportanceScore{Composite: 0.7}},
		}
		budget := domain.TokenBudget{Total: 1500, Episodic: 500, Semantic: 600}
		result := svc.submodularSelect(scored, budget)
		epTokens := 0
		for _, sm := range result {
			if sm.Memory.Tier == domain.TierEpisodic {
				epTokens += sm.Memory.TokenCount
			}
		}
		if epTokens > 500 {
			t.Errorf("episodic exceeded tier budget: %d > 500", epTokens)
		}
	})

	t.Run("empty input returns nil", func(t *testing.T) {
		result := svc.submodularSelect(nil, domain.DefaultBudget())
		if result != nil {
			t.Errorf("expected nil for empty input")
		}
	})
}

func TestRedundancyPenalty(t *testing.T) {
	svc := &RecallService{}

	t.Run("no penalty for empty selected set", func(t *testing.T) {
		cand := &domain.ScoredMemory{Memory: &domain.MemoryItem{Embedding: make([]float32, 10)}}
		penalty := svc.redundancyPenalty(cand, nil)
		if penalty != 0 {
			t.Errorf("expected 0 penalty for empty selected, got %f", penalty)
		}
	})

	t.Run("high penalty for identical embeddings", func(t *testing.T) {
		emb := make([]float32, 10)
		for i := range emb {
			emb[i] = float32(i)
		}
		cand := &domain.ScoredMemory{Memory: &domain.MemoryItem{Embedding: emb}}
		selected := []*domain.ScoredMemory{{Memory: &domain.MemoryItem{Embedding: emb}}}
		penalty := svc.redundancyPenalty(cand, selected)
		if penalty < 100 {
			t.Errorf("expected high penalty for identical embeddings, got %f", penalty)
		}
	})
}

func TestCosineSimilarity(t *testing.T) {
	t.Run("identical vectors", func(t *testing.T) {
		a := []float32{1, 0, 0}
		sim := vecutil.CosineSimilarity(a, a)
		if sim < 0.999 {
			t.Errorf("expected ~1.0 for identical vectors, got %f", sim)
		}
	})

	t.Run("orthogonal vectors", func(t *testing.T) {
		a := []float32{1, 0, 0}
		b := []float32{0, 1, 0}
		sim := vecutil.CosineSimilarity(a, b)
		if sim > 0.001 {
			t.Errorf("expected ~0.0 for orthogonal vectors, got %f", sim)
		}
	})

	t.Run("opposite vectors", func(t *testing.T) {
		a := []float32{1, 0}
		b := []float32{-1, 0}
		sim := vecutil.CosineSimilarity(a, b)
		if sim > -0.999 {
			t.Errorf("expected ~-1.0 for opposite vectors, got %f", sim)
		}
	})

	t.Run("empty vectors", func(t *testing.T) {
		sim := vecutil.CosineSimilarity(nil, nil)
		if sim != 0 {
			t.Errorf("expected 0 for empty vectors, got %f", sim)
		}
	})

	t.Run("different lengths", func(t *testing.T) {
		a := []float32{1, 0}
		b := []float32{1, 0, 0}
		sim := vecutil.CosineSimilarity(a, b)
		if sim != 0 {
			t.Errorf("expected 0 for different length vectors, got %f", sim)
		}
	})
}

func TestDedup(t *testing.T) {
	id1, id2 := uuid.New(), uuid.New()
	items := []*domain.MemoryItem{
		{ID: id1, Content: "first"},
		{ID: id2, Content: "second"},
		{ID: id1, Content: "first duplicate"},
	}
	result := dedup(items)
	if len(result) != 2 {
		t.Errorf("expected 2 unique items, got %d", len(result))
	}
}

func TestParseLLMScores(t *testing.T) {
	tests := []struct {
		raw      string
		expected int
		count    int
	}{
		{"8, 5, 3, 7, 2", 5, 5},
		{"8,5,3", 3, 3},
		{"[8, 5, 3]", 3, 3},
		{"8", 1, 1},
		{"8, 5", 0, 3},
	}
	for _, tt := range tests {
		scores := parseLLMScores(tt.raw, tt.count)
		if tt.expected == 0 && scores != nil {
			t.Errorf("parseLLMScores(%q, %d) = %v, want nil", tt.raw, tt.count, scores)
		}
		if tt.expected > 0 && len(scores) != tt.expected {
			t.Errorf("parseLLMScores(%q, %d) = %v (len=%d), want len=%d",
				tt.raw, tt.count, scores, len(scores), tt.expected)
		}
	}
}

func TestContainsCyrillic(t *testing.T) {
	if !containsCyrillic("как работает recall") {
		t.Error("expected true for Russian text")
	}
	if containsCyrillic("how does recall work") {
		t.Error("expected false for English text")
	}
	if containsCyrillic("") {
		t.Error("expected false for empty string")
	}
}

func TestIsWordChar(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{'a', true}, {'z', true}, {'A', true}, {'Z', true},
		{'0', true}, {'9', true}, {'_', true},
		{' ', false}, {'.', false}, {',', false}, {'-', false},
	}
	for _, tt := range tests {
		if got := isWordChar(tt.r); got != tt.want {
			t.Errorf("isWordChar(%q) = %v, want %v", tt.r, got, tt.want)
		}
	}
}

func TestCalibrateConfidence(t *testing.T) {
	tests := []struct {
		raw     float64
		wantMin float64
		wantMax float64
	}{
		{0.0, 0.0, 0.01},    // very low → near 0
		{0.40, 0.15, 0.25},   // weak match → low confidence
		{0.55, 0.45, 0.55},   // midpoint → ~50%
		{0.65, 0.70, 0.80},   // good match → solid confidence
		{0.75, 0.85, 0.92},   // strong match → high confidence
		{0.85, 0.93, 0.97},   // very strong → near 100%
	}
	for _, tt := range tests {
		got := calibrateConfidence(tt.raw)
		if got < tt.wantMin || got > tt.wantMax {
			t.Errorf("calibrateConfidence(%.2f) = %.3f, want [%.2f, %.2f]",
				tt.raw, got, tt.wantMin, tt.wantMax)
		}
	}
}

func TestParseTemporalHint(t *testing.T) {
	now := time.Now()
	tests := []struct {
		query string
		want  bool
	}{
		{"what happened yesterday", true},
		{"что было вчера", true},
		{"recent errors", false},  // "recent" not "recently"
		{"show me recently fixed bugs", true},
		{"last week changes", true},
		{"how does scoring work", false},
	}
	for _, tt := range tests {
		got := parseTemporalHint(tt.query, now)
		if (got != nil) != tt.want {
			t.Errorf("parseTemporalHint(%q): got hint=%v, want has_hint=%v", tt.query, got, tt.want)
		}
	}
}
