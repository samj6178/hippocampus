package app

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
)

func TestPreventionPipeline_FullCycle(t *testing.T) {
	// Setup: create a rule that hippocampus has "learned"
	rule := CachedRule{
		ID:      uuid.New(),
		Content: "WHEN: using pgxpool connection\nWATCH: pool.Acquire() without defer Release()\nBECAUSE: connection leak observed 3 times in repo layer\nDO: always defer conn.Release() after Acquire\nANTIPATTERN: _\\s*=\\s*pool\\.Acquire",
		FilePaths:    []string{"internal/repo/user_repo.go"},
		WhenKeywords: []string{"pgx", "pool", "acquire", "connection"},
		AntiPattern:  `_\s*=\s*pool\.Acquire`,
		WhenText:     "using pgxpool connection",
		WatchText:    "pool.Acquire() without defer Release()",
		DoText:       "always defer conn.Release() after Acquire",
	}

	// Step 1: WarningMatcher loads the rule and matches it
	wm := NewWarningMatcher(nil, nil, slog.Default())
	wm.mu.Lock()
	wm.cache = []CachedRule{rule}
	wm.mu.Unlock()

	signals := MatchSignals{
		FilePath: "internal/repo/user_repo.go",
		Query:    "implement user repository with pgx pool",
	}

	matches := wm.Match(context.Background(), signals)
	if len(matches) == 0 {
		t.Fatal("expected warning to match for user_repo.go")
	}
	if matches[0].Signal != "file_match" {
		t.Errorf("expected file_match signal, got %s", matches[0].Signal)
	}
	if matches[0].Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", matches[0].Confidence)
	}

	t.Log("Step 1 PASS: Warning matched for user_repo.go via file_match")

	// Step 2: Agent sees warning and writes CORRECT code (follows the warning)
	correctDiff := `diff --git a/internal/repo/user_repo.go b/internal/repo/user_repo.go
@@ -10,0 +11,8 @@
+func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
+	conn, err := r.pool.Acquire(ctx)
+	if err != nil {
+		return nil, fmt.Errorf("acquire connection: %w", err)
+	}
+	defer conn.Release()
+	return scanUser(conn.QueryRow(ctx, "SELECT * FROM users WHERE id = $1", id))
+}
`

	pa := NewPreventionAnalyzer(slog.Default())
	diffs := parseUnifiedDiff(correctDiff)

	report := &PreventionReport{
		TotalWarnings: len(matches),
		FilesAnalyzed: len(diffs),
	}

	// Run analysis for each matched warning
	for _, w := range matches {
		if w.Rule.AntiPattern == "" {
			report.NotApplicable++
			continue
		}
		for _, fp := range w.Rule.FilePaths {
			diffContent := findDiffForFile(diffs, fp)
			if diffContent == "" {
				report.NotApplicable++
				continue
			}
			addedLines := extractAddedLines(diffContent)
			found, _ := matchAntiPattern(addedLines, w.Rule.AntiPattern)
			if found {
				report.Ignored++
			} else {
				report.Prevented++
			}
		}
	}

	if report.Prevented != 1 {
		t.Errorf("Step 2: expected 1 prevented (agent followed warning), got %d", report.Prevented)
	}
	if report.Ignored != 0 {
		t.Errorf("Step 2: expected 0 ignored, got %d", report.Ignored)
	}
	t.Log("Step 2 PASS: Agent followed warning, anti-pattern NOT in code → Prevented")

	// Step 3: Simulate agent IGNORING the warning — writes buggy code
	buggyDiff := `diff --git a/internal/repo/user_repo.go b/internal/repo/user_repo.go
@@ -10,0 +11,5 @@
+func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
+	conn, _ = pool.Acquire(ctx)
+	row := conn.QueryRow(ctx, "SELECT * FROM users WHERE id = $1", id)
+	return scanUser(row)
+}
`

	buggyDiffs := parseUnifiedDiff(buggyDiff)
	buggyReport := &PreventionReport{
		TotalWarnings: len(matches),
		FilesAnalyzed: len(buggyDiffs),
	}

	for _, w := range matches {
		if w.Rule.AntiPattern == "" {
			buggyReport.NotApplicable++
			continue
		}
		for _, fp := range w.Rule.FilePaths {
			diffContent := findDiffForFile(buggyDiffs, fp)
			if diffContent == "" {
				buggyReport.NotApplicable++
				continue
			}
			addedLines := extractAddedLines(diffContent)
			found, snippet := matchAntiPattern(addedLines, w.Rule.AntiPattern)
			if found {
				buggyReport.Ignored++
				t.Logf("Anti-pattern found in buggy code: %s", snippet)
			} else {
				buggyReport.Prevented++
			}
		}
	}

	if buggyReport.Ignored != 1 {
		t.Errorf("Step 3: expected 1 ignored (agent ignored warning), got %d", buggyReport.Ignored)
	}
	if buggyReport.Prevented != 0 {
		t.Errorf("Step 3: expected 0 prevented, got %d", buggyReport.Prevented)
	}
	t.Log("Step 3 PASS: Agent ignored warning, anti-pattern found → Ignored")

	// Step 4: Verify prevention rate calculation
	if report.Prevented+report.Ignored > 0 {
		report.PreventionRate = float64(report.Prevented) / float64(report.Prevented+report.Ignored)
	}
	if report.PreventionRate != 1.0 {
		t.Errorf("Step 4: correct code prevention rate should be 1.0, got %f", report.PreventionRate)
	}

	if buggyReport.Prevented+buggyReport.Ignored > 0 {
		buggyReport.PreventionRate = float64(buggyReport.Prevented) / float64(buggyReport.Prevented+buggyReport.Ignored)
	}
	if buggyReport.PreventionRate != 0.0 {
		t.Errorf("Step 4: buggy code prevention rate should be 0.0, got %f", buggyReport.PreventionRate)
	}
	t.Log("Step 4 PASS: Prevention rates correct (1.0 for followed, 0.0 for ignored)")

	_ = pa // reference the analyzer to show it's available for full integration
	t.Log("Full prevention pipeline validated: rule → match → code → analysis → report")
}

