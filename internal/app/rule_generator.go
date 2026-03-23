package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

type RuleGenerator struct {
	episodic  domain.EpisodicRepo
	semantic  domain.SemanticRepo
	embedding domain.EmbeddingProvider
	project   *ProjectService
	logger    *slog.Logger
}

func NewRuleGenerator(
	episodic domain.EpisodicRepo,
	semantic domain.SemanticRepo,
	embedding domain.EmbeddingProvider,
	project *ProjectService,
	logger *slog.Logger,
) *RuleGenerator {
	return &RuleGenerator{
		episodic:  episodic,
		semantic:  semantic,
		embedding: embedding,
		project:   project,
		logger:    logger,
	}
}

type ruleCluster struct {
	topic    string
	memories []*domain.EpisodicMemory
	globs    []string
}

// GenerateAll scans all projects for pattern clusters and creates .mdc rules.
func (rg *RuleGenerator) GenerateAll(ctx context.Context) {
	projects, err := rg.project.List(ctx)
	if err != nil {
		rg.logger.Warn("rule generator: list projects failed", "error", err)
		return
	}

	for _, p := range projects {
		if p.RootPath == "" {
			continue
		}
		rg.GenerateForProject(ctx, p)
	}
}

func (rg *RuleGenerator) GenerateForProject(ctx context.Context, p *domain.Project) {
	errorTags := []string{"error", "bugfix", "gotcha", "warning", "pattern", "learned_pattern"}
	memories, err := rg.episodic.ListByTags(ctx, &p.ID, errorTags, 100)
	if err != nil {
		rg.logger.Warn("rule generator: list by tags failed", "project", p.Slug, "error", err)
		return
	}

	if len(memories) < 3 {
		return
	}

	clusters := rg.clusterByTopic(memories)

	rulesDir := filepath.Join(p.RootPath, ".cursor", "rules")
	claudeDir := filepath.Join(p.RootPath, ".claude")

	for _, cl := range clusters {
		if len(cl.memories) < 2 {
			continue
		}

		content := rg.formatRule(cl, p)
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))[:8]
		filename := fmt.Sprintf("mos_learned_%s.mdc", sanitizeFilename(cl.topic))

		cursorPath := filepath.Join(rulesDir, filename)
		if err := os.MkdirAll(rulesDir, 0755); err == nil {
			if shouldUpdate(cursorPath, hash) {
				os.WriteFile(cursorPath, []byte(content), 0644)
				rg.logger.Info("rule generated",
					"project", p.Slug,
					"topic", cl.topic,
					"patterns", len(cl.memories),
					"path", cursorPath,
				)
			}
		}

		claudeContent := rg.formatClaudeRule(cl, p)
		claudePath := filepath.Join(claudeDir, filename)
		if err := os.MkdirAll(claudeDir, 0755); err == nil {
			if shouldUpdate(claudePath, hash) {
				os.WriteFile(claudePath, []byte(claudeContent), 0644)
			}
		}
	}

	rg.generateClaudeMD(ctx, p, clusters)
}

func (rg *RuleGenerator) clusterByTopic(memories []*domain.EpisodicMemory) []*ruleCluster {
	topicMap := make(map[string]*ruleCluster)

	for _, m := range memories {
		topic := extractTopic(m)
		if topic == "" {
			topic = "general"
		}

		cl, ok := topicMap[topic]
		if !ok {
			cl = &ruleCluster{topic: topic}
			topicMap[topic] = cl
		}
		cl.memories = append(cl.memories, m)

		for _, tag := range m.Tags {
			if strings.HasPrefix(tag, "file:") {
				glob := inferGlob(strings.TrimPrefix(tag, "file:"))
				if glob != "" {
					cl.globs = appendUnique(cl.globs, glob)
				}
			}
		}
	}

	var clusters []*ruleCluster
	for _, cl := range topicMap {
		clusters = append(clusters, cl)
	}
	sort.Slice(clusters, func(i, j int) bool {
		return len(clusters[i].memories) > len(clusters[j].memories)
	})
	return clusters
}

