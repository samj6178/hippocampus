package app

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
)

func TestParseUnifiedDiff(t *testing.T) {
	raw := `diff --git a/internal/repo/user_repo.go b/internal/repo/user_repo.go
index abc1234..def5678 100644
--- a/internal/repo/user_repo.go
+++ b/internal/repo/user_repo.go
@@ -10,6 +10,8 @@ func (r *UserRepo) Get(ctx context.Context) {
 	conn, err := r.pool.Acquire(ctx)
+	// no defer here — bug!
+	result := conn.Query(ctx, "SELECT 1")
 	return result
diff --git a/internal/app/service.go b/internal/app/service.go
index 111..222 100644
--- a/internal/app/service.go
+++ b/internal/app/service.go
@@ -1,3 +1,4 @@
+package app
 func Hello() {}
`

	diffs := parseUnifiedDiff(raw)

	if len(diffs) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(diffs), keys(diffs))
	}

	if _, ok := diffs["internal/repo/user_repo.go"]; !ok {
		t.Error("missing internal/repo/user_repo.go")
	}
	if _, ok := diffs["internal/app/service.go"]; !ok {
		t.Error("missing internal/app/service.go")
	}
}

func TestParseUnifiedDiff_Empty(t *testing.T) {
	diffs := parseUnifiedDiff("")
	if len(diffs) != 0 {
		t.Errorf("expected 0 files for empty diff, got %d", len(diffs))
	}
}

func TestExtractAddedLines(t *testing.T) {
	diff := `--- a/file.go
+++ b/file.go
@@ -1,3 +1,5 @@
 existing line
+added line 1
+added line 2
 another existing
-removed line
`

	added := extractAddedLines(diff)
	if !contains(added, "added line 1") {
		t.Error("expected 'added line 1' in added lines")
	}
	if !contains(added, "added line 2") {
		t.Error("expected 'added line 2' in added lines")
	}
	if contains(added, "existing line") {
		t.Error("should not contain unchanged lines")
	}
	if contains(added, "removed line") {
		t.Error("should not contain removed lines")
	}
	if contains(added, "b/file.go") {
		t.Error("should not contain +++ header")
	}
}

func TestMatchAntiPattern_Found(t *testing.T) {
	addedLines := "conn, err := r.pool.Acquire(ctx)\nresult := conn.Query(ctx, sql)\n"
	pattern := `Acquire\(.*\)`

	found, snippet := matchAntiPattern(addedLines, pattern)
	if !found {
		t.Error("expected anti-pattern to be found")
	}
	if snippet == "" {
		t.Error("expected non-empty snippet")
	}
}

func TestMatchAntiPattern_NotFound(t *testing.T) {
	addedLines := "defer conn.Release()\nresult := conn.Query(ctx, sql)\n"
	pattern := `Acquire\(.*\)`

	found, _ := matchAntiPattern(addedLines, pattern)
	if found {
		t.Error("expected anti-pattern NOT to be found")
	}
}

func TestMatchAntiPattern_InvalidRegex(t *testing.T) {
	addedLines := "this has [invalid( in it\n"
	pattern := `[invalid(`

	found, snippet := matchAntiPattern(addedLines, pattern)
	if !found {
		t.Error("invalid regex should fall back to literal substring match")
	}
	if snippet == "" {
		t.Error("expected snippet from literal match")
	}
}

func TestMatchAntiPattern_Empty(t *testing.T) {
	found, _ := matchAntiPattern("", `Acquire\(`)
	if found {
		t.Error("empty added lines should not match")
	}

	found, _ = matchAntiPattern("some code", "")
	if found {
		t.Error("empty pattern should not match")
	}
}

func TestFindDiffForFile_ExactMatch(t *testing.T) {
	diffs := map[string]string{
		"internal/repo/user_repo.go": "diff content here",
		"internal/app/service.go":    "other diff",
	}

	got := findDiffForFile(diffs, "internal/repo/user_repo.go")
	if got != "diff content here" {
		t.Errorf("expected exact match, got %q", got)
	}
}

func TestFindDiffForFile_SuffixMatch(t *testing.T) {
	diffs := map[string]string{
		"internal/repo/user_repo.go": "diff content",
	}

	got := findDiffForFile(diffs, "repo/user_repo.go")
	if got != "diff content" {
		t.Errorf("expected suffix match, got %q", got)
	}
}

func TestFindDiffForFile_NotFound(t *testing.T) {
	diffs := map[string]string{
		"internal/repo/user_repo.go": "diff content",
	}

	got := findDiffForFile(diffs, "internal/app/other.go")
	if got != "" {
		t.Errorf("expected empty for non-matching file, got %q", got)
	}
}

