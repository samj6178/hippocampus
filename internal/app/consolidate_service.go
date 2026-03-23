package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/metrics"
	"github.com/hippocampus-mcp/hippocampus/internal/pkg/vecutil"
)

// ProjectLister provides the list of projects for consolidation.
// Defined here (consumer-side) to avoid depending on ProjectService.
type ProjectLister interface {
	List(ctx context.Context) ([]*domain.Project, error)
}

type ConsolidateService struct {
	episodic  domain.EpisodicRepo
	semantic  domain.SemanticRepo
	embedding domain.EmbeddingProvider
	projects  ProjectLister
	llm       domain.LLMProvider
	logger    *slog.Logger

	clusterThreshold float64
	minClusterSize   int
}

type ConsolidateConfig struct {
	ClusterThreshold float64 // cosine similarity threshold for merging (default 0.75)
	MinClusterSize   int     // minimum episodes to form a semantic fact (default 2)
}

func NewConsolidateService(
	episodic domain.EpisodicRepo,
	semantic domain.SemanticRepo,
	embedding domain.EmbeddingProvider,
	projects ProjectLister,
	cfg ConsolidateConfig,
	logger *slog.Logger,
) *ConsolidateService {
	if cfg.ClusterThreshold <= 0 {
		cfg.ClusterThreshold = 0.72
	}
	if cfg.MinClusterSize <= 0 {
		cfg.MinClusterSize = 2
	}
	return &ConsolidateService{
		episodic:         episodic,
		semantic:         semantic,
		embedding:        embedding,
		projects:         projects,
		logger:           logger,
		clusterThreshold: cfg.ClusterThreshold,
		minClusterSize:   cfg.MinClusterSize,
	}
}

func (s *ConsolidateService) SetLLM(llm domain.LLMProvider) {
	s.llm = llm
}

type ConsolidateResult struct {
	ProjectID          *uuid.UUID `json:"project_id,omitempty"`
	EpisodesProcessed  int        `json:"episodes_processed"`
	ClustersFormed     int        `json:"clusters_formed"`
	SemanticCreated    int        `json:"semantic_created"`
	EpisodesMarked     int        `json:"episodes_marked"`
	SingletonsSkipped  int        `json:"singletons_skipped"`
}

type cluster struct {
	members    []*domain.EpisodicMemory
	embeddings [][]float32
	centroid   []float32
}

