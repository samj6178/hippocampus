package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/memory"
	"github.com/hippocampus-mcp/hippocampus/internal/metrics"
	"github.com/hippocampus-mcp/hippocampus/internal/pkg/vecutil"
)

// RecallService implements the RECALL operation of Memory Algebra.
// Assembles optimal context from all memory tiers using submodular maximization.
//
// Flow: query -> Embed -> SearchAll(tiers) -> Score -> Submodular Select -> Assemble
type RecallService struct {
	episodic   domain.EpisodicRepo
	semantic   domain.SemanticRepo
	procedural domain.ProceduralRepo
	embedding  domain.EmbeddingProvider
	working    *memory.WorkingMemory
	causal     *CausalDetector
	hybrid     *HybridRetriever
	llm        domain.LLMProvider
	logger     *slog.Logger

	decayHalfLifeSec float64
	weights          ScoreWeights
	ollamaBaseURL    string
	translationModel string
}

func (s *RecallService) SetCausalDetector(cd *CausalDetector) {
	s.causal = cd
}

func (s *RecallService) SetHybridRetriever(hr *HybridRetriever) {
	s.hybrid = hr
}

type ScoreWeights struct {
	Semantic  float64
	Recency   float64
	Explicit  float64
	Emotional float64
	Keyword   float64
}

func DefaultWeights() ScoreWeights {
	return ScoreWeights{
		Semantic:  0.35,
		Keyword:   0.25,
		Recency:   0.20,
		Explicit:  0.15,
		Emotional: 0.05,
	}
}

type RecallServiceConfig struct {
	DecayHalfLifeDays float64
	Weights           ScoreWeights
	OllamaBaseURL     string // for query translation, e.g. "http://localhost:11434"
	TranslationModel  string // e.g. "qwen2.5:7b"
}

func NewRecallService(
	episodic domain.EpisodicRepo,
	semantic domain.SemanticRepo,
	procedural domain.ProceduralRepo,
	embedding domain.EmbeddingProvider,
	working *memory.WorkingMemory,
	cfg RecallServiceConfig,
	logger *slog.Logger,
	llm ...domain.LLMProvider,
) *RecallService {
	if cfg.DecayHalfLifeDays <= 0 {
		cfg.DecayHalfLifeDays = 7.0
	}
	w := cfg.Weights
	if w.Semantic+w.Recency+w.Explicit+w.Emotional == 0 {
		w = DefaultWeights()
	}
	ollamaURL := cfg.OllamaBaseURL
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}
	transModel := cfg.TranslationModel
	if transModel == "" {
		transModel = "qwen2.5:7b"
	}
	var llmProvider domain.LLMProvider
	if len(llm) > 0 && llm[0] != nil {
		llmProvider = llm[0]
	}
	return &RecallService{
		episodic:         episodic,
		semantic:         semantic,
		procedural:       procedural,
		embedding:        embedding,
		working:          working,
		llm:              llmProvider,
		logger:           logger,
		decayHalfLifeSec: cfg.DecayHalfLifeDays * 86400,
		weights:          w,
		ollamaBaseURL:    ollamaURL,
		translationModel: transModel,
	}
}

type RecallRequest struct {
	Query      string         `json:"query"`
	ProjectID  *uuid.UUID     `json:"project_id,omitempty"`
	Budget     domain.TokenBudget `json:"budget"`
	AgentID    string         `json:"agent_id"`
	IncludeGlobal bool       `json:"include_global"`
}

type RecallResponse struct {
	Context      *domain.AssembledContext `json:"context"`
	Candidates   int                      `json:"candidates_considered"`
	Latency      time.Duration            `json:"latency"`
	RejectReason string                   `json:"reject_reason,omitempty"`
	BestSim      float64                  `json:"best_sim,omitempty"`
	QueryEmb     []float32                `json:"-"`
}