func extractTopic(m *domain.EpisodicMemory) string {
	for _, tag := range m.Tags {
		switch {
		case tag == "error" || tag == "bugfix" || tag == "gotcha" ||
			tag == "warning" || tag == "pattern" || tag == "learned_pattern" ||
			tag == "session_summary" || tag == "auto" ||
			tag == "critical" || tag == "release" || tag == "quality":
			continue
		case strings.HasPrefix(tag, "file:"):
			continue
		default:
			return tag
		}
	}

	content := strings.ToLower(m.Content)
	keywords := map[string]string{
		"database":    "database",
		"migration":   "database",
		"sql":         "database",
		"timescale":   "database",
		"pgx":         "database",
		"mcp":         "mcp-protocol",
		"stdio":       "mcp-protocol",
		"json-rpc":    "mcp-protocol",
		"deploy":      "deployment",
		"systemctl":   "deployment",
		"docker":      "deployment",
		"embed":       "embeddings",
		"ollama":      "embeddings",
		"vector":      "embeddings",
		"test":        "testing",
		"import":      "go-imports",
		"compilation": "go-compilation",
		"undefined":   "go-compilation",
		"build":       "go-compilation",
		"api":         "api-design",
		"rest":        "api-design",
		"handler":     "api-design",
	}

	for keyword, topic := range keywords {
		if strings.Contains(content, keyword) {
			return topic
		}
	}
	return "general"
}