// Run executes one consolidation cycle for the given project.
// Fetches unconsolidated episodes, clusters by embedding similarity,
// promotes clusters to semantic memory, and marks sources as consolidated.
func (s *ConsolidateService) Run(ctx context.Context, projectID *uuid.UUID) (*ConsolidateResult, error) {
	episodes, err := s.episodic.ListUnconsolidated(ctx, projectID, 500)
	if err != nil {
		return nil, fmt.Errorf("list unconsolidated: %w", err)
	}

	if len(episodes) < s.minClusterSize {
		return &ConsolidateResult{
			ProjectID:         projectID,
			EpisodesProcessed: len(episodes),
		}, nil
	}

	s.logger.Info("consolidation started",
		"project_id", projectID,
		"episodes", len(episodes),
	)

	embeddings, err := s.embedEpisodes(ctx, episodes)
	if err != nil {
		return nil, fmt.Errorf("embed episodes: %w", err)
	}

	threshold := s.clusterThreshold
	if len(episodes) < 20 {
		threshold = s.clusterThreshold - 0.04
	}

	clusters := s.clusterByContentType(episodes, embeddings, threshold)

	result := &ConsolidateResult{
		ProjectID:         projectID,
		EpisodesProcessed: len(episodes),
		ClustersFormed:    len(clusters),
	}

	var allMarkedIDs []uuid.UUID
	var singletons []*domain.EpisodicMemory
	var singletonEmbs [][]float32

	for i, cl := range clusters {
		if len(cl.members) < s.minClusterSize {
			result.SingletonsSkipped += len(cl.members)
			for j, ep := range cl.members {
				singletons = append(singletons, ep)
				singletonEmbs = append(singletonEmbs, clusters[i].embeddings[j])
			}
			continue
		}

		sem, err := s.promoteCluster(ctx, cl, projectID)
		if err != nil {
			s.logger.Warn("failed to promote cluster", "error", err, "members", len(cl.members))
			continue
		}

		for _, ep := range cl.members {
			allMarkedIDs = append(allMarkedIDs, ep.ID)
		}

		result.SemanticCreated++
		s.logger.Debug("cluster promoted to semantic",
			"semantic_id", sem.ID,
			"source_count", len(cl.members),
			"entity_type", sem.EntityType,
		)
	}

	// Loose thematic clustering disabled — centroid of diverse topics
	// becomes a "universal match" that pollutes every recall.
	// Re-enable when LLM-based summarization is available (v0.5+).

	if len(allMarkedIDs) > 0 {
		if err := s.episodic.MarkConsolidated(ctx, allMarkedIDs); err != nil {
			return nil, fmt.Errorf("mark consolidated: %w", err)
		}
		result.EpisodesMarked = len(allMarkedIDs)
	}

	metrics.ConsolidationRuns.Inc()
	metrics.ConsolidationPromoted.Add(float64(result.SemanticCreated))

	// Pattern → Rule graduation: find clusters of error episodes and
	// generate rules. When 3+ similar errors exist, the system has enough
	// evidence to formulate a prevention rule automatically.
	rulesGenerated := s.graduateErrorPatterns(ctx, projectID)
	if rulesGenerated > 0 {
		s.logger.Info("error patterns graduated to rules", "rules", rulesGenerated)
	}

	s.logger.Info("consolidation completed",
		"project_id", projectID,
		"clusters", result.ClustersFormed,
		"semantic_created", result.SemanticCreated,
		"episodes_marked", result.EpisodesMarked,
		"singletons_skipped", result.SingletonsSkipped,
		"rules_generated", rulesGenerated,
	)

	return result, nil
}

// RunAll consolidates each project independently, then global memory, then runs decay.
// Project-scoped consolidation prevents cross-project clusters from polluting semantic tier.
func (s *ConsolidateService) RunAll(ctx context.Context) ([]*ConsolidateResult, error) {
	var results []*ConsolidateResult

	if s.projects != nil {
		projects, err := s.projects.List(ctx)
		if err != nil {
			s.logger.Warn("consolidation: failed to list projects", "error", err)
		} else {
			for _, p := range projects {
				pid := p.ID
				r, err := s.Run(ctx, &pid)
				if err != nil {
					s.logger.Warn("project consolidation failed", "project", p.Slug, "error", err)
					continue
				}
				if r != nil && r.EpisodesProcessed > 0 {
					results = append(results, r)
				}
			}
		}
	}

	globalResult, err := s.Run(ctx, nil)
	if err != nil {
		s.logger.Warn("global consolidation failed", "error", err)
	}
	if globalResult != nil && globalResult.EpisodesProcessed > 0 {
		results = append(results, globalResult)
	}

	s.decayOldMemories(ctx)

	return results, nil
}

// decayOldMemories reduces importance of memories not accessed in 7+ days.
// Episodic: *0.97 per cycle (floor 0.1). Semantic: *0.99 per cycle (floor 0.2).
// This implements "use it or lose it" — ensures old irrelevant memories fade.
func (s *ConsolidateService) decayOldMemories(ctx context.Context) {
	epDecayed, err := s.episodic.DecayImportance(ctx, 7*24*time.Hour, 0.97, 0.1)
	if err != nil {
		s.logger.Warn("episodic decay failed", "error", err)
	}
	semDecayed, err := s.semantic.DecayImportance(ctx, 14*24*time.Hour, 0.99, 0.2)
	if err != nil {
		s.logger.Warn("semantic decay failed", "error", err)
	}
	if epDecayed > 0 || semDecayed > 0 {
		s.logger.Info("memory decay applied",
			"episodic_decayed", epDecayed,
			"semantic_decayed", semDecayed,
		)
	}
}

