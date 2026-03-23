package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

type ContextWriter struct {
	episodic domain.EpisodicRepo
	semantic domain.SemanticRepo
	project  *ProjectService
	logger   *slog.Logger
}

func NewContextWriter(
	episodic domain.EpisodicRepo,
	semantic domain.SemanticRepo,
	project *ProjectService,
	logger *slog.Logger,
) *ContextWriter {
	return &ContextWriter{
		episodic: episodic,
		semantic: semantic,
		project:  project,
		logger:   logger,
	}
}

// WriteAll generates .cursor/rules/mos_context.mdc for each project that has root_path.
// Called after consolidation and on startup.
func (cw *ContextWriter) WriteAll(ctx context.Context) {
	projects, err := cw.project.List(ctx)
	if err != nil {
		cw.logger.Warn("context writer: failed to list projects", "error", err)
		return
	}

	for _, p := range projects {
		if p.RootPath == "" {
			continue
		}
		if err := cw.WriteProject(ctx, p); err != nil {
			cw.logger.Warn("context writer: failed for project",
				"slug", p.Slug, "error", err)
		}
	}
}

func (cw *ContextWriter) WriteProject(ctx context.Context, p *domain.Project) error {
	rulesDir := filepath.Join(p.RootPath, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return fmt.Errorf("mkdir rules: %w", err)
	}

	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var queryErrors []string

	projectSemantic, err := cw.semantic.ListByProject(queryCtx, &p.ID, 20)
	if err != nil {
		cw.logger.Warn("context writer: ListByProject failed", "project", p.Slug, "error", err)
		queryErrors = append(queryErrors, fmt.Sprintf("ListByProject: %v", err))
	}
	globalSemantic, err := cw.semantic.ListGlobal(queryCtx, 10)
	if err != nil {
		cw.logger.Warn("context writer: ListGlobal failed", "project", p.Slug, "error", err)
		queryErrors = append(queryErrors, fmt.Sprintf("ListGlobal: %v", err))
	}
	recentEpisodes, err := cw.episodic.ListUnconsolidated(queryCtx, &p.ID, 15)
	if err != nil {
		cw.logger.Warn("context writer: ListUnconsolidated failed", "project", p.Slug, "error", err)
		queryErrors = append(queryErrors, fmt.Sprintf("ListUnconsolidated: %v", err))
	}

	epCount, err := cw.episodic.Count(queryCtx, &p.ID)
	if err != nil {
		cw.logger.Warn("context writer: episodic Count failed", "project", p.Slug, "error", err)
		queryErrors = append(queryErrors, fmt.Sprintf("episodic Count: %v", err))
	}
	semCount, err := cw.semantic.Count(queryCtx, &p.ID)
	if err != nil {
		cw.logger.Warn("context writer: semantic Count failed", "project", p.Slug, "error", err)
		queryErrors = append(queryErrors, fmt.Sprintf("semantic Count: %v", err))
	}

	var warnings []*domain.EpisodicMemory
	var decisions []*domain.EpisodicMemory
	var recent []*domain.EpisodicMemory

	for _, ep := range recentEpisodes {
		tagged := false
		for _, tag := range ep.Tags {
			switch tag {
			case "bugfix", "error", "warning", "gotcha", "pattern":
				warnings = append(warnings, ep)
				tagged = true
			case "decision", "architecture":
				decisions = append(decisions, ep)
				tagged = true
			}
			if tagged {
				break
			}
		}
		if !tagged {
			recent = append(recent, ep)
		}
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("description: Auto-generated live context from Hippocampus MOS\n")
	b.WriteString("globs: [\"**/*\"]\n")
	b.WriteString("alwaysApply: true\n")
	b.WriteString("---\n\n")
	b.WriteString(fmt.Sprintf("# %s (updated %s)\n\n", p.DisplayName, time.Now().Format("2006-01-02 15:04")))

	b.WriteString(fmt.Sprintf("Memory: %d episodic, %d semantic | ", epCount, semCount))
	b.WriteString(fmt.Sprintf("Use `mos_recall` for deep search, `mos_remember` to store, `mos_session_end` at end\n\n"))

	if len(warnings) > 0 {
		b.WriteString("## DO NOT REPEAT THESE MISTAKES\n\n")
		for _, ep := range warnings {
			content := firstLine(ep.Content)
			if len(content) > 300 {
				content = content[:300] + "..."
			}
			b.WriteString(fmt.Sprintf("- %s\n", content))
		}
		b.WriteString("\n")
	}

	if len(decisions) > 0 {
		b.WriteString("## Active Decisions\n\n")
		for _, ep := range decisions {
			content := firstLine(ep.Content)
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			b.WriteString(fmt.Sprintf("- %s\n", content))
		}
		b.WriteString("\n")
	}

	if len(projectSemantic) > 0 || len(globalSemantic) > 0 {
		b.WriteString("## Knowledge\n\n")
		seen := make(map[string]bool)
		for _, s := range projectSemantic {
			line := firstLine(s.Content)
			key := dedupKey(s.Content)
			if seen[key] || line == "" {
				continue
			}
			seen[key] = true
			b.WriteString(fmt.Sprintf("- %s\n", line))
		}
		for _, s := range globalSemantic {
			line := firstLine(s.Content)
			key := dedupKey(s.Content)
			if seen[key] || line == "" {
				continue
			}
			seen[key] = true
			b.WriteString(fmt.Sprintf("- [global] %s\n", line))
		}
		b.WriteString("\n")
	}

	if len(recent) > 0 {
		b.WriteString("## Recent\n\n")
		max := 5
		if len(recent) < max {
			max = len(recent)
		}
		for _, ep := range recent[:max] {
			age := time.Since(ep.CreatedAt).Round(time.Hour)
			summary := firstLine(ep.Content)
			if len(summary) > 150 {
				summary = summary[:150] + "..."
			}
			b.WriteString(fmt.Sprintf("- (%s ago) %s\n", formatDuration(age), summary))
		}
		b.WriteString("\n")
	}

	// Write to all detected environments
	envs := DetectEnvironments(p.RootPath)
	var written []string

	for _, env := range envs.Environments {
		switch env {
		case EnvCursor:
			outPath := filepath.Join(rulesDir, "mos_context.mdc")
			if err := os.WriteFile(outPath, []byte(b.String()), 0644); err != nil {
				cw.logger.Warn("failed to write Cursor context", "error", err)
			} else {
				written = append(written, outPath)
			}
		case EnvClaudeCode:
			outPath := filepath.Join(p.RootPath, "CLAUDE.md")
			claudeContent := convertMDCToMarkdown(b.String())
			if err := writeWithMarkers(outPath, claudeContent); err != nil {
				cw.logger.Warn("failed to write Claude Code context", "error", err)
			} else {
				written = append(written, outPath)
			}
		case EnvVSCode:
			vscodeDir := filepath.Join(p.RootPath, ".vscode")
			_ = os.MkdirAll(vscodeDir, 0755)
			outPath := filepath.Join(vscodeDir, "mos_context.md")
			mdContent := convertMDCToMarkdown(b.String())
			if err := os.WriteFile(outPath, []byte(mdContent), 0644); err != nil {
				cw.logger.Warn("failed to write VS Code context", "error", err)
			} else {
				written = append(written, outPath)
			}
		case EnvWindsurf:
			windsurfDir := filepath.Join(p.RootPath, ".windsurf", "rules")
			_ = os.MkdirAll(windsurfDir, 0755)
			outPath := filepath.Join(windsurfDir, "mos_context.md")
			mdContent := convertMDCToMarkdown(b.String())
			if err := os.WriteFile(outPath, []byte(mdContent), 0644); err != nil {
				cw.logger.Warn("failed to write Windsurf context", "error", err)
			} else {
				written = append(written, outPath)
			}
		default:
			outPath := filepath.Join(rulesDir, "mos_context.mdc")
			if err := os.WriteFile(outPath, []byte(b.String()), 0644); err != nil {
				cw.logger.Warn("failed to write default context", "error", err)
			} else {
				written = append(written, outPath)
			}
		}
	}

	cw.logger.Info("context files updated",
		"project", p.Slug,
		"targets", written,
		"warnings", len(warnings),
		"decisions", len(decisions),
		"semantic", len(projectSemantic)+len(globalSemantic),
	)
	if len(queryErrors) > 0 {
		return fmt.Errorf("context written with %d query failures: %s", len(queryErrors), strings.Join(queryErrors, "; "))
	}
	return nil
}

const (
	markerBegin = "<!-- HIPPOCAMPUS:BEGIN -->"
	markerEnd   = "<!-- HIPPOCAMPUS:END -->"
)

// writeWithMarkers replaces only the auto-generated section between markers,
// preserving any hand-written content in the file.
// If the file doesn't exist or has no markers, the auto-generated block is appended.
func writeWithMarkers(path string, generated string) error {
	block := markerBegin + "\n" + generated + "\n" + markerEnd

	existing, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist — write marker block as the entire file.
		return os.WriteFile(path, []byte(block+"\n"), 0644)
	}

	content := string(existing)
	beginIdx := strings.Index(content, markerBegin)
	endIdx := strings.Index(content, markerEnd)

	if beginIdx < 0 || endIdx < 0 || endIdx < beginIdx {
		// Markers not found — check if file has only old auto-generated content
		// (starts with "# " and contains "Auto-generated" or "Hippocampus MOS").
		// In that case, replace the whole file to migrate from old format.
		trimmed := strings.TrimSpace(content)
		if strings.Contains(trimmed, "Auto-generated") || strings.Contains(trimmed, "Hippocampus MOS — Learned") {
			return os.WriteFile(path, []byte(block+"\n"), 0644)
		}
		// Has user content but no markers — append the block at the end.
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n" + block + "\n"
		return os.WriteFile(path, []byte(content), 0644)
	}

	// Replace everything between markers (inclusive).
	newContent := content[:beginIdx] + block + content[endIdx+len(markerEnd):]
	return os.WriteFile(path, []byte(newContent), 0644)
}

// convertMDCToMarkdown strips .mdc YAML frontmatter and returns clean markdown.
func convertMDCToMarkdown(mdcContent string) string {
	if !strings.HasPrefix(mdcContent, "---\n") {
		return mdcContent
	}
	end := strings.Index(mdcContent[4:], "---")
	if end < 0 {
		return mdcContent
	}
	return strings.TrimSpace(mdcContent[4+end+3:])
}

// dedupKey returns a normalized key for deduplication.
// Uses first 100 chars of trimmed+lowercased content to catch near-duplicates
// that differ only in casing or trailing whitespace.
func dedupKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx > 0 {
		return s[:idx]
	}
	return s
}

func formatDuration(d time.Duration) string {
	if d < time.Hour {
		return "just now"
	}
	hours := int(d.Hours())
	if hours < 24 {
		return fmt.Sprintf("%dh", hours)
	}
	days := hours / 24
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}