func (rg *RuleGenerator) formatRule(cl *ruleCluster, p *domain.Project) string {
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("description: \"Auto-learned patterns: %s (from %d incidents)\"\n", cl.topic, len(cl.memories)))
	if len(cl.globs) > 0 {
		b.WriteString(fmt.Sprintf("globs: [\"%s\"]\n", strings.Join(cl.globs, "\", \"")))
	} else {
		b.WriteString("globs: [\"**/*\"]\n")
	}
	b.WriteString("alwaysApply: false\n")
	b.WriteString("---\n\n")

	b.WriteString(fmt.Sprintf("# Learned Patterns: %s\n\n", strings.Title(cl.topic)))
	b.WriteString(fmt.Sprintf("*Auto-generated by Hippocampus MOS from %d error patterns in %s.*\n",
		len(cl.memories), p.DisplayName))
	b.WriteString(fmt.Sprintf("*Last updated: %s*\n\n", time.Now().Format("2006-01-02 15:04")))

	for i, m := range cl.memories {
		lines := strings.Split(strings.TrimSpace(m.Content), "\n")
		b.WriteString(fmt.Sprintf("## Pattern %d\n\n", i+1))
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "ERROR:") || strings.HasPrefix(line, "BUG:") ||
				strings.HasPrefix(line, "GOTCHA:") {
				b.WriteString(fmt.Sprintf("**%s**\n\n", line))
			} else if strings.HasPrefix(line, "ROOT CAUSE:") || strings.HasPrefix(line, "CAUSE:") {
				b.WriteString(fmt.Sprintf("- Why: %s\n", strings.TrimPrefix(strings.TrimPrefix(line, "ROOT CAUSE:"), "CAUSE:")))
			} else if strings.HasPrefix(line, "FIX:") {
				b.WriteString(fmt.Sprintf("- Fix: %s\n", strings.TrimPrefix(line, "FIX:")))
			} else if strings.HasPrefix(line, "PREVENTION:") || strings.HasPrefix(line, "DO:") {
				b.WriteString(fmt.Sprintf("- Prevention: %s\n", strings.TrimPrefix(strings.TrimPrefix(line, "PREVENTION:"), "DO:")))
			} else if strings.HasPrefix(line, "DON'T:") {
				b.WriteString(fmt.Sprintf("- Avoid: %s\n", strings.TrimPrefix(line, "DON'T:")))
			} else if strings.HasPrefix(line, "FILE:") {
				b.WriteString(fmt.Sprintf("- File: `%s`\n", strings.TrimSpace(strings.TrimPrefix(line, "FILE:"))))
			} else if strings.HasPrefix(line, "CONTEXT:") {
				b.WriteString(fmt.Sprintf("- Context: %s\n", strings.TrimPrefix(line, "CONTEXT:")))
			} else {
				b.WriteString(fmt.Sprintf("%s\n", line))
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (rg *RuleGenerator) formatClaudeRule(cl *ruleCluster, p *domain.Project) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Learned Patterns: %s\n\n", strings.Title(cl.topic)))
	b.WriteString(fmt.Sprintf("Auto-generated from %d incidents in %s.\n\n", len(cl.memories), p.DisplayName))

	for i, m := range cl.memories {
		b.WriteString(fmt.Sprintf("## %d. ", i+1))
		lines := strings.Split(strings.TrimSpace(m.Content), "\n")
		for _, line := range lines {
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// generateClaudeMD writes learned patterns to CLAUDE.md using markers,
// preserving any hand-written content outside the marker block.
func (rg *RuleGenerator) generateClaudeMD(ctx context.Context, p *domain.Project, clusters []*ruleCluster) {
	claudeMD := filepath.Join(p.RootPath, "CLAUDE.md")

	var totalPatterns int
	for _, cl := range clusters {
		if len(cl.memories) >= 2 {
			totalPatterns += len(cl.memories)
		}
	}
	if totalPatterns == 0 {
		return
	}

	var b strings.Builder
	b.WriteString("# Hippocampus MOS — Learned Knowledge\n\n")
	b.WriteString(fmt.Sprintf("*Auto-generated: %s | %d patterns from %d topics*\n\n",
		time.Now().Format("2006-01-02 15:04"), totalPatterns, len(clusters)))

	epCount, _ := rg.episodic.Count(ctx, &p.ID)
	semCount, _ := rg.semantic.Count(ctx, &p.ID)
	b.WriteString(fmt.Sprintf("Memory: %d episodic, %d semantic\n\n", epCount, semCount))

	b.WriteString("## Critical Patterns (DO NOT REPEAT)\n\n")
	for _, cl := range clusters {
		if len(cl.memories) < 2 {
			continue
		}
		b.WriteString(fmt.Sprintf("### %s (%d patterns)\n\n", strings.Title(cl.topic), len(cl.memories)))
		for _, m := range cl.memories {
			first := firstLine(m.Content)
			if len(first) > 200 {
				first = first[:200] + "..."
			}
			b.WriteString(fmt.Sprintf("- %s\n", first))
		}
		b.WriteString("\n")
	}

	b.WriteString("## MCP Integration\n\n")
	b.WriteString("This project uses Hippocampus MOS for persistent memory.\n")
	b.WriteString("- `mos_init` — call at session start with workspace path\n")
	b.WriteString("- `mos_learn_error` — call on ANY error\n")
	b.WriteString("- `mos_recall` — deep search across all memories\n")
	b.WriteString("- `mos_remember` — store decisions, patterns, gotchas\n")
	b.WriteString("- `mos_session_end` — call at session end with summary\n")

	if err := writeWithMarkers(claudeMD, b.String()); err != nil {
		rg.logger.Warn("failed to write CLAUDE.md", "project", p.Slug, "error", err)
		return
	}
	rg.logger.Info("CLAUDE.md updated", "project", p.Slug, "patterns", totalPatterns)
}

func sanitizeFilename(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	var result []byte
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			result = append(result, c)
		}
	}
	if len(result) > 40 {
		result = result[:40]
	}
	return string(result)
}

func inferGlob(filePath string) string {
	ext := filepath.Ext(filePath)
	if ext == "" {
		return ""
	}
	dir := filepath.Dir(filePath)
	if dir == "." || dir == "" {
		return "**/*" + ext
	}
	return filepath.ToSlash(dir) + "/**/*" + ext
}

func appendUnique(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}

func shouldUpdate(path string, newHash string) bool {
	existing, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	existingHash := fmt.Sprintf("%x", sha256.Sum256(existing))[:8]
	return existingHash != newHash
}