func (s *RecallService) Recall(ctx context.Context, req *RecallRequest) (*RecallResponse, error) {
	recallStart := time.Now()
	if req.Query == "" {
		return &RecallResponse{
			Context: &domain.AssembledContext{
				Text:       "No query provided. Use mos_recall with a description of what you need to know, e.g. 'how does deployment work' or 'what bugs were fixed recently'.",
				TokenCount: 30,
				Confidence: 0,
			},
			Candidates: 0,
			Latency:    time.Since(recallStart),
		}, nil
	}
	if req.Budget.Total <= 0 {
		req.Budget = domain.DefaultBudget()
	}

	query := req.Query
	var originalEmb []float32
	isCyrillic := containsCyrillic(query)

	if isCyrillic {
		origEmb, err := s.embedding.Embed(ctx, query)
		if err == nil {
			originalEmb = origEmb
		}
		translated, err := s.translateToEnglish(ctx, query)
		if err != nil {
			s.logger.Warn("query translation failed, using original", "error", err)
		} else if translated != "" {
			s.logger.Info("query translated", "original", query, "translated", translated)
			query = translated
		}
	}

	queryEmb, err := s.embedding.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	candidates, err := s.gatherCandidates(ctx, query, queryEmb, req)
	if err != nil {
		return nil, fmt.Errorf("gather candidates: %w", err)
	}

	if isCyrillic && len(originalEmb) > 0 {
		origCandidates, err := s.gatherCandidates(ctx, req.Query, originalEmb, req)
		if err == nil {
			candidates = dedup(append(candidates, origCandidates...))
		}
	}

	// Query expansion: generate an alternative phrasing to catch synonyms
	// that the original query misses (e.g. "emotion detection" ↔ "emotional boost").
	// Triggered when best candidate similarity is weak (<0.72), suggesting
	// the original phrasing doesn't match stored content well.
	if s.llm != nil && shouldExpandQuery(candidates) {
		if alt := s.expandQuery(ctx, query); alt != "" {
			altEmb, err := s.embedding.Embed(ctx, alt)
			if err == nil {
				altCandidates, err := s.gatherCandidates(ctx, alt, altEmb, req)
				if err == nil {
					candidates = dedup(append(candidates, altCandidates...))
					metrics.QueryExpansions.Inc()
					s.logger.Debug("query expanded", "original", query, "expanded", alt, "extra", len(altCandidates))
				}
			}
		}
	}

	now := time.Now()
	bestEmb := queryEmb
	if isCyrillic && len(originalEmb) > 0 {
		scored1 := s.scoreAll(candidates, queryEmb, query, now)
		scored2 := s.scoreAll(candidates, originalEmb, req.Query, now)
		best1 := 0.0
		best2 := 0.0
		if len(scored1) > 0 {
			best1 = scored1[0].Score.SemanticSimilarity
		}
		if len(scored2) > 0 {
			best2 = scored2[0].Score.SemanticSimilarity
		}
		if best2 > best1 {
			bestEmb = originalEmb
			s.logger.Info("bilingual recall: original embedding wins", "best_orig", best2, "best_trans", best1)
		}
	}
	scored := s.scoreAll(candidates, bestEmb, query, now)
	scored = s.filterWeakCandidates(scored)

	if s.llm != nil && len(scored) > 0 {
		scored = s.llmRerank(ctx, query, scored)
	}

	irrelevant, rejectReason := s.detectIrrelevantQuery(scored, query, req.ProjectID)
	if irrelevant {
		bestSim := 0.0
		if len(scored) > 0 {
			bestSim = scored[0].Score.SemanticSimilarity
		}
		metrics.RecallTotal.Inc()
		metrics.RecallMisses.Inc()
		metrics.RecallLatency.Observe(time.Since(recallStart).Seconds())
		metrics.RecallConfidence.Observe(0)
		reasonCat := "other"
		if strings.Contains(rejectReason, "absolute_floor") {
			reasonCat = "absolute_floor"
		} else if strings.Contains(rejectReason, "entropy") {
			reasonCat = "high_entropy"
		} else if strings.Contains(rejectReason, "spread") {
			reasonCat = "flat_spread"
		} else if strings.Contains(rejectReason, "keyword") {
			reasonCat = "weak_keywords"
		} else if strings.Contains(rejectReason, "domain") {
			reasonCat = "domain_specific"
		} else if strings.Contains(rejectReason, "cross_project") {
			reasonCat = "cross_project"
		}
		metrics.RejectionsByReason.WithLabelValues(reasonCat).Inc()

		s.logger.Info("recall: no relevant memories found",
			"query_len", len(req.Query),
			"best_sim", bestSim,
			"reason", rejectReason,
			"candidates", len(scored),
		)
		return &RecallResponse{
			Context: &domain.AssembledContext{
				Text:       "No relevant memories found for this query.",
				TokenCount: 8,
				Confidence: 0,
			},
			Candidates:   len(candidates),
			Latency:      time.Since(recallStart),
			RejectReason: rejectReason,
			BestSim:      bestSim,
		}, nil
	}

	selected := s.submodularSelect(scored, req.Budget)

	assembled := s.assembleContext(selected, req.Budget)

	if s.causal != nil && len(selected) > 0 {
		var ids []uuid.UUID
		for _, sm := range selected {
			ids = append(ids, sm.Memory.ID)
		}
		causalCtx := s.causal.GetCausalContext(ctx, ids)
		if causalCtx != "" {
			causalTokens := estimateTokens(causalCtx)
			if assembled.TokenCount+causalTokens <= req.Budget.Total {
				assembled.Text += causalCtx
				assembled.TokenCount += causalTokens
			}
		}
	}

	go s.refreshAccessed(ctx, selected)

	metrics.RecallTotal.Inc()
	metrics.RecallHits.Inc()
	metrics.RecallLatency.Observe(time.Since(recallStart).Seconds())
	metrics.RecallTokens.Observe(float64(assembled.TokenCount))
	metrics.RecallConfidence.Observe(assembled.Confidence)

	s.logger.Info("recall completed",
		"query_len", len(req.Query),
		"candidates", len(candidates),
		"selected", len(selected),
		"tokens", assembled.TokenCount,
	)

	return &RecallResponse{
		Context:    assembled,
		Candidates: len(candidates),
		Latency:    time.Since(recallStart),
		QueryEmb:   bestEmb,
	}, nil
}

func (s *RecallService) gatherCandidates(ctx context.Context, query string, queryEmb []float32, req *RecallRequest) ([]*domain.MemoryItem, error) {
	var candidates []*domain.MemoryItem
	limit := 50

	workingSnapshot := s.working.Snapshot(ctx)
	for _, sm := range workingSnapshot {
		candidates = append(candidates, sm.Memory)
	}

	if s.hybrid != nil && query != "" {
		results, err := s.hybrid.Retrieve(ctx, query, queryEmb, req.ProjectID, limit)
		if err != nil {
			s.logger.Warn("hybrid retrieval failed, falling back to vector-only", "error", err)
		} else {
			for _, r := range results {
				if r.BM25Rank > 0 {
					if r.Memory.Metadata == nil {
						r.Memory.Metadata = make(domain.Metadata)
					}
					r.Memory.Metadata["_bm25_rank"] = r.BM25Rank
				}
				candidates = append(candidates, r.Memory)
			}

			if req.IncludeGlobal {
				global, err := s.semantic.SearchGlobal(ctx, queryEmb, 10)
				if err != nil {
					s.logger.Warn("global semantic search failed", "error", err)
				} else {
					for _, g := range global {
						if g.Similarity >= 0.45 {
							candidates = append(candidates, &g.MemoryItem)
						}
					}
				}
			}

			procedural, err := s.procedural.SearchByTaskType(ctx, queryEmb, req.ProjectID, limit/2)
			if err != nil {
				s.logger.Warn("procedural search failed", "error", err)
			} else {
				for _, p := range procedural {
					candidates = append(candidates, &p.MemoryItem)
				}
			}

			s.logger.Debug("hybrid gather", "total", len(candidates), "hybrid_results", len(results))
			return dedup(candidates), nil
		}
	}

	// Fallback: vector-only retrieval
	episodic, err := s.episodic.SearchSimilar(ctx, queryEmb, req.ProjectID, limit)
	if err != nil {
		s.logger.Warn("episodic search failed", "error", err)
	} else {
		for _, e := range episodic {
			candidates = append(candidates, &e.MemoryItem)
		}
	}

	semantic, err := s.semantic.SearchSimilar(ctx, queryEmb, req.ProjectID, limit)
	if err != nil {
		s.logger.Warn("semantic search failed", "error", err)
	} else {
		for _, sm := range semantic {
			candidates = append(candidates, &sm.MemoryItem)
		}
	}

	if req.IncludeGlobal {
		global, err := s.semantic.SearchGlobal(ctx, queryEmb, 10)
		if err != nil {
			s.logger.Warn("global semantic search failed", "error", err)
		} else {
			for _, g := range global {
				if g.Similarity >= 0.45 {
					candidates = append(candidates, &g.MemoryItem)
				}
			}
		}
	}

	procedural, err := s.procedural.SearchByTaskType(ctx, queryEmb, req.ProjectID, limit/2)
	if err != nil {
		s.logger.Warn("procedural search failed", "error", err)
	} else {
		for _, p := range procedural {
			candidates = append(candidates, &p.MemoryItem)
		}
	}

	return dedup(candidates), nil
}