func TestFindDiffForFile_NoFalseSubstringMatch(t *testing.T) {
	diffs := map[string]string{
		"internal/repo/test_user_repo.go": "wrong diff",
		"internal/repo/user_repo.go":      "correct diff",
	}

	// "user_repo.go" must match "internal/repo/user_repo.go" (path boundary),
	// NOT "internal/repo/test_user_repo.go" (no boundary before "user_repo.go").
	got := findDiffForFile(diffs, "user_repo.go")
	if got != "correct diff" {
		t.Errorf("expected path-boundary match, got %q", got)
	}
}

func TestFindDiffForFile_NoMatchWithoutBoundary(t *testing.T) {
	diffs := map[string]string{
		"internal/repo/test_user_repo.go": "wrong diff",
	}

	got := findDiffForFile(diffs, "user_repo.go")
	if got != "" {
		t.Errorf("expected no match for non-boundary suffix, got %q", got)
	}
}

func TestPathSuffixMatch(t *testing.T) {
	tests := []struct {
		full, suffix string
		want         bool
	}{
		{"internal/repo/user_repo.go", "user_repo.go", true},
		{"internal/repo/user_repo.go", "repo/user_repo.go", true},
		{"internal/repo/test_user_repo.go", "user_repo.go", false},
		{"user_repo.go", "user_repo.go", true},
		{"a/b.go", "c/b.go", false},
	}
	for _, tt := range tests {
		got := pathSuffixMatch(tt.full, tt.suffix)
		if got != tt.want {
			t.Errorf("pathSuffixMatch(%q, %q) = %v, want %v", tt.full, tt.suffix, got, tt.want)
		}
	}
}

func TestAnalyze_Prevented(t *testing.T) {
	pa := NewPreventionAnalyzer(slog.Default())

	// Anti-pattern: SQL string concatenation. Good code uses parameterized queries.
	warnings := []MatchedWarning{
		{
			Rule: CachedRule{
				ID:          uuid.New(),
				Content:     "WHEN: SQL queries\nWATCH: string concatenation in SQL",
				AntiPattern: `"\s*\+\s*\w+\s*\+\s*"`,
				FilePaths:   []string{"test_file.go"},
			},
			Signal:     "file_match",
			Confidence: 1.0,
		},
	}

	// Agent used parameterized query — anti-pattern NOT present → prevented
	diffs := map[string]string{
		"test_file.go": `@@ -1,3 +1,5 @@
+rows, err := pool.Query(ctx, "SELECT * FROM users WHERE id = $1", userID)
+defer rows.Close()
`,
	}

	report := analyzeWithDiffs(pa, warnings, diffs)

	if report.Prevented != 1 {
		t.Errorf("expected 1 prevented, got %d", report.Prevented)
	}
	if report.Ignored != 0 {
		t.Errorf("expected 0 ignored, got %d", report.Ignored)
	}
}

func TestAnalyze_Ignored(t *testing.T) {
	pa := NewPreventionAnalyzer(slog.Default())

	warnings := []MatchedWarning{
		{
			Rule: CachedRule{
				ID:          uuid.New(),
				Content:     "WHEN: SQL queries\nWATCH: string concatenation",
				AntiPattern: `"SELECT.*"\s*\+`,
				FilePaths:   []string{"internal/repo/query.go"},
			},
		},
	}

	diffs := map[string]string{
		"internal/repo/query.go": `@@ -5,3 +5,4 @@
+	query := "SELECT * FROM users WHERE id = " + userID
`,
	}

	report := analyzeWithDiffs(pa, warnings, diffs)

	if report.Ignored != 1 {
		t.Errorf("expected 1 ignored, got %d", report.Ignored)
	}
	if report.Prevented != 0 {
		t.Errorf("expected 0 prevented, got %d", report.Prevented)
	}
}

func TestAnalyze_NotApplicable(t *testing.T) {
	pa := NewPreventionAnalyzer(slog.Default())

	warnings := []MatchedWarning{
		{
			Rule: CachedRule{
				ID:          uuid.New(),
				Content:     "Some rule about auth",
				AntiPattern: `jwt\.Parse\(`,
				FilePaths:   []string{"internal/auth/jwt.go"},
			},
		},
	}

	// File not in diff at all
	diffs := map[string]string{
		"internal/repo/user_repo.go": "+some other change\n",
	}

	report := analyzeWithDiffs(pa, warnings, diffs)

	if report.NotApplicable != 1 {
		t.Errorf("expected 1 not_applicable, got %d", report.NotApplicable)
	}
	if report.Prevented != 0 || report.Ignored != 0 {
		t.Errorf("expected 0 prevented/ignored, got %d/%d", report.Prevented, report.Ignored)
	}
}

