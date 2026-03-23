package app

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
)

// keywordOverlapSufficient returns true if at least one of the scenario's rule keywords
// appears with >=0.5 overlap against the task query. Used to skip scenarios where
// keyword-only matching is structurally insufficient.
func keywordOverlapSufficient(sc ABScenario) bool {
	queryKW := extractKeywords(sc.TaskQuery)
	return kwOverlap(queryKW, sc.RuleKeywords) >= 0.5
}

func TestABScenarios_BuggyDiffMatchesAntiPattern(t *testing.T) {
	ab := NewABBenchmark(slog.New(slog.NewTextHandler(nil, nil)))
	for _, sc := range ab.scenarios() {
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			added := extractAddedLines(sc.BuggyDiff)
			found, _ := matchAntiPattern(added, sc.AntiPattern)
			if !found {
				t.Errorf("BuggyDiff for %q does not match AntiPattern %q\nadded lines:\n%s", sc.ID, sc.AntiPattern, added)
			}
		})
	}
}

func TestABScenarios_CorrectDiffClean(t *testing.T) {
	ab := NewABBenchmark(slog.New(slog.NewTextHandler(nil, nil)))
	for _, sc := range ab.scenarios() {
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			added := extractAddedLines(sc.CorrectDiff)
			found, snippet := matchAntiPattern(added, sc.AntiPattern)
			if found {
				t.Errorf("CorrectDiff for %q triggers AntiPattern %q\nmatching snippet: %s", sc.ID, sc.AntiPattern, snippet)
			}
		})
	}
}

func TestABScenarios_RuleMatchesByFile(t *testing.T) {
	ab := NewABBenchmark(slog.New(slog.NewTextHandler(nil, nil)))
	ctx := context.Background()

	for _, sc := range ab.scenarios() {
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			rule := CachedRule{
				ID:           uuid.New(),
				Content:      sc.RuleContent,
				FilePaths:    sc.RuleFiles,
				WhenKeywords: sc.RuleKeywords,
				AntiPattern:  sc.AntiPattern,
			}

			wm := NewWarningMatcher(nil, nil, slog.New(slog.NewTextHandler(nil, nil)))
			wm.mu.Lock()
			wm.cache = []CachedRule{rule}
			wm.mu.Unlock()

			matches := wm.Match(ctx, MatchSignals{FilePath: sc.TaskFile})

			var fileMatches []MatchedWarning
			for _, m := range matches {
				if m.Signal == "file_match" {
					fileMatches = append(fileMatches, m)
				}
			}
			if len(fileMatches) == 0 {
				t.Errorf("expected at least 1 file_match for %q (file=%q), got %d matches total", sc.ID, sc.TaskFile, len(matches))
			}
		})
	}
}

func TestABScenarios_RuleMatchesByKeyword(t *testing.T) {
	ab := NewABBenchmark(slog.New(slog.NewTextHandler(nil, nil)))
	ctx := context.Background()

	for _, sc := range ab.scenarios() {
		sc := sc
		t.Run(sc.ID, func(t *testing.T) {
			if !keywordOverlapSufficient(sc) {
				t.Skipf("keyword overlap < 0.5 for query %q — structural skip", sc.TaskQuery)
			}

			rule := CachedRule{
				ID:           uuid.New(),
				Content:      sc.RuleContent,
				FilePaths:    nil, // cleared to force keyword path
				WhenKeywords: sc.RuleKeywords,
				AntiPattern:  sc.AntiPattern,
			}

			wm := NewWarningMatcher(nil, nil, slog.New(slog.NewTextHandler(nil, nil)))
			wm.mu.Lock()
			wm.cache = []CachedRule{rule}
			wm.mu.Unlock()

			matches := wm.Match(ctx, MatchSignals{Query: sc.TaskQuery})

			var kwMatches []MatchedWarning
			for _, m := range matches {
				if m.Signal == "keyword_match" {
					kwMatches = append(kwMatches, m)
				}
			}
			if len(kwMatches) == 0 {
				t.Errorf("expected at least 1 keyword_match for %q (query=%q), got %d matches", sc.ID, sc.TaskQuery, len(matches))
			}
		})
	}
}