func (s *RecallService) scoreAll(candidates []*domain.MemoryItem, queryEmb []float32, query string, now time.Time) []*domain.ScoredMemory {
	decayλ := math.Ln2 / s.decayHalfLifeSec
	scored := make([]*domain.ScoredMemory, 0, len(candidates))

	queryKeywords := extractKeywords(query)
	temporalRange := parseTemporalHint(query, now)

	for _, mem := range candidates {
		sim := mem.Similarity
		if len(mem.Embedding) > 0 && len(queryEmb) > 0 {
			computed := vecutil.CosineSimilarity(queryEmb, mem.Embedding)
			if computed > sim {
				sim = computed
			}
		}

		age := now.Sub(mem.LastAccessed).Seconds()
		recency := math.Exp(-decayλ * age)

		kwScore := keywordOverlapScore(mem.Content, queryKeywords)

		if rank, ok := mem.Metadata["_bm25_rank"]; ok {
			if r, ok := rank.(int); ok && r > 0 {
				bm25Boost := 0.3 / float64(r)
				kwScore = math.Min(1.0, kwScore+bm25Boost)
			}
		}

		composite := s.weights.Semantic*sim +
			s.weights.Keyword*kwScore +
			s.weights.Recency*recency +
			s.weights.Explicit*mem.Importance

		// Temporal boost: if query mentions a time range ("last week", "yesterday"),
		// memories created within that range get a score boost.
		if temporalRange != nil && !mem.CreatedAt.IsZero() {
			if mem.CreatedAt.After(temporalRange.from) && mem.CreatedAt.Before(temporalRange.to) {
				composite += 0.15
			}
		}

		scored = append(scored, &domain.ScoredMemory{
			Memory: mem,
			Score: domain.ImportanceScore{
				SemanticSimilarity: sim,
				KeywordRelevance:   kwScore,
				Recency:            recency,
				ExplicitImportance: mem.Importance,
				Composite:          composite,
			},
		})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score.Composite > scored[j].Score.Composite
	})

	return scored
}

type temporalHint struct {
	from, to time.Time
}

// parseTemporalHint detects time references in the query and returns the implied date range.
// Supports: "yesterday", "today", "last week", "this week", "last month", "recently" (7 days),
// and Russian equivalents.
func parseTemporalHint(query string, now time.Time) *temporalHint {
	lower := strings.ToLower(query)

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch {
	case strings.Contains(lower, "yesterday") || strings.Contains(lower, "вчера"):
		return &temporalHint{from: today.AddDate(0, 0, -1), to: today}

	case strings.Contains(lower, "today") || strings.Contains(lower, "сегодня"):
		return &temporalHint{from: today, to: now}

	case strings.Contains(lower, "last week") || strings.Contains(lower, "на прошлой неделе") ||
		strings.Contains(lower, "за последнюю неделю") || strings.Contains(lower, "на этой неделе"):
		return &temporalHint{from: today.AddDate(0, 0, -7), to: now}

	case strings.Contains(lower, "last month") || strings.Contains(lower, "в прошлом месяце") ||
		strings.Contains(lower, "за последний месяц"):
		return &temporalHint{from: today.AddDate(0, -1, 0), to: now}

	case strings.Contains(lower, "recently") || strings.Contains(lower, "недавно") ||
		strings.Contains(lower, "последн"):
		return &temporalHint{from: today.AddDate(0, 0, -7), to: now}

	case strings.Contains(lower, "last 3 days") || strings.Contains(lower, "за 3 дня") ||
		strings.Contains(lower, "за три дня"):
		return &temporalHint{from: today.AddDate(0, 0, -3), to: now}
	}

	return nil
}

// filterWeakCandidates removes candidates whose semantic similarity is below
// 50% of the best candidate's similarity. This prevents low-relevance memories
// from consuming the token budget as the memory store grows.
// Working memory items are exempt (no embedding-based score).
func (s *RecallService) filterWeakCandidates(scored []*domain.ScoredMemory) []*domain.ScoredMemory {
	if len(scored) == 0 {
		return scored
	}
	var maxSim float64
	for _, sm := range scored {
		if sm.Score.SemanticSimilarity > maxSim {
			maxSim = sm.Score.SemanticSimilarity
		}
	}

	const relativeThreshold = 0.65
	const absoluteFloor = 0.35
	cutoff := maxSim * relativeThreshold
	if cutoff < absoluteFloor {
		cutoff = absoluteFloor
	}

	filtered := make([]*domain.ScoredMemory, 0, len(scored))
	for _, sm := range scored {
		if sm.Memory.Tier == domain.TierWorking || sm.Score.SemanticSimilarity >= cutoff {
			filtered = append(filtered, sm)
		}
	}

	if len(filtered) == 0 {
		return scored
	}
	return filtered
}