func TestAnalyze_NoAntiPattern(t *testing.T) {
	pa := NewPreventionAnalyzer(slog.Default())

	warnings := []MatchedWarning{
		{
			Rule: CachedRule{
				ID:      uuid.New(),
				Content: "Rule without anti-pattern",
				// AntiPattern is empty
				FilePaths: []string{"some/file.go"},
			},
		},
	}

	diffs := map[string]string{
		"some/file.go": "+change\n",
	}

	report := analyzeWithDiffs(pa, warnings, diffs)
	if report.NotApplicable != 1 {
		t.Errorf("expected 1 not_applicable for rule without anti-pattern, got %d", report.NotApplicable)
	}
}

func TestAnalyze_PreventionRate(t *testing.T) {
	pa := NewPreventionAnalyzer(slog.Default())

	warnings := []MatchedWarning{
		{
			Rule: CachedRule{
				ID: uuid.New(), Content: "rule1",
				AntiPattern: `badFunc\(`, FilePaths: []string{"a.go"},
			},
		},
		{
			Rule: CachedRule{
				ID: uuid.New(), Content: "rule2",
				AntiPattern: `evilCall\(`, FilePaths: []string{"b.go"},
			},
		},
		{
			Rule: CachedRule{
				ID: uuid.New(), Content: "rule3",
				AntiPattern: `danger\(`, FilePaths: []string{"c.go"},
			},
		},
	}

	diffs := map[string]string{
		"a.go": "+goodFunc(x)\n",     // prevented (no badFunc)
		"b.go": "+evilCall(y)\n",     // ignored (anti-pattern present)
		"c.go": "+safeOperation()\n", // prevented (no danger)
	}

	report := analyzeWithDiffs(pa, warnings, diffs)

	if report.Prevented != 2 {
		t.Errorf("expected 2 prevented, got %d", report.Prevented)
	}
	if report.Ignored != 1 {
		t.Errorf("expected 1 ignored, got %d", report.Ignored)
	}

	expectedRate := 2.0 / 3.0
	if report.PreventionRate < expectedRate-0.01 || report.PreventionRate > expectedRate+0.01 {
		t.Errorf("expected prevention rate ~%.2f, got %.2f", expectedRate, report.PreventionRate)
	}
}

func TestAnalyze_EmptyWarnings(t *testing.T) {
	pa := NewPreventionAnalyzer(slog.Default())
	report, err := pa.Analyze(context.Background(), nil, ".", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.TotalWarnings != 0 {
		t.Errorf("expected 0 total warnings, got %d", report.TotalWarnings)
	}
}

// analyzeWithDiffs is a test helper that runs analysis with pre-parsed diffs
// instead of calling git.
func analyzeWithDiffs(pa *PreventionAnalyzer, warnings []MatchedWarning, diffs map[string]string) *PreventionReport {
	report := &PreventionReport{
		TotalWarnings: len(warnings),
		FilesAnalyzed: len(diffs),
	}

	seen := map[string]bool{}

	for _, w := range warnings {
		if w.Rule.AntiPattern == "" {
			report.NotApplicable++
			continue
		}
		if len(w.Rule.FilePaths) == 0 {
			report.NotApplicable++
			continue
		}

		for _, fp := range w.Rule.FilePaths {
			key := fp + "|" + w.Rule.AntiPattern
			if seen[key] {
				continue
			}
			seen[key] = true

			diffContent := findDiffForFile(diffs, fp)
			if diffContent == "" {
				report.NotApplicable++
				report.Details = append(report.Details, PreventionDetail{
					File: fp, Rule: firstLine(w.Rule.Content),
					AntiPattern: w.Rule.AntiPattern, DiffSnippet: "(file not modified)",
				})
				continue
			}

			addedLines := extractAddedLines(diffContent)
			found, snippet := matchAntiPattern(addedLines, w.Rule.AntiPattern)
			if found {
				report.Ignored++
			} else {
				report.Prevented++
			}
			report.Details = append(report.Details, PreventionDetail{
				File: fp, Rule: firstLine(w.Rule.Content),
				AntiPattern: w.Rule.AntiPattern, FoundInDiff: found, DiffSnippet: snippet,
			})
		}
	}

	if report.Prevented+report.Ignored > 0 {
		report.PreventionRate = float64(report.Prevented) / float64(report.Prevented+report.Ignored)
	}

	return report
}

func keys(m map[string]string) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && len(s) >= len(substr) &&
		(s == substr || findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