func (s *ConsolidateService) embedEpisodes(ctx context.Context, episodes []*domain.EpisodicMemory) ([][]float32, error) {
	result := make([][]float32, len(episodes))
	var missingIdx []int
	for i, ep := range episodes {
		if len(ep.Embedding) > 0 {
			result[i] = ep.Embedding
		} else {
			missingIdx = append(missingIdx, i)
		}
	}
	if len(missingIdx) == 0 {
		return result, nil
	}
	texts := make([]string, len(missingIdx))
	for i, idx := range missingIdx {
		texts[i] = episodes[idx].Content
	}
	embedded, err := s.embedding.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	for i, idx := range missingIdx {
		result[idx] = embedded[i]
	}
	return result, nil
}

// clusterByContentType groups episodes by their inferred content type (error, decision,
// code_change, session, etc.) before running similarity clustering within each group.
// This prevents unrelated content types from polluting each other's clusters.
func (s *ConsolidateService) clusterByContentType(episodes []*domain.EpisodicMemory, embeddings [][]float32, threshold float64) []*cluster {
	groups := make(map[string][]int) // content_type -> indices
	for i, ep := range episodes {
		ct := classifyContentType(ep.Content, ep.Tags)
		groups[ct] = append(groups[ct], i)
	}

	var allClusters []*cluster
	for _, indices := range groups {
		subEps := make([]*domain.EpisodicMemory, len(indices))
		subEmbs := make([][]float32, len(indices))
		for j, idx := range indices {
			subEps[j] = episodes[idx]
			subEmbs[j] = embeddings[idx]
		}
		cls := s.clusterWithThreshold(subEps, subEmbs, threshold)
		allClusters = append(allClusters, cls...)
	}
	return allClusters
}

func classifyContentType(content string, tags []string) string {
	lower := strings.ToLower(content)
	tagSet := make(map[string]bool)
	for _, t := range tags {
		tagSet[strings.ToLower(t)] = true
	}

	if strings.HasPrefix(lower, "error:") || strings.HasPrefix(lower, "bug:") ||
		tagSet["error"] || tagSet["bugfix"] || tagSet["fix"] ||
		strings.Contains(lower, "root cause:") || strings.Contains(lower, "sqlstate") {
		return "error"
	}
	if strings.HasPrefix(lower, "decision:") || tagSet["decision"] || tagSet["architecture"] ||
		strings.Contains(lower, "decided") || strings.Contains(lower, "chose") {
		return "decision"
	}
	if strings.HasPrefix(lower, "code change:") || strings.HasPrefix(lower, "code file:") ||
		tagSet["code"] || tagSet["code_change"] {
		return "code"
	}
	if strings.HasPrefix(lower, "session summary:") || tagSet["session"] {
		return "session"
	}
	if strings.HasPrefix(lower, "gotcha:") || strings.HasPrefix(lower, "performance") ||
		tagSet["gotcha"] || tagSet["performance"] {
		return "pattern"
	}
	if strings.HasPrefix(lower, "project doc:") || tagSet["project_knowledge"] {
		return "documentation"
	}
	if strings.HasPrefix(lower, "prediction error") || tagSet["prediction_error"] {
		return "prediction"
	}
	return "general"
}

func (s *ConsolidateService) greedyClusterAdaptive(episodes []*domain.EpisodicMemory, embeddings [][]float32, threshold float64) []*cluster {
	return s.clusterWithThreshold(episodes, embeddings, threshold)
}

// greedyCluster assigns each episode to the most similar existing cluster
// (if similarity > threshold), or creates a new cluster.
// O(n*k) where k = number of clusters.
func (s *ConsolidateService) greedyCluster(episodes []*domain.EpisodicMemory, embeddings [][]float32) []*cluster {
	var clusters []*cluster

	for i, ep := range episodes {
		emb := embeddings[i]
		bestClusterIdx := -1
		bestSim := 0.0

		for ci, cl := range clusters {
			sim := vecutil.CosineSimilarity(emb, cl.centroid)
			if sim > bestSim {
				bestSim = sim
				bestClusterIdx = ci
			}
		}

		if bestClusterIdx >= 0 && bestSim >= s.clusterThreshold {
			cl := clusters[bestClusterIdx]
			cl.members = append(cl.members, ep)
			cl.embeddings = append(cl.embeddings, emb)
			cl.centroid = vecAverage(cl.embeddings)
		} else {
			clusters = append(clusters, &cluster{
				members:    []*domain.EpisodicMemory{ep},
				embeddings: [][]float32{emb},
				centroid:   copyVec(emb),
			})
		}
	}

	return clusters
}