// submodularSelect implements greedy submodular maximization.
// At each step, picks the candidate that maximizes marginal gain
// (score minus redundancy penalty from already-selected items).
// This avoids the "5 copies of the same fact" problem of naive top-K.
func (s *RecallService) submodularSelect(scored []*domain.ScoredMemory, budget domain.TokenBudget) []*domain.ScoredMemory {
	if len(scored) == 0 {
		return nil
	}

	tierMax := map[domain.MemoryTier]int{
		domain.TierEpisodic:   int(float64(budget.Total) * 0.50),
		domain.TierSemantic:   int(float64(budget.Total) * 0.35),
		domain.TierProcedural: int(float64(budget.Total) * 0.15),
		domain.TierWorking:    int(float64(budget.Total) * 0.20),
	}
	tierUsed := map[domain.MemoryTier]int{}

	selected := make([]*domain.ScoredMemory, 0, len(scored))
	used := make(map[uuid.UUID]bool)
	tokensUsed := 0
	firstComposite := 0.0

	for tokensUsed < budget.Total && len(scored) > 0 {
		bestIdx := -1
		bestGain := -1.0

		for i, cand := range scored {
			if used[cand.Memory.ID] {
				continue
			}
			if tokensUsed+cand.Memory.TokenCount > budget.Total {
				continue
			}
			if max, ok := tierMax[cand.Memory.Tier]; ok {
				if tierUsed[cand.Memory.Tier]+cand.Memory.TokenCount > max {
					continue
				}
			}

			gain := cand.Score.Composite - s.redundancyPenalty(cand, selected)
			if gain > bestGain {
				bestGain = gain
				bestIdx = i
			}
		}

		if bestIdx < 0 || bestGain <= 0 {
			break
		}

		if firstComposite == 0 {
			firstComposite = scored[bestIdx].Score.Composite
		} else if bestGain < firstComposite*0.30 {
			break
		}

		pick := scored[bestIdx]
		selected = append(selected, pick)
		used[pick.Memory.ID] = true
		tokensUsed += pick.Memory.TokenCount
		tierUsed[pick.Memory.Tier] += pick.Memory.TokenCount
	}

	return selected
}

// llmRerank uses the LLM as a cross-encoder judge to rescore the top candidates.
// Only the top 5 are sent to the LLM (cost/latency tradeoff). If LLM is unavailable
// or slow (>4s timeout), falls back to the original scoring silently.
// The LLM scores 0-10 are normalized and blended with the cosine-based composite.
func (s *RecallService) llmRerank(ctx context.Context, query string, scored []*domain.ScoredMemory) []*domain.ScoredMemory {
	topK := 5
	if topK > len(scored) {
		topK = len(scored)
	}

	rerankStart := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	var prompt strings.Builder
	prompt.WriteString("Rate the relevance of each document to the query. Scale 0-10.\n")
	prompt.WriteString("0 = completely unrelated, 10 = directly answers the query.\n")
	prompt.WriteString("Output ONLY comma-separated numbers, nothing else.\n\n")
	prompt.WriteString(fmt.Sprintf("Query: %s\n\n", query))

	for i := 0; i < topK; i++ {
		content := scored[i].Memory.Content
		if len(content) > 400 {
			content = content[:400] + "..."
		}
		prompt.WriteString(fmt.Sprintf("Doc %d: %s\n\n", i+1, content))
	}
	prompt.WriteString(fmt.Sprintf("Scores (%d numbers, comma-separated):", topK))

	result, err := s.llm.Chat(ctx, []domain.ChatMessage{
		{Role: "user", Content: prompt.String()},
	}, domain.ChatOptions{Temperature: 0.1, MaxTokens: 30})
	if err != nil {
		s.logger.Warn("llm rerank failed, using cosine scores", "error", err)
		return scored
	}

	scores := parseLLMScores(strings.TrimSpace(result), topK)
	if scores == nil {
		return scored
	}

	avgLLM := 0.0
	for _, sc := range scores {
		avgLLM += sc
	}
	avgLLM /= float64(len(scores))

	if avgLLM < 2.0 {
		s.logger.Debug("llm rerank: all candidates scored low, keeping original order", "avg", avgLLM)
		return scored
	}

	for i := 0; i < topK; i++ {
		llmNorm := scores[i] / 10.0
		original := scored[i].Score.Composite
		scored[i].Score.Composite = 0.6*original + 0.4*llmNorm
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score.Composite > scored[j].Score.Composite
	})

	metrics.LLMRerankLatency.Observe(time.Since(rerankStart).Seconds())

	s.logger.Debug("llm rerank applied", "topK", topK, "scores", scores)
	return scored
}

func parseLLMScores(raw string, expected int) []float64 {
	raw = strings.Trim(raw, "[]() \n\r\t")
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n'
	})
	var scores []float64
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v := 0.0
		for i, c := range p {
			if (c >= '0' && c <= '9') || c == '.' {
				continue
			}
			p = p[:i]
			break
		}
		fmt.Sscanf(p, "%f", &v)
		if v < 0 {
			v = 0
		}
		if v > 10 {
			v = 10
		}
		scores = append(scores, v)
	}
	if len(scores) < expected {
		return nil
	}
	return scores[:expected]
}

// redundancyPenalty computes how much information overlap exists
// between a candidate and the already-selected set.
// Uses max cosine similarity as a proxy for redundancy.
// Hard reject at similarity > 0.85 (near-duplicate), soft penalty λ=0.7.
func (s *RecallService) redundancyPenalty(candidate *domain.ScoredMemory, selected []*domain.ScoredMemory) float64 {
	if len(selected) == 0 {
		return 0
	}

	maxSim := 0.0
	for _, sel := range selected {
		sim := vecutil.CosineSimilarity(candidate.Memory.Embedding, sel.Memory.Embedding)
		if sim > maxSim {
			maxSim = sim
		}
	}

	if maxSim > 0.85 {
		return 999.0
	}

	return maxSim * 0.7
}

