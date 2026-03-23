package app

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/pkg/vecutil"
)

type CachedRule struct {
	ID           uuid.UUID
	Content      string
	Embedding    []float32
	FilePaths    []string
	WhenKeywords []string
	AntiPattern  string
	WhenText     string
	WatchText    string
	DoText       string
}

type MatchSignals struct {
	FilePath    string
	Query       string
	QueryEmb    []float32
	CodeSnippet string
	ProjectID   *uuid.UUID
}

type MatchedWarning struct {
	Rule       CachedRule
	Signal     string  // "file_match", "keyword_match", "embedding_match"
	Confidence float64 // 0.0-1.0
}

type WarningMatcher struct {
	semantic  domain.SemanticRepo
	embedding domain.EmbeddingProvider
	logger    *slog.Logger

	mu    sync.RWMutex
	cache []CachedRule
}

func NewWarningMatcher(semantic domain.SemanticRepo, embedding domain.EmbeddingProvider, logger *slog.Logger) *WarningMatcher {
	return &WarningMatcher{
		semantic:  semantic,
		embedding: embedding,
		logger:    logger,
	}
}

func (wm *WarningMatcher) LoadRules(ctx context.Context, projectID *uuid.UUID) {
	rules, err := wm.semantic.ListByEntityType(ctx, projectID, "rule", 100)
	if err != nil {
		wm.logger.Warn("warning matcher: failed to load rules", "error", err)
		return
	}

	cached := make([]CachedRule, 0, len(rules))
	for _, r := range rules {
		cr := CachedRule{
			ID:        r.ID,
			Content:   r.Content,
			Embedding: r.Embedding,
		}

		if r.Metadata != nil {
			if v, ok := r.Metadata["rule_when"].(string); ok {
				cr.WhenText = v
				cr.WhenKeywords = extractKeywords(v)
			}
			if v, ok := r.Metadata["rule_watch"].(string); ok {
				cr.WatchText = v
			}
			if v, ok := r.Metadata["rule_do"].(string); ok {
				cr.DoText = v
			}
			if v, ok := r.Metadata["rule_antipattern"].(string); ok {
				cr.AntiPattern = v
			}
			if v, ok := r.Metadata["rule_files"].([]interface{}); ok {
				for _, f := range v {
					if s, ok := f.(string); ok {
						cr.FilePaths = append(cr.FilePaths, s)
					}
				}
			}
			if v, ok := r.Metadata["rule_files"].([]string); ok {
				cr.FilePaths = v
			}
		}

		// Fallback: extract file paths from tags
		if len(cr.FilePaths) == 0 {
			for _, tag := range r.Tags {
				if strings.HasPrefix(tag, "file:") {
					cr.FilePaths = append(cr.FilePaths, strings.TrimPrefix(tag, "file:"))
				}
			}
		}

		// Fallback: extract when keywords from content
		if len(cr.WhenKeywords) == 0 && cr.Content != "" {
			for _, line := range strings.Split(cr.Content, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "WHEN:") {
					cr.WhenText = strings.TrimSpace(strings.TrimPrefix(line, "WHEN:"))
					cr.WhenKeywords = extractKeywords(cr.WhenText)
					break
				}
			}
		}

		cached = append(cached, cr)
	}

	wm.mu.Lock()
	wm.cache = cached
	wm.mu.Unlock()

	wm.logger.Debug("warning matcher: rules loaded", "count", len(cached))
}

func (wm *WarningMatcher) Match(ctx context.Context, signals MatchSignals) []MatchedWarning {
	wm.mu.RLock()
	rules := wm.cache
	wm.mu.RUnlock()

	if len(rules) == 0 {
		return nil
	}

	seen := map[uuid.UUID]bool{}
	var matches []MatchedWarning

	// Level 1: File-path exact match (deterministic, highest confidence)
	if signals.FilePath != "" {
		normPath := normalizePath(signals.FilePath)
		for _, rule := range rules {
			for _, fp := range rule.FilePaths {
					normFP := normalizePath(fp)
				if normFP == normPath || pathSuffixMatch(normPath, normFP) {
					if !seen[rule.ID] {
						matches = append(matches, MatchedWarning{Rule: rule, Signal: "file_match", Confidence: 1.0})
						seen[rule.ID] = true
					}
					break
				}
			}
		}
	}

	// Level 2: Keyword/package match (semi-deterministic)
	if signals.Query != "" {
		queryKW := extractKeywords(signals.Query)
		if len(queryKW) > 0 {
			for _, rule := range rules {
				if seen[rule.ID] || len(rule.WhenKeywords) == 0 {
					continue
				}
				overlap := kwOverlap(queryKW, rule.WhenKeywords)
				if overlap >= 0.5 {
					matches = append(matches, MatchedWarning{Rule: rule, Signal: "keyword_match", Confidence: overlap})
					seen[rule.ID] = true
				}
			}
		}
	}

	// Level 3: Embedding similarity (fallback)
	if len(signals.QueryEmb) > 0 {
		for _, rule := range rules {
			if seen[rule.ID] || len(rule.Embedding) == 0 {
				continue
			}
			sim := vecutil.CosineSimilarity(signals.QueryEmb, rule.Embedding)
			if sim >= 0.65 {
				matches = append(matches, MatchedWarning{Rule: rule, Signal: "embedding_match", Confidence: float64(sim)})
				seen[rule.ID] = true
			}
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Confidence > matches[j].Confidence
	})

	// Confidence gap filter: drop tail significantly weaker than top match
	if len(matches) > 1 {
		topConf := matches[0].Confidence
		cutoff := len(matches)
		for i := 1; i < len(matches); i++ {
			if matches[i].Confidence < topConf*0.6 && topConf-matches[i].Confidence > 0.25 {
				cutoff = i
				break
			}
		}
		matches = matches[:cutoff]
	}

	if len(matches) > 3 {
		matches = matches[:3]
	}

	return matches
}

func (wm *WarningMatcher) RuleCount() int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	return len(wm.cache)
}

func kwOverlap(queryKW, ruleKW []string) float64 {
	if len(ruleKW) == 0 {
		return 0
	}
	matched := 0
	for _, qk := range queryKW {
		for _, rk := range ruleKW {
			if stemmedContains(rk, qk) || stemmedContains(qk, rk) {
				matched++
				break
			}
		}
	}
	return float64(matched) / float64(len(ruleKW))
}

func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	return strings.ToLower(p)
}