func (s *ConsolidateService) clusterWithThreshold(episodes []*domain.EpisodicMemory, embeddings [][]float32, threshold float64) []*cluster {
	var clusters []*cluster
	for i, ep := range episodes {
		emb := embeddings[i]
		bestIdx := -1
		bestSim := 0.0
		for ci, cl := range clusters {
			sim := vecutil.CosineSimilarity(emb, cl.centroid)
			if sim > bestSim {
				bestSim = sim
				bestIdx = ci
			}
		}
		if bestIdx >= 0 && bestSim >= threshold {
			cl := clusters[bestIdx]
			cl.members = append(cl.members, ep)
			cl.embeddings = append(cl.embeddings, emb)
			cl.centroid = vecAverage(cl.embeddings)
		} else {
			clusters = append(clusters, &cluster{
				members:    []*domain.EpisodicMemory{ep},
				embeddings: [][]float32{emb},
				centroid:   copyVec(emb),
			})
		}
	}
	return clusters
}

// promoteClusterThematic creates a semantic fact from a loose (thematic) cluster.
// Uses first sentence of each member for a compact summary.
func (s *ConsolidateService) promoteClusterThematic(ctx context.Context, cl *cluster, projectID *uuid.UUID) (*domain.SemanticMemory, error) {
	var b strings.Builder
	b.WriteString("Thematic summary:\n")
	for i, ep := range cl.members {
		if i >= 5 {
			b.WriteString(fmt.Sprintf("  ... and %d more\n", len(cl.members)-5))
			break
		}
		sentence := firstSentence(ep.Content)
		b.WriteString(fmt.Sprintf("  - %s\n", sentence))
	}

	content := b.String()
	const maxChars = 600
	if len(content) > maxChars {
		content = content[:maxChars]
	}

	avgImportance := 0.0
	for _, ep := range cl.members {
		avgImportance += ep.Importance
	}
	avgImportance /= float64(len(cl.members))
	if avgImportance < 0.5 {
		avgImportance = 0.5
	}

	sourceIDs := make([]uuid.UUID, len(cl.members))
	for i, ep := range cl.members {
		sourceIDs[i] = ep.ID
	}

	now := time.Now()
	sem := &domain.SemanticMemory{
		MemoryItem: domain.MemoryItem{
			ID:           uuid.New(),
			ProjectID:    projectID,
			Tier:         domain.TierSemantic,
			Content:      content,
			Embedding:    cl.centroid,
			Importance:   avgImportance,
			Confidence:   float64(len(cl.members)) / float64(len(cl.members)+3), // higher bar: n/(n+3)
			TokenCount:   estimateTokens(content),
			LastAccessed: now,
			CreatedAt:    now,
			UpdatedAt:    now,
			Tags:         mergeUniqueTags(cl.members),
		},
		EntityType:     "theme",
		SourceEpisodes: sourceIDs,
	}

	if err := s.semantic.Insert(ctx, sem); err != nil {
		return nil, fmt.Errorf("insert thematic semantic: %w", err)
	}
	return sem, nil
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, ". "); idx > 0 && idx < 200 {
		return s[:idx+1]
	}
	if len(s) > 200 {
		if sp := strings.LastIndex(s[:200], " "); sp > 100 {
			return s[:sp] + "..."
		}
		return s[:200] + "..."
	}
	return s
}