// detectIrrelevantQuery applies multi-layer rejection to decide if the query
// is unrelated to stored memories. Returns (true, reason) if irrelevant.
//
// Layer 1: Absolute similarity floor — hard reject when best match is too weak.
// Layer 2: Spread heuristic — if top candidates are uniformly distant, query is noise.
// Layer 3: Entropy-based — if top-N similarities form a near-uniform distribution,
//
//	no memory is meaningfully more relevant than any other.
func (s *RecallService) detectIrrelevantQuery(scored []*domain.ScoredMemory, originalQuery string, projectID *uuid.UUID) (bool, string) {
	if len(scored) == 0 {
		return true, "no_candidates"
	}

	bestSim := scored[0].Score.SemanticSimilarity

	const absoluteFloor = 0.35
	if bestSim < absoluteFloor {
		return true, fmt.Sprintf("below_absolute_floor(%.3f < %.3f)", bestSim, absoluteFloor)
	}

	// BM25 fast path: if a top candidate was found by keyword search (BM25),
	// trust the text match and skip statistical rejection.
	// This is critical for code search where cosine sim is low but name matches exactly.
	if hasBM25MatchInTop(scored, 5) {
		return false, ""
	}

	// Project-scoped fast path: when the user explicitly requested a project
	// and the top result is from that project with decent similarity,
	// skip statistical rejection layers (entropy, spread, domain).
	// These layers are designed to catch cross-project noise, but for
	// project-scoped queries they produce false negatives.
	projectScoped := projectID != nil && hasProjectMemoryInTop(scored, projectID, 3, 0.40)
	if projectScoped {
		return false, ""
	}

	n := len(scored)

	topN := 10
	if topN > n {
		topN = n
	}
	if topN >= 5 {
		sims := make([]float64, topN)
		for i := 0; i < topN; i++ {
			sims[i] = scored[i].Score.SemanticSimilarity
		}
		entropy := normalizedEntropy(sims)
		if entropy > 0.92 && bestSim < 0.55 {
			if !hasProjectMemoryInTop(scored, projectID, 5, 0.46) {
				return true, fmt.Sprintf("high_entropy(H=%.3f, best=%.3f)", entropy, bestSim)
			}
		}
	}

	if n >= 3 {
		thirdSim := scored[2].Score.SemanticSimilarity
		spread := bestSim - thirdSim
		if spread < 0.025 && bestSim < 0.55 {
			if !hasProjectMemoryInTop(scored, projectID, 5, 0.46) {
				return true, fmt.Sprintf("flat_spread(spread=%.4f, best=%.3f)", spread, bestSim)
			}
		}
	}

	if bestSim < 0.72 {
		topCheck := 5
		if topCheck > n {
			topCheck = n
		}
		queryKWCount := countQueryKeywords(originalQuery)
		topFromProject := topMemoryFromProject(scored, projectID)

		if topFromProject && queryKWCount <= 2 && bestSim >= 0.44 {
			return false, ""
		}

		overlapCount := queryKeywordsOverlapStemmed(originalQuery, scored[:topCheck])
		minOverlap := 2
		if queryKWCount <= 2 {
			minOverlap = 1
		}

		projectOverlap := 0
		if projectID != nil {
			projectOnly := filterByProject(scored, projectID)
			if len(projectOnly) > 0 {
				projectOverlap = queryKeywordsOverlapStemmed(originalQuery, projectOnly)
			}
		}

		if overlapCount < minOverlap {
			if topFromProject && projectOverlap >= 1 {
				// Don't reject yet — fall through to domain-specific rejection
				// to verify the matching keywords are truly domain-relevant
			} else {
				return true, fmt.Sprintf("weak_keyword_overlap(overlap=%d/%d, proj_overlap=%d, need=%d, best=%.3f, project=%v)", overlapCount, queryKWCount, projectOverlap, minOverlap, bestSim, topFromProject)
			}
		}

		if projectID != nil && projectOverlap == 0 && overlapCount >= minOverlap {
			return true, fmt.Sprintf("cross_project_noise(overlap=%d from non-project, proj_overlap=0, best=%.3f)", overlapCount, bestSim)
		}
	}

	if bestSim < 0.72 {
		if rejected, reason := domainSpecificRejection(scored, originalQuery, n); rejected {
			return true, reason
		}
	}

	return false, ""
}

// domainSpecificRejection is the final rejection layer.
// Even if keyword overlap passes (generic words like "optimization", "framework"
// can match many project memories), we verify that at least one domain-specific
// query keyword appears as an EXACT word in the top candidates' content.
//
// "Domain-specific" = not in the expanded stop-word list (which includes
// generic programming terms like "function", "model", "data", "optimization").
//
// This catches queries like "React useState hook" or "python django framework"
// that share generic vocabulary with the project but are about unrelated domains.
func domainSpecificRejection(scored []*domain.ScoredMemory, query string, n int) (bool, string) {
	queryKWs := extractDomainKeywords(query)
	if len(queryKWs) < 2 {
		return false, ""
	}

	topCheck := 15
	if topCheck > n {
		topCheck = n
	}

	matchedKWs := 0
	for _, kw := range queryKWs {
		for i := 0; i < topCheck; i++ {
			contentLower := strings.ToLower(scored[i].Memory.Content)
			if exactWordMatch(contentLower, kw) {
				matchedKWs++
				break
			}
		}
	}

	if matchedKWs >= 2 {
		return false, ""
	}

	if matchedKWs == 1 && len(queryKWs) <= 2 {
		return false, ""
	}

	return true, fmt.Sprintf("domain_specific_rejection(matched=%d/%d domain keywords %v in top-%d)", matchedKWs, len(queryKWs), queryKWs, topCheck)
}