func TestABScenarios_DistractorsNotMatchFile(t *testing.T) {
	ab := NewABBenchmark(slog.New(slog.NewTextHandler(nil, nil)))
	ctx := context.Background()
	allScenarios := ab.scenarios()

	for i, sc := range allScenarios {
		sc := sc
		i := i
		t.Run(sc.ID, func(t *testing.T) {
			var distractors []CachedRule
			for j, other := range allScenarios {
				if j == i {
					continue
				}
				// Only use scenarios whose file paths differ from the current scenario's
				overlaps := false
				for _, otherFile := range other.RuleFiles {
					for _, scFile := range sc.RuleFiles {
						if normalizePath(otherFile) == normalizePath(scFile) {
							overlaps = true
							break
						}
					}
					if overlaps {
						break
					}
				}
				if !overlaps {
					distractors = append(distractors, CachedRule{
						ID:           uuid.New(),
						Content:      other.RuleContent,
						FilePaths:    other.RuleFiles,
						WhenKeywords: other.RuleKeywords,
						AntiPattern:  other.AntiPattern,
					})
				}
				if len(distractors) == 2 {
					break
				}
			}

			if len(distractors) < 2 {
				t.Skipf("could not find 2 distractors with distinct file paths for %q", sc.ID)
			}

			wm := NewWarningMatcher(nil, nil, slog.New(slog.NewTextHandler(nil, nil)))
			wm.mu.Lock()
			wm.cache = distractors
			wm.mu.Unlock()

			matches := wm.Match(ctx, MatchSignals{FilePath: sc.TaskFile})
			if len(matches) != 0 {
				t.Errorf("expected 0 distractor matches for file %q, got %d", sc.TaskFile, len(matches))
				for _, m := range matches {
					t.Logf("  matched: signal=%s conf=%.2f files=%v", m.Signal, m.Confidence, m.Rule.FilePaths)
				}
			}
		})
	}
}

func TestABBenchmark_Run(t *testing.T) {
	ab := NewABBenchmark(slog.New(slog.NewTextHandler(nil, nil)))
	ctx := context.Background()

	report, err := ab.Run(ctx)
	if err != nil {
		t.Fatalf("ab.Run returned error: %v", err)
	}

	if report.Scenarios != 12 {
		t.Errorf("expected 12 scenarios, got %d", report.Scenarios)
	}
	if report.WarningRecall != 1.0 {
		t.Errorf("expected WarningRecall=1.0 (all rules matched by file), got %.4f", report.WarningRecall)
	}
	if report.WarningPrecision <= 0.8 {
		t.Errorf("expected WarningPrecision > 0.8, got %.4f", report.WarningPrecision)
	}
	if report.PreventionLift <= 0.5 {
		t.Errorf("expected PreventionLift > 0.5, got %.4f", report.PreventionLift)
	}
	if len(report.Details) != 24 {
		t.Errorf("expected 24 detail rows (12 scenarios × 2 conditions), got %d", len(report.Details))
	}

	maxLatency := 10 * time.Millisecond
	for _, d := range report.Details {
		if d.Condition == ConditionTreatment && d.MatchLatency > maxLatency {
			t.Errorf("scenario %q treatment latency %v exceeds 10ms", d.ScenarioID, d.MatchLatency)
		}
	}

	if report.Formatted == "" {
		t.Error("expected non-empty Formatted report")
	}
	if !abContains(report.Formatted, "A/B Test") {
		n := len(report.Formatted)
		if n > 500 {
			n = 500
		}
		t.Errorf("Formatted report missing 'A/B Test' heading\nFormatted:\n%s", report.Formatted[:n])
	}
}

func TestABBenchmark_ReportFormat(t *testing.T) {
	ab := NewABBenchmark(slog.New(slog.NewTextHandler(nil, nil)))
	ctx := context.Background()

	report, err := ab.Run(ctx)
	if err != nil {
		t.Fatalf("ab.Run returned error: %v", err)
	}

	sections := []string{
		"Warning Quality",
		"Prevention Effectiveness",
		"Latency",
		"By Category",
	}
	for _, section := range sections {
		if !abContains(report.Formatted, section) {
			t.Errorf("Formatted report missing section %q", section)
		}
	}
}

// abContains checks whether s contains sub as a substring.
func abContains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
