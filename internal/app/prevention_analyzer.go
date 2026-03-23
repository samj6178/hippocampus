package app

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type PreventionAnalyzer struct {
	logger *slog.Logger
}

func NewPreventionAnalyzer(logger *slog.Logger) *PreventionAnalyzer {
	return &PreventionAnalyzer{logger: logger}
}

type PreventionReport struct {
	TotalWarnings  int               `json:"total_warnings"`
	FilesAnalyzed  int               `json:"files_analyzed"`
	Prevented      int               `json:"prevented"`
	Ignored        int               `json:"ignored"`
	NotApplicable  int               `json:"not_applicable"`
	PreventionRate float64           `json:"prevention_rate"`
	Details        []PreventionDetail `json:"details,omitempty"`
}

type PreventionDetail struct {
	File        string `json:"file"`
	Rule        string `json:"rule"`
	AntiPattern string `json:"anti_pattern"`
	FoundInDiff bool   `json:"found_in_diff"`
	DiffSnippet string `json:"diff_snippet,omitempty"`
}

// Analyze checks git diff against exposed warnings to measure prevention effectiveness.
// baseCommit is the git commit hash from session start. If non-empty, uses
// `git diff <baseCommit>` to capture all changes since session start (including commits).
// If empty, falls back to `git diff HEAD` (uncommitted changes only).
//
// For each warning with an anti-pattern regex and file paths:
//   - If the warned file wasn't modified → NotApplicable
//   - If modified but anti-pattern NOT in added lines → Prevented
//   - If modified and anti-pattern IS in added lines → Ignored
func (pa *PreventionAnalyzer) Analyze(ctx context.Context, warnings []MatchedWarning, projectRoot string, baseCommit string) (*PreventionReport, error) {
	if len(warnings) == 0 {
		return &PreventionReport{}, nil
	}

	diffs, err := gitDiff(ctx, projectRoot, baseCommit)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}

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
					File:        fp,
					Rule:        firstLine(w.Rule.Content),
					AntiPattern: w.Rule.AntiPattern,
					FoundInDiff: false,
					DiffSnippet: "(file not modified)",
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
				File:        fp,
				Rule:        firstLine(w.Rule.Content),
				AntiPattern: w.Rule.AntiPattern,
				FoundInDiff: found,
				DiffSnippet: snippet,
			})
		}
	}

	if report.Prevented+report.Ignored > 0 {
		report.PreventionRate = float64(report.Prevented) / float64(report.Prevented+report.Ignored)
	}

	pa.logger.Info("prevention analysis complete",
		"total", report.TotalWarnings,
		"prevented", report.Prevented,
		"ignored", report.Ignored,
		"not_applicable", report.NotApplicable,
		"rate", report.PreventionRate,
	)

	return report, nil
}

// gitDiff returns map[normalizedFilePath]diffContent.
// If baseCommit is non-empty, runs `git diff <baseCommit>` to capture all changes since session start.
// Otherwise falls back to `git diff HEAD` (uncommitted changes only).
func gitDiff(ctx context.Context, projectRoot string, baseCommit string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	args := []string{"diff"}
	if baseCommit != "" {
		args = append(args, baseCommit)
	} else {
		args = append(args, "HEAD")
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	return parseUnifiedDiff(string(out)), nil
}

// parseUnifiedDiff splits unified diff output into per-file sections.
// Returns map[normalizedFilePath]fullDiffSection.
func parseUnifiedDiff(raw string) map[string]string {
	result := map[string]string{}
	sections := strings.Split(raw, "diff --git ")
	for _, section := range sections {
		if section == "" {
			continue
		}
		// Extract file path from "a/path b/path" header
		firstLine := section
		if idx := strings.IndexByte(section, '\n'); idx > 0 {
			firstLine = section[:idx]
		}
		parts := strings.Fields(firstLine)
		if len(parts) < 2 {
			continue
		}
		// Use b/ path (destination)
		fp := strings.TrimPrefix(parts[len(parts)-1], "b/")
		fp = normalizePath(fp)
		result[fp] = section
	}
	return result
}

// findDiffForFile finds the diff section for a given file path.
// Uses path-boundary-aware suffix matching to avoid false positives
// (e.g. "user_repo.go" must NOT match "test_user_repo.go").
func findDiffForFile(diffs map[string]string, filePath string) string {
	norm := normalizePath(filePath)

	// Exact match
	if d, ok := diffs[norm]; ok {
		return d
	}

	// Path-boundary suffix match: the suffix must be preceded by "/" or be the full string.
	// This prevents "user_repo.go" from matching "test_user_repo.go".
	var best string
	bestLen := 0
	for dp, content := range diffs {
		if pathSuffixMatch(dp, norm) || pathSuffixMatch(norm, dp) {
			// Prefer longest match (most specific path)
			matchLen := len(dp) + len(norm)
			if matchLen > bestLen {
				best = content
				bestLen = matchLen
			}
		}
	}
	return best
}

// pathSuffixMatch returns true if full ends with suffix at a "/" boundary.
func pathSuffixMatch(full, suffix string) bool {
	if !strings.HasSuffix(full, suffix) {
		return false
	}
	if len(full) == len(suffix) {
		return true
	}
	// Character before the suffix must be "/"
	return full[len(full)-len(suffix)-1] == '/'
}

// extractAddedLines returns only lines starting with "+" (excluding "+++ " header).
func extractAddedLines(diff string) string {
	var b strings.Builder
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			b.WriteString(strings.TrimPrefix(line, "+"))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// matchAntiPattern checks if the anti-pattern regex matches any added line.
// Returns (matched, snippet) where snippet is the matching line or empty.
func matchAntiPattern(addedLines, pattern string) (bool, string) {
	if pattern == "" || addedLines == "" {
		return false, ""
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		// Invalid regex — treat as literal substring search
		for _, line := range strings.Split(addedLines, "\n") {
			if strings.Contains(line, pattern) {
				snippet := line
				if len(snippet) > 120 {
					snippet = snippet[:120] + "..."
				}
				return true, snippet
			}
		}
		return false, ""
	}

	for _, line := range strings.Split(addedLines, "\n") {
		if re.MatchString(line) {
			snippet := line
			if len(snippet) > 120 {
				snippet = snippet[:120] + "..."
			}
			return true, snippet
		}
	}
	return false, ""
}

// firstLine is defined in context_writer.go — reused here.