func (s *ConsolidateService) promoteCluster(ctx context.Context, cl *cluster, projectID *uuid.UUID) (*domain.SemanticMemory, error) {
	best := cl.members[0]
	maxImportance := best.Importance
	for _, ep := range cl.members[1:] {
		if ep.Importance > maxImportance {
			best = ep
			maxImportance = ep.Importance
		}
	}

	entityType := inferEntityType(best.Tags, best.Content)

	content := best.Content
	if len(cl.members) > 1 {
		if s.llm != nil {
			synthesized, err := s.synthesizeCluster(ctx, cl, entityType)
			if err != nil {
				s.logger.Warn("LLM synthesis failed, falling back to merge", "error", err)
				content = mergeClusterContent(cl.members, best)
			} else {
				content = synthesized
			}
		} else {
			content = mergeClusterContent(cl.members, best)
		}
	}
	const maxSemanticChars = 800 // ~200 tokens
	if len(content) > maxSemanticChars {
		cut := content[:maxSemanticChars]
		if idx := strings.LastIndex(cut, ". "); idx > maxSemanticChars/2 {
			cut = cut[:idx+1]
		}
		content = cut
	}

	avgImportance := 0.0
	for _, ep := range cl.members {
		avgImportance += ep.Importance
	}
	avgImportance /= float64(len(cl.members))
	importance := math.Max(avgImportance, maxImportance)
	if importance < 0.6 {
		importance = 0.6
	}

	sourceIDs := make([]uuid.UUID, len(cl.members))
	for i, ep := range cl.members {
		sourceIDs[i] = ep.ID
	}

	allTags := mergeUniqueTags(cl.members)

	// Re-embed the final content so the embedding matches the actual text.
	// Centroid of N source embeddings drifts from the synthesized/merged content,
	// causing recall mismatches where the content says X but embedding points to Y.
	finalEmb, err := s.embedding.Embed(ctx, content)
	if err != nil {
		s.logger.Warn("re-embed failed, falling back to centroid", "error", err)
		finalEmb = cl.centroid
	}

	tokenCount := estimateTokens(content)
	now := time.Now()

	sem := &domain.SemanticMemory{
		MemoryItem: domain.MemoryItem{
			ID:           uuid.New(),
			ProjectID:    projectID,
			Tier:         domain.TierSemantic,
			Content:      content,
			Embedding:    finalEmb,
			Importance:   importance,
			Confidence:   float64(len(cl.members)) / float64(len(cl.members)+2), // Bayesian: n/(n+2)
			TokenCount:   tokenCount,
			LastAccessed: now,
			CreatedAt:    now,
			UpdatedAt:    now,
			Tags:         allTags,
		},
		EntityType:     entityType,
		SourceEpisodes: sourceIDs,
	}

	if err := s.semantic.Insert(ctx, sem); err != nil {
		return nil, fmt.Errorf("insert semantic: %w", err)
	}

	return sem, nil
}

// graduateErrorPatterns finds clusters of similar error episodes and
// generates prevention rules via LLM. This is the "skill acquisition" layer:
// repeated errors → condensed rule → proactive prevention.
func (s *ConsolidateService) graduateErrorPatterns(ctx context.Context, projectID *uuid.UUID) int {
	if s.llm == nil {
		return 0
	}

	errors, err := s.episodic.ListByTags(ctx, projectID, []string{"error"}, 50)
	if err != nil || len(errors) < 2 {
		return 0
	}

	// Embed all error episodes
	embs := make([][]float32, len(errors))
	for i, ep := range errors {
		emb, err := s.embedding.Embed(ctx, ep.Content)
		if err != nil {
			return 0
		}
		embs[i] = emb
	}

	// Cluster errors with lower threshold (0.60) to catch related errors
	clusters := s.clusterWithThreshold(errors, embs, 0.60)

	const maxRulesPerRun = 5
	rulesGenerated := 0
	for _, cl := range clusters {
		if len(cl.members) < 2 {
			continue
		}

		// Check if a rule for this pattern already exists
		if s.ruleAlreadyExists(ctx, cl.centroid, projectID) {
			continue
		}

		rule, err := s.generateRule(ctx, cl)
		if err != nil {
			s.logger.Warn("rule generation failed", "error", err, "cluster_size", len(cl.members))
			continue
		}

		emb, err := s.embedding.Embed(ctx, rule)
		if err != nil {
			continue
		}

		meta := parseRuleMetadata(rule, cl.members)
		tags := []string{"auto_rule", "error_pattern", "prevention"}
		if files, ok := meta["rule_files"].([]string); ok {
			for _, f := range files {
				tags = append(tags, "file:"+f)
			}
		}

		now := time.Now()
		sem := &domain.SemanticMemory{
			MemoryItem: domain.MemoryItem{
				ID:           uuid.New(),
				ProjectID:    projectID,
				Tier:         domain.TierSemantic,
				Content:      rule,
				Embedding:    emb,
				Importance:   0.90,
				Confidence:   float64(len(cl.members)) / float64(len(cl.members)+2),
				TokenCount:   estimateTokens(rule),
				LastAccessed: now,
				CreatedAt:    now,
				UpdatedAt:    now,
				Tags:         tags,
				Metadata:     meta,
			},
			EntityType: "rule",
		}

		if err := s.semantic.Insert(ctx, sem); err != nil {
			s.logger.Error("rule insert failed — data loss", "error", err)
			continue
		}
		rulesGenerated++
		metrics.RulesGenerated.Inc()
		if rulesGenerated >= maxRulesPerRun {
			s.logger.Info("max rules per run reached", "max", maxRulesPerRun)
			break
		}
	}

	return rulesGenerated
}