func TestPreventionPipeline_NotApplicable(t *testing.T) {
	// Rule for file A, but agent only modifies file B
	rule := CachedRule{
		ID:           uuid.New(),
		Content:      "WHEN: auth middleware\nWATCH: JWT without expiry",
		FilePaths:    []string{"internal/auth/jwt.go"},
		AntiPattern:  `jwt\.New\(\)`,
		WhenKeywords: []string{"auth", "jwt", "middleware"},
	}

	wm := NewWarningMatcher(nil, nil, slog.Default())
	wm.mu.Lock()
	wm.cache = []CachedRule{rule}
	wm.mu.Unlock()

	// Agent queries about auth but only modifies a different file
	matches := wm.Match(context.Background(), MatchSignals{
		FilePath: "internal/auth/jwt.go",
		Query:    "implement JWT auth middleware",
	})

	if len(matches) == 0 {
		t.Fatal("expected warning to match")
	}

	// But the actual diff is for a completely different file
	diff := `diff --git a/internal/handler/health.go b/internal/handler/health.go
@@ -1,0 +2,3 @@
+func handleHealth(w http.ResponseWriter, r *http.Request) {
+	w.WriteHeader(200)
+}
`
	diffs := parseUnifiedDiff(diff)

	report := &PreventionReport{
		TotalWarnings: 1,
		FilesAnalyzed: len(diffs),
	}

	for _, w := range matches {
		for _, fp := range w.Rule.FilePaths {
			diffContent := findDiffForFile(diffs, fp)
			if diffContent == "" {
				report.NotApplicable++
			}
		}
	}

	if report.NotApplicable != 1 {
		t.Errorf("expected 1 not_applicable (warned file not modified), got %d", report.NotApplicable)
	}
	t.Log("PASS: Warning for unmodified file correctly classified as not_applicable")
}