var genericProgrammingWords = map[string]bool{
	"error": true, "bug": true, "fix": true, "code": true, "file": true,
	"test": true, "debug": true, "log": true, "config": true, "server": true,
	"client": true, "api": true, "endpoint": true, "request": true, "response": true,
	"database": true, "query": true, "table": true, "schema": true, "migration": true,
	"deploy": true, "docker": true, "container": true, "port": true, "host": true,
	"memory": true, "cache": true, "queue": true, "event": true, "handler": true,
	"service": true, "module": true, "package": true, "import": true, "export": true,
	"class": true, "interface": true, "struct": true, "type": true, "method": true,
	"variable": true, "constant": true, "string": true, "number": true, "boolean": true,
	"array": true, "list": true, "map": true, "object": true, "json": true,
	"http": true, "url": true, "path": true, "route": true, "middleware": true,
	"auth": true, "token": true, "session": true, "user": true, "role": true,
	"async": true, "sync": true, "channel": true, "goroutine": true, "thread": true,
	"performance": true, "optimization": true, "latency": true, "throughput": true,
	"render": true, "rendering": true, "component": true, "state": true, "context": true,
	"hook": true, "callback": true, "promise": true, "future": true, "stream": true,
	"framework": true, "library": true, "tool": true, "plugin": true, "extension": true,
	"pattern": true, "design": true, "architecture": true, "layer": true, "tier": true,
	"version": true, "release": true, "branch": true, "commit": true, "merge": true,
	"project": true, "repository": true, "workspace": true, "environment": true,
	"production": true, "staging": true, "development": true, "local": true,
	"monitor": true, "metric": true, "alert": true, "dashboard": true, "report": true,
	"search": true, "filter": true, "sort": true, "index": true, "rank": true,
	"embedding": true, "vector": true, "similarity": true, "score": true, "weight": true,
	"train": true, "training": true, "dataset": true, "batch": true, "epoch": true,
	"pipeline": true, "workflow": true, "process": true, "task": true, "job": true,
	"management": true, "system": true, "platform": true, "application": true, "app": true,
	"web": true, "frontend": true, "backend": true, "fullstack": true, "stack": true,
	"embed": true, "encode": true, "decode": true, "parse": true, "format": true,
	"validate": true, "generate": true, "transform": true, "convert": true, "load": true,
	"save": true, "store": true, "fetch": true, "send": true, "receive": true,
	"happen": true, "happened": true, "happening": true, "explain": true, "describe": true,
	"implement": true, "evaluation": true, "evaluate": true, "analyze": true, "compare": true,
	"improve": true, "resolve": true, "handle": true, "manage": true,
	"buffer": true, "formula": true, "option": true, "options": true, "adjustment": true,
	"show": true, "showing": true, "shown": true, "display": true, "appear": true,
	"appearing": true, "rebuild": true, "rebuilt": true, "connect": true, "disconnect": true,
	"return": true, "returns": true, "call": true, "calling": true, "write": true,
	"read": true, "check": true, "checking": true, "look": true, "looking": true,
	"find": true, "finding": true, "need": true, "working": true, "break": true, "broken": true,
	"fail": true, "failing": true, "failed": true, "crash": true, "crashing": true,
	"slow": true, "fast": true, "large": true, "small": true, "new": true, "old": true,
}

func extractDomainKeywords(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	var domain []string
	for _, w := range words {
		w = strings.Trim(w, ".,!?;:'\"()[]{}/-")
		if len(w) < 3 {
			continue
		}
		if recallStopWords[w] || genericProgrammingWords[w] {
			continue
		}
		domain = append(domain, w)
	}
	return domain
}

func exactWordMatch(content, keyword string) bool {
	idx := 0
	for {
		pos := strings.Index(content[idx:], keyword)
		if pos < 0 {
			return false
		}
		absPos := idx + pos
		leftOK := absPos == 0 || !isWordChar(rune(content[absPos-1]))
		rightEnd := absPos + len(keyword)
		rightOK := rightEnd >= len(content) || !isWordChar(rune(content[rightEnd]))
		if leftOK && rightOK {
			return true
		}
		idx = absPos + 1
		if idx >= len(content) {
			return false
		}
	}
}

func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

func topMemoryFromProject(top []*domain.ScoredMemory, projectID *uuid.UUID) bool {
	if projectID == nil || len(top) == 0 {
		return false
	}
	for _, sm := range top {
		if sm.Memory.ProjectID != nil && *sm.Memory.ProjectID == *projectID {
			return true
		}
	}
	return false
}

func filterByProject(scored []*domain.ScoredMemory, projectID *uuid.UUID) []*domain.ScoredMemory {
	if projectID == nil {
		return scored
	}
	var result []*domain.ScoredMemory
	for _, sm := range scored {
		if sm.Memory.ProjectID != nil && *sm.Memory.ProjectID == *projectID {
			result = append(result, sm)
		}
	}
	return result
}

func hasBM25MatchInTop(scored []*domain.ScoredMemory, topK int) bool {
	if topK > len(scored) {
		topK = len(scored)
	}
	for i := 0; i < topK; i++ {
		if scored[i].Memory.Metadata != nil {
			if rank, ok := scored[i].Memory.Metadata["_bm25_rank"]; ok {
				if r, ok := rank.(int); ok && r > 0 && r <= 5 {
					return true
				}
			}
		}
	}
	return false
}

func hasProjectMemoryInTop(scored []*domain.ScoredMemory, projectID *uuid.UUID, topK int, minSim float64) bool {
	if projectID == nil {
		return false
	}
	if topK > len(scored) {
		topK = len(scored)
	}
	for i := 0; i < topK; i++ {
		sm := scored[i]
		if sm.Memory.ProjectID != nil && *sm.Memory.ProjectID == *projectID && sm.Score.SemanticSimilarity >= minSim {
			return true
		}
	}
	return false
}

// normalizedEntropy computes H(p) / H_max for a set of similarity scores.
// Returns value in [0,1]. Close to 1.0 means uniform (noise), close to 0 means peaked (signal).
func normalizedEntropy(sims []float64) float64 {
	n := len(sims)
	if n <= 1 {
		return 0
	}
	total := 0.0
	for _, s := range sims {
		if s < 0 {
			s = 0
		}
		total += s
	}
	if total == 0 {
		return 1
	}
	H := 0.0
	for _, s := range sims {
		p := s / total
		if p > 0 {
			H -= p * math.Log2(p)
		}
	}
	Hmax := math.Log2(float64(n))
	if Hmax == 0 {
		return 0
	}
	return H / Hmax
}

var recallStopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "being": true, "have": true,
	"has": true, "had": true, "do": true, "does": true, "did": true,
	"will": true, "would": true, "could": true, "should": true, "may": true,
	"might": true, "shall": true, "can": true, "to": true, "of": true,
	"in": true, "for": true, "on": true, "with": true, "at": true, "by": true,
	"from": true, "as": true, "into": true, "through": true, "during": true,
	"before": true, "after": true, "above": true, "below": true, "between": true,
	"and": true, "but": true, "or": true, "nor": true, "not": true, "so": true,
	"yet": true, "both": true, "either": true, "neither": true, "each": true,
	"every": true, "all": true, "any": true, "few": true, "more": true,
	"most": true, "other": true, "some": true, "such": true, "no": true,
	"only": true, "own": true, "same": true, "than": true, "too": true,
	"very": true, "just": true, "how": true, "what": true, "which": true,
	"who": true, "whom": true, "this": true, "that": true, "these": true,
	"those": true, "i": true, "me": true, "my": true, "we": true, "our": true,
	"you": true, "your": true, "he": true, "she": true, "it": true, "they": true,
	"use": true, "using": true, "used": true, "best": true, "practices": true,
	"function": true, "model": true, "data": true, "custom": true, "get": true,
	"configure": true, "setup": true, "install": true, "create": true, "make": true,
	"build": true, "run": true, "start": true, "stop": true, "add": true,
	"work": true, "works": true, "working": true, "set": true, "setting": true,
	"update": true, "change": true, "tutorial": true, "guide": true, "example": true,
}

func extractKeywords(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	var keywords []string
	for _, w := range words {
		w = strings.Trim(w, ".,!?;:'\"()[]{}/-")
		if len(w) >= 3 && !recallStopWords[w] {
			keywords = append(keywords, w)
		}
	}
	return keywords
}

func countQueryKeywords(query string) int {
	return len(extractKeywords(query))
}

func simpleStem(word string) string {
	if len(word) <= 4 {
		return word
	}
	for _, suffix := range []string{"tion", "sion", "ment", "ness", "ity", "ing", "ies", "ied", "able", "ible", "ous", "ful", "less", "ence", "ance", "ers", "ure"} {
		if strings.HasSuffix(word, suffix) {
			stem := word[:len(word)-len(suffix)]
			if len(stem) >= 3 {
				return stem
			}
		}
	}
	for _, suffix := range []string{"es", "ed", "ly", "al", "er"} {
		if strings.HasSuffix(word, suffix) {
			stem := word[:len(word)-len(suffix)]
			if len(stem) >= 3 {
				return stem
			}
		}
	}
	if strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") {
		stem := word[:len(word)-1]
		if len(stem) >= 3 {
			return stem
		}
	}
	return word
}

func stemmedContains(content, keyword string) bool {
	stemmed := simpleStem(keyword)
	if len(keyword) <= 5 {
		contentWords := strings.Fields(content)
		for _, cw := range contentWords {
			cw = strings.Trim(cw, ".,!?;:'\"()[]{}/-")
			if cw == keyword || simpleStem(cw) == stemmed {
				return true
			}
		}
		return false
	}
	if strings.Contains(content, keyword) {
		return true
	}
	if stemmed != keyword && strings.Contains(content, stemmed) {
		return true
	}
	contentWords := strings.Fields(content)
	for _, cw := range contentWords {
		cw = strings.Trim(cw, ".,!?;:'\"()[]{}/-")
		if simpleStem(cw) == stemmed {
			return true
		}
	}
	return false
}

// queryKeywordsOverlapStemmed returns how many unique query keywords match
// content in top memories, using stemming for fuzzy matching.
// "dependency" matches "dependencies", "structured" matches "structure", etc.
func queryKeywordsOverlapStemmed(query string, topScored []*domain.ScoredMemory) int {
	keywords := extractKeywords(query)
	if len(keywords) == 0 {
		return 999
	}

	found := make(map[string]bool)
	for _, sm := range topScored {
		contentLower := strings.ToLower(sm.Memory.Content)
		for _, kw := range keywords {
			if !found[kw] && stemmedContains(contentLower, kw) {
				found[kw] = true
			}
		}
	}
	return len(found)
}

func (s *RecallService) assembleContext(selected []*domain.ScoredMemory, budget domain.TokenBudget) *domain.AssembledContext {
	if len(selected) == 0 {
		return &domain.AssembledContext{
			Text:       "",
			Sources:    nil,
			TokenCount: 0,
			Confidence: 0,
		}
	}

	// Confidence uses calibrated similarity. Raw nomic-embed-text cosine gives
	// 0.55-0.75 for good matches, which looks low to users. We apply sigmoid
	// calibration: midpoint=0.55 (average good match), steepness=8.
	// This maps: 0.40→25%, 0.55→50%, 0.65→73%, 0.75→89%, 0.85→97%.
	bestSim := 0.0
	for _, sm := range selected {
		if sm.Score.SemanticSimilarity > bestSim {
			bestSim = sm.Score.SemanticSimilarity
		}
	}
	calibrated := calibrateConfidence(bestSim)

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("## %d memories (confidence %.0f%%)\n\n", len(selected), calibrated*100))

	sources := make([]domain.RecallSource, 0, len(selected))
	totalTokens := 0

	for i, sm := range selected {
		content := sm.Memory.Content
		contentTokens := sm.Memory.TokenCount
		if sm.Memory.Summary != "" && sm.Memory.Summary != sm.Memory.Content {
			summaryTokens := estimateTokens(sm.Memory.Summary)
			// Memories at index >= 3: always prefer summary if available.
			// Top 3: use full content, fallback to summary only if it would bust budget.
			if i >= 3 || totalTokens+contentTokens > budget.Total {
				content = sm.Memory.Summary
				contentTokens = summaryTokens
			}
		}

		if totalTokens+contentTokens > budget.Total {
			break
		}

		// Header shows similarity (true relevance to query) separately from importance
		header := fmt.Sprintf("[%s | sim=%.0f%% | importance=%.2f | %s]",
			sm.Memory.Tier, sm.Score.SemanticSimilarity*100, sm.Memory.Importance, humanAge(sm.Memory.CreatedAt))
		if len(sm.Memory.Tags) > 0 {
			for _, tag := range sm.Memory.Tags {
				header += " #" + tag
			}
		}
		builder.WriteString(header + "\n")
		builder.WriteString(content + "\n\n")

		totalTokens += contentTokens

		snippet := content
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		sources = append(sources, domain.RecallSource{
			MemoryID:  sm.Memory.ID,
			Tier:      sm.Memory.Tier,
			Relevance: sm.Score.SemanticSimilarity,
			Snippet:   snippet,
		})
	}

	return &domain.AssembledContext{
		Text:       builder.String(),
		Sources:    sources,
		TokenCount: totalTokens,
		Confidence: calibrated,
	}
}