func (s *ConsolidateService) ruleAlreadyExists(ctx context.Context, centroid []float32, projectID *uuid.UUID) bool {
	similar, err := s.semantic.SearchSimilar(ctx, centroid, projectID, 1)
	if err != nil || len(similar) == 0 {
		return false
	}
	// If a rule with high similarity already exists, skip
	for _, sem := range similar {
		if sem.Similarity >= 0.80 && sem.EntityType == "rule" {
			return true
		}
	}
	return false
}

func (s *ConsolidateService) generateRule(ctx context.Context, cl *cluster) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var prompt strings.Builder
	prompt.WriteString("You are analyzing recurring code errors. Generate a CONCRETE prevention rule.\n\n")
	prompt.WriteString("Output ONLY this format, nothing else:\n")
	prompt.WriteString("WHEN: <specific trigger — package, function, pattern>\n")
	prompt.WriteString("WATCH: <exact thing to check — specific code pattern or mistake>\n")
	prompt.WriteString("BECAUSE: <what happened, with actual error messages from below>\n")
	prompt.WriteString("DO: <exact fix instruction, with code example if possible>\n")
	prompt.WriteString("ANTIPATTERN: <regex matching ONLY the buggy pattern, not valid usage. Must include surrounding context to avoid false positives. Example: `pool\\.Acquire\\([^)]+\\)\\s*$` (matches Acquire at end-of-line without defer). Avoid bare `.*` — always anchor with specific context. If no precise regex is possible, write NONE.>\n\n")
	prompt.WriteString("Rules:\n")
	prompt.WriteString("- NEVER use words: ensure, be careful, make sure, consider, remember to\n")
	prompt.WriteString("- Include specific function names, package names, error messages from the errors below\n")
	prompt.WriteString("- Include file paths if they appear in the errors\n")
	prompt.WriteString("- ANTIPATTERN must be a valid regex that detects the mistake in source code\n")
	prompt.WriteString("- ANTIPATTERN must NOT match valid code — it should detect ONLY the mistake\n")
	prompt.WriteString("- If you cannot write a precise regex, use ANTIPATTERN: NONE\n\n")
	prompt.WriteString("Errors:\n\n")

	for i, ep := range cl.members {
		if i >= 5 {
			prompt.WriteString(fmt.Sprintf("... and %d more similar errors\n", len(cl.members)-5))
			break
		}
		content := ep.Content
		if len(content) > 400 {
			content = content[:400] + "..."
		}
		prompt.WriteString(fmt.Sprintf("Error %d:\n%s\n\n", i+1, content))
	}

	result, err := s.llm.Chat(ctx, []domain.ChatMessage{
		{Role: "user", Content: prompt.String()},
	}, domain.ChatOptions{Temperature: 0.2, MaxTokens: 400})
	if err != nil {
		return "", err
	}

	result = strings.TrimSpace(result)
	if len(result) < 30 {
		return "", fmt.Errorf("rule too short: %d chars", len(result))
	}
	return result, nil
}

