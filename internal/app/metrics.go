package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

type MetricsService struct {
	episodic domain.EpisodicRepo
	semantic domain.SemanticRepo
	logger   *slog.Logger

	mu      sync.Mutex
	recalls int64
	hits    int64
	misses  int64
	errors  int64
}

func NewMetricsService(
	episodic domain.EpisodicRepo,
	semantic domain.SemanticRepo,
	logger *slog.Logger,
) *MetricsService {
	return &MetricsService{
		episodic: episodic,
		semantic: semantic,
		logger:   logger,
	}
}

func (m *MetricsService) RecordRecall(hit bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recalls++
	if hit {
		m.hits++
	} else {
		m.misses++
	}
}

func (m *MetricsService) RecordError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors++
}

type LearningReport struct {
	TotalRecalls      int64              `json:"total_recalls"`
	RecallHits        int64              `json:"recall_hits"`
	RecallMisses      int64              `json:"recall_misses"`
	HitRate           float64            `json:"hit_rate"`
	ErrorsLearned     int64              `json:"errors_learned"`
	MemoryBreakdown   map[string]int     `json:"memory_breakdown"`
	KnowledgeCoverage map[string]int     `json:"knowledge_coverage"`
	TopPatterns       []PatternStat      `json:"top_patterns"`
	LearningVelocity  float64            `json:"learning_velocity"`
	ProjectStats      []ProjectLearnStat `json:"project_stats"`
	GeneratedRules    int                `json:"generated_rules"`
	Timestamp         time.Time          `json:"timestamp"`
}

type PatternStat struct {
	Topic string `json:"topic"`
	Count int    `json:"count"`
}

type ProjectLearnStat struct {
	Slug      string `json:"slug"`
	Episodic  int    `json:"episodic"`
	Semantic  int    `json:"semantic"`
	Errors    int    `json:"errors_learned"`
	CreatedAt string `json:"created_at"`
}

func (m *MetricsService) Report(ctx context.Context, projects []*domain.Project) *LearningReport {
	m.mu.Lock()
	report := &LearningReport{
		TotalRecalls:      m.recalls,
		RecallHits:        m.hits,
		RecallMisses:      m.misses,
		ErrorsLearned:     m.errors,
		MemoryBreakdown:   make(map[string]int),
		KnowledgeCoverage: make(map[string]int),
		Timestamp:         time.Now(),
	}
	if m.recalls > 0 {
		report.HitRate = float64(m.hits) / float64(m.recalls)
	}
	m.mu.Unlock()

	for _, p := range projects {
		epCount, _ := m.episodic.Count(ctx, &p.ID)
		semCount, _ := m.semantic.Count(ctx, &p.ID)

		errorTags := []string{"error", "bugfix", "gotcha", "learned_pattern"}
		errorMems, _ := m.episodic.ListByTags(ctx, &p.ID, errorTags, 100)

		stat := ProjectLearnStat{
			Slug:      p.Slug,
			Episodic:  epCount,
			Semantic:  semCount,
			Errors:    len(errorMems),
			CreatedAt: p.CreatedAt.Format("2006-01-02"),
		}
		report.ProjectStats = append(report.ProjectStats, stat)

		report.MemoryBreakdown["episodic"] += epCount
		report.MemoryBreakdown["semantic"] += semCount

		for _, em := range errorMems {
			topic := extractTopic(em)
			report.KnowledgeCoverage[topic]++
		}
	}

	for topic, count := range report.KnowledgeCoverage {
		report.TopPatterns = append(report.TopPatterns, PatternStat{
			Topic: topic,
			Count: count,
		})
	}

	totalMem := report.MemoryBreakdown["episodic"] + report.MemoryBreakdown["semantic"]
	if totalMem > 0 {
		hours := time.Since(oldestProject(projects)).Hours()
		if hours > 0 {
			report.LearningVelocity = float64(totalMem) / hours * 24
		}
	}

	report.GeneratedRules = countGeneratedRules(projects)

	return report
}

func oldestProject(projects []*domain.Project) time.Time {
	oldest := time.Now()
	for _, p := range projects {
		if p.CreatedAt.Before(oldest) {
			oldest = p.CreatedAt
		}
	}
	return oldest
}

func countGeneratedRules(projects []*domain.Project) int {
	count := 0
	for _, p := range projects {
		if p.RootPath == "" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(p.RootPath, ".cursor", "rules", "mos_learned_*.mdc"))
		count += len(matches)
	}
	return count
}

// --- NEW REST + MCP endpoint ---

func (m *MetricsService) FormatText(ctx context.Context, projects []*domain.Project) string {
	r := m.Report(ctx, projects)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Hippocampus MOS — Learning Report\n"))
	b.WriteString(fmt.Sprintf("Generated: %s\n\n", r.Timestamp.Format("2006-01-02 15:04")))

	b.WriteString(fmt.Sprintf("Memory: %d episodic, %d semantic\n",
		r.MemoryBreakdown["episodic"], r.MemoryBreakdown["semantic"]))
	b.WriteString(fmt.Sprintf("Recall: %d total, %.0f%% hit rate\n", r.TotalRecalls, r.HitRate*100))
	b.WriteString(fmt.Sprintf("Errors learned: %d\n", r.ErrorsLearned))
	b.WriteString(fmt.Sprintf("Auto-generated rules: %d\n", r.GeneratedRules))
	b.WriteString(fmt.Sprintf("Learning velocity: %.1f memories/day\n\n", r.LearningVelocity))

	if len(r.TopPatterns) > 0 {
		b.WriteString("Knowledge coverage:\n")
		for _, p := range r.TopPatterns {
			b.WriteString(fmt.Sprintf("  %s: %d patterns\n", p.Topic, p.Count))
		}
		b.WriteString("\n")
	}

	if len(r.ProjectStats) > 0 {
		b.WriteString("Projects:\n")
		for _, ps := range r.ProjectStats {
			b.WriteString(fmt.Sprintf("  %s: %d ep, %d sem, %d errors (since %s)\n",
				ps.Slug, ps.Episodic, ps.Semantic, ps.Errors, ps.CreatedAt))
		}
	}

	return b.String()
}