// calibrateConfidence applies sigmoid scaling to raw cosine similarity.
// nomic-embed-text returns 0.40-0.80 for relevant matches, which looks
// unreliably low to users. Sigmoid maps this to a more intuitive 0-1 range.
// Midpoint 0.55 = 50% confidence, steepness 8.
func calibrateConfidence(rawSim float64) float64 {
	const midpoint = 0.55
	const steepness = 10.0
	return 1.0 / (1.0 + math.Exp(-steepness*(rawSim-midpoint)))
}

func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return "<1h"
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// keywordOverlapScore returns a 0.0-1.0 score based on what fraction
// of query keywords appear in the memory content (using stemming).
// This breaks cosine similarity ties when many memories have similar embeddings.
func keywordOverlapScore(content string, queryKeywords []string) float64 {
	if len(queryKeywords) == 0 {
		return 0
	}
	contentLower := strings.ToLower(content)
	matched := 0
	for _, kw := range queryKeywords {
		if stemmedContains(contentLower, kw) {
			matched++
		}
	}
	return float64(matched) / float64(len(queryKeywords))
}


func dedup(items []*domain.MemoryItem) []*domain.MemoryItem {
	seen := make(map[uuid.UUID]bool, len(items))
	result := make([]*domain.MemoryItem, 0, len(items))
	for _, item := range items {
		if !seen[item.ID] {
			seen[item.ID] = true
			result = append(result, item)
		}
	}
	return result
}

// refreshAccessed boosts importance of recalled memories by a small factor,
// implementing "use it or lose it" — frequently recalled memories survive longer.
func (s *RecallService) refreshAccessed(ctx context.Context, selected []*domain.ScoredMemory) {
	for _, sm := range selected {
		boost := sm.Memory.Importance * 1.05
		if boost > 1.0 {
			boost = 1.0
		}
		if boost <= sm.Memory.Importance {
			continue
		}
		switch sm.Memory.Tier {
		case domain.TierEpisodic:
			_ = s.episodic.UpdateImportance(ctx, sm.Memory.ID, boost)
		case domain.TierSemantic:
			_ = s.semantic.UpdateImportance(ctx, sm.Memory.ID, boost)
		}
	}
}

func containsCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// translateToEnglish uses LLMProvider (if available) or falls back to direct Ollama.
// Timeout is tight (8s) — if LLM is slow, we fall back to the original.
// shouldExpandQuery checks if query expansion is needed based on candidate quality.
// Returns true if no candidate has similarity >= 0.72, meaning the original
// query phrasing may not match stored content.
func shouldExpandQuery(candidates []*domain.MemoryItem) bool {
	if len(candidates) == 0 {
		return true
	}
	for _, c := range candidates {
		if c.Similarity >= 0.72 {
			return false
		}
	}
	return true
}

// expandQuery asks LLM to rephrase the query using alternative terms.
// Only called when initial retrieval returns few results (<10 candidates),
// suggesting the original phrasing may not match stored content.
func (s *RecallService) expandQuery(ctx context.Context, query string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result, err := s.llm.Chat(ctx, []domain.ChatMessage{
		{Role: "user", Content: "Rephrase this search query using different words and synonyms. " +
			"Output ONLY the rephrased query, nothing else. Keep it concise (under 15 words).\n\nQuery: " + query},
	}, domain.ChatOptions{Temperature: 0.3, MaxTokens: 30})
	if err != nil {
		s.logger.Warn("query expansion failed", "error", err)
		return ""
	}
	result = strings.TrimSpace(result)
	if result == "" || strings.EqualFold(result, query) {
		return ""
	}
	return result
}

func (s *RecallService) translateToEnglish(ctx context.Context, text string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	if s.llm != nil {
		translated, err := s.llm.Chat(ctx, []domain.ChatMessage{
			{Role: "system", Content: "You are a translator. Translate the user's text to English. Output ONLY the English translation, no explanations."},
			{Role: "user", Content: text},
		}, domain.ChatOptions{Temperature: 0.1, MaxTokens: 200})
		if err != nil {
			s.logger.Warn("LLM translation failed, trying direct ollama", "error", err)
		} else {
			translated = strings.TrimSpace(translated)
			if translated != "" && len(translated) <= len(text)*5 {
				return translated, nil
			}
		}
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model": s.translationModel,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a translator. Translate the user's text to English. Output ONLY the English translation, no explanations."},
			{"role": "user", "content": text},
		},
		"stream": false,
		"options": map[string]any{
			"temperature": 0.1,
			"num_predict": 200,
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.ollamaBaseURL+"/api/chat", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	translated := strings.TrimSpace(result.Message.Content)
	if translated == "" || len(translated) > len(text)*5 {
		return "", fmt.Errorf("translation result suspicious: len=%d", len(translated))
	}
	return translated, nil
}