// parseRuleMetadata extracts structured fields from a WHEN/WATCH/BECAUSE/DO/ANTIPATTERN rule.
func parseRuleMetadata(ruleContent string, errorMembers []*domain.EpisodicMemory) domain.Metadata {
	meta := domain.Metadata{}

	lines := strings.Split(ruleContent, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "WHEN:"):
			meta["rule_when"] = strings.TrimSpace(strings.TrimPrefix(line, "WHEN:"))
		case strings.HasPrefix(line, "WATCH:"):
			meta["rule_watch"] = strings.TrimSpace(strings.TrimPrefix(line, "WATCH:"))
		case strings.HasPrefix(line, "BECAUSE:"):
			meta["rule_because"] = strings.TrimSpace(strings.TrimPrefix(line, "BECAUSE:"))
		case strings.HasPrefix(line, "DO:"):
			meta["rule_do"] = strings.TrimSpace(strings.TrimPrefix(line, "DO:"))
		case strings.HasPrefix(line, "ANTIPATTERN:"):
			ap := strings.TrimSpace(strings.TrimPrefix(line, "ANTIPATTERN:"))
			ap = strings.Trim(ap, "`")
			if ap == "" || strings.EqualFold(ap, "NONE") || strings.EqualFold(ap, "N/A") {
				break
			}
			if isAntiPatternTooGeneric(ap) {
				break
			}
			if _, err := regexp.Compile(ap); err == nil {
				meta["rule_antipattern"] = ap
			}
		}
	}

	// Extract file paths from error members
	var files []string
	seen := map[string]bool{}
	for _, ep := range errorMembers {
		for _, tag := range ep.Tags {
			if strings.HasPrefix(tag, "file:") {
				fp := strings.TrimPrefix(tag, "file:")
				if !seen[fp] {
					files = append(files, fp)
					seen[fp] = true
				}
			}
		}
	}
	if len(files) > 0 {
		meta["rule_files"] = files
	}

	return meta
}

// synthesizeCluster uses LLM to distill N episodic memories into one semantic fact.
// The LLM extracts the core knowledge, discards noise, and produces a concise fact
// that an AI agent can use without reading all source episodes.
func (s *ConsolidateService) synthesizeCluster(ctx context.Context, cl *cluster, entityType string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var prompt strings.Builder
	prompt.WriteString("You are a knowledge distillation system. ")
	prompt.WriteString("Given multiple related memory entries from an AI agent's experience, ")
	prompt.WriteString("synthesize them into ONE concise knowledge fact.\n\n")
	prompt.WriteString("Rules:\n")
	prompt.WriteString("- Extract the core insight, decision, or pattern\n")
	prompt.WriteString("- Remove redundancy and noise\n")
	prompt.WriteString("- Keep specific details (file names, error codes, config values)\n")
	prompt.WriteString("- Max 3-4 sentences\n")
	prompt.WriteString("- Output ONLY the synthesized fact, no meta-commentary\n\n")
	prompt.WriteString(fmt.Sprintf("Category: %s\n\n", entityType))

	for i, ep := range cl.members {
		if i >= 7 {
			prompt.WriteString(fmt.Sprintf("... and %d more entries\n", len(cl.members)-7))
			break
		}
		content := ep.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		prompt.WriteString(fmt.Sprintf("Entry %d:\n%s\n\n", i+1, content))
	}
	prompt.WriteString("Synthesized fact:")

	result, err := s.llm.Chat(ctx, []domain.ChatMessage{
		{Role: "user", Content: prompt.String()},
	}, domain.ChatOptions{Temperature: 0.2, MaxTokens: 200})
	if err != nil {
		return "", fmt.Errorf("llm synthesis: %w", err)
	}

	result = strings.TrimSpace(result)
	if len(result) < 20 {
		return "", fmt.Errorf("llm synthesis too short: %d chars", len(result))
	}

	s.logger.Info("cluster synthesized via LLM",
		"entity_type", entityType,
		"members", len(cl.members),
		"result_len", len(result),
	)
	return result, nil
}

func mergeClusterContent(members []*domain.EpisodicMemory, primary *domain.EpisodicMemory) string {
	var b strings.Builder
	b.WriteString(primary.Content)

	seen := make(map[string]bool)
	seen[primary.Content] = true

	for _, ep := range members {
		if seen[ep.Content] {
			continue
		}
		seen[ep.Content] = true
		extra := extractUniqueInfo(ep.Content, primary.Content)
		if extra != "" {
			b.WriteString("\n\n---\nAdditional context: ")
			b.WriteString(extra)
		}
	}

	return b.String()
}

// extractUniqueInfo returns sentences from `source` not present in `primary`.
// Simple heuristic: split by period, keep sentences longer than 20 chars
// that share less than 50% words with primary.
func extractUniqueInfo(source, primary string) string {
	primaryWords := wordSet(primary)
	sentences := strings.Split(source, ".")

	var unique []string
	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if len(s) < 20 {
			continue
		}
		sWords := wordSet(s)
		overlap := setOverlap(sWords, primaryWords)
		if overlap < 0.5 {
			unique = append(unique, s)
		}
	}

	if len(unique) == 0 {
		return ""
	}
	if len(unique) > 3 {
		unique = unique[:3]
	}
	return strings.Join(unique, ". ") + "."
}

func inferEntityType(tags []string, content string) string {
	tagSet := make(map[string]bool)
	for _, t := range tags {
		tagSet[strings.ToLower(t)] = true
	}

	if tagSet["decision"] || tagSet["architecture"] {
		return "decision"
	}
	if tagSet["bugfix"] || tagSet["fix"] || tagSet["error"] {
		return "bugfix"
	}
	if tagSet["pattern"] || tagSet["best-practice"] {
		return "pattern"
	}

	lower := strings.ToLower(content)
	if strings.Contains(lower, "decision") || strings.Contains(lower, "chose") || strings.Contains(lower, "decided") {
		return "decision"
	}
	if strings.Contains(lower, "fix") || strings.Contains(lower, "bug") || strings.Contains(lower, "error") {
		return "bugfix"
	}
	if strings.Contains(lower, "pattern") || strings.Contains(lower, "always") || strings.Contains(lower, "best practice") {
		return "pattern"
	}
	return "fact"
}

func mergeUniqueTags(members []*domain.EpisodicMemory) []string {
	seen := make(map[string]bool)
	var result []string
	for _, ep := range members {
		for _, t := range ep.Tags {
			if !seen[t] {
				seen[t] = true
				result = append(result, t)
			}
		}
	}
	return result
}

func wordSet(s string) map[string]bool {
	words := strings.Fields(strings.ToLower(s))
	set := make(map[string]bool, len(words))
	for _, w := range words {
		if len(w) > 2 {
			set[w] = true
		}
	}
	return set
}

func setOverlap(a, b map[string]bool) float64 {
	if len(a) == 0 {
		return 0
	}
	common := 0
	for w := range a {
		if b[w] {
			common++
		}
	}
	return float64(common) / float64(len(a))
}


func vecAverage(vecs [][]float32) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	dims := len(vecs[0])
	avg := make([]float32, dims)
	n := float32(len(vecs))
	for _, v := range vecs {
		for i, val := range v {
			avg[i] += val
		}
	}
	for i := range avg {
		avg[i] /= n
	}
	return avg
}

func copyVec(v []float32) []float32 {
	cp := make([]float32, len(v))
	copy(cp, v)
	return cp
}

// isAntiPatternTooGeneric rejects anti-patterns that would match virtually any code.
func isAntiPatternTooGeneric(pattern string) bool {
	if pattern == "" {
		return true
	}

	// Reject trivially broad patterns
	stripped := strings.TrimLeft(pattern, "^")
	stripped = strings.TrimRight(stripped, "$")
	if stripped == ".*" || stripped == ".+" || stripped == "" {
		return true
	}

	// Count literal characters (letters, digits, dots, underscores)
	literalCount := 0
	for _, c := range pattern {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' {
			literalCount++
		}
	}
	if literalCount < 5 {
		return true
	}

	// Compile and test against common Go code — if matches 2+, too broad
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return true
	}
	probes := []string{
		"func main() {",
		"return nil",
		"if err != nil {",
		"fmt.Println(x)",
	}
	matchCount := 0
	for _, probe := range probes {
		if compiled.MatchString(probe) {
			matchCount++
		}
	}
	return matchCount >= 2
}
