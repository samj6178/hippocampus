package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// FusionEngine implements three-layer knowledge fusion using Dempster-Shafer
// evidence theory. Instead of simple cosine top-K, it combines evidence from:
//   - Layer 1: MOS curated knowledge (from research agents + code memory)
//   - Layer 2: External evidence (from Cursor's real-time web search)
//   - Layer 3: Historical patterns (causal chains, procedural success rates)
//
// Dempster's rule: m_combined(A) = Σ m1(B)·m2(C) / (1-K) where B∩C=A
// K = conflict measure = Σ m1(B)·m2(C) where B∩C=∅
type FusionEngine struct {
	retriever    *HybridRetriever
	crossEncoder *CrossEncoder
	causal       *CausalDetector
	procedural   *ProceduralService
	llm          domain.LLMProvider
	logger       *slog.Logger
}

func NewFusionEngine(
	retriever *HybridRetriever,
	crossEncoder *CrossEncoder,
	causal *CausalDetector,
	procedural *ProceduralService,
	llm domain.LLMProvider,
	logger *slog.Logger,
) *FusionEngine {
	return &FusionEngine{
		retriever:    retriever,
		crossEncoder: crossEncoder,
		causal:       causal,
		procedural:   procedural,
		llm:          llm,
		logger:       logger,
	}
}

// FusionRequest is the input to the three-layer fusion.
type FusionRequest struct {
	Query            string   `json:"query"`
	ExternalEvidence []string `json:"external_evidence,omitempty"`
	ProjectID        *uuid.UUID `json:"project_id,omitempty"`
	Budget           int      `json:"budget,omitempty"`
	Rerank           bool     `json:"rerank,omitempty"`
}

// FusionResponse is the output: ranked facts with calibrated confidence.
type FusionResponse struct {
	Facts      []FusedFact `json:"facts"`
	Insight    string      `json:"insight"`
	Conflict   float64     `json:"conflict"`
	Confidence float64     `json:"confidence"`
	Sources    []string    `json:"sources"`
}

// FusedFact is a single piece of evidence after Dempster-Shafer combination.
type FusedFact struct {
	Content     string   `json:"content"`
	Belief      float64  `json:"belief"`
	Uncertainty float64  `json:"uncertainty"`
	Provenance  []string `json:"provenance"`
	Agreement   int      `json:"agreement"`
}

// EvidenceMass represents a Dempster-Shafer mass assignment for one source.
type EvidenceMass struct {
	Belief      float64 // mass assigned to "fact is true"
	Disbelief   float64 // mass assigned to "fact is false"
	Uncertainty float64 // remaining mass (ignorance)
	Source      string
	Content     string
}

// Fuse performs three-layer Dempster-Shafer knowledge fusion.
func (fe *FusionEngine) Fuse(ctx context.Context, req *FusionRequest) (*FusionResponse, error) {
	if req.Budget <= 0 {
		req.Budget = 4096
	}

	embedding, err := fe.retriever.embedding.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// Layer 1: MOS curated knowledge via hybrid retrieval
	hybridResults, err := fe.retriever.Retrieve(ctx, req.Query, embedding, req.ProjectID, 30)
	if err != nil {
		fe.logger.Warn("hybrid retrieval failed", "error", err)
	}

	if req.Rerank && fe.crossEncoder != nil && len(hybridResults) > 0 {
		hybridResults, _ = fe.crossEncoder.Rerank(ctx, req.Query, hybridResults, 15)
	}

	var allEvidence []EvidenceMass

	// Convert MOS results to evidence masses
	for _, hr := range hybridResults {
		belief := math.Min(hr.RRFScore*5, 0.9)
		if belief < 0.1 {
			belief = 0.1
		}
		allEvidence = append(allEvidence, EvidenceMass{
			Belief:      belief,
			Disbelief:   0.05,
			Uncertainty: 1.0 - belief - 0.05,
			Source:      "mos:" + string(hr.Memory.Tier),
			Content:     truncateForFusion(hr.Memory.Content, 300),
		})
	}

	// Layer 2: External evidence from Cursor web search
	for _, ext := range req.ExternalEvidence {
		allEvidence = append(allEvidence, EvidenceMass{
			Belief:      0.6,
			Disbelief:   0.1,
			Uncertainty: 0.3,
			Source:      "web:cursor",
			Content:     truncateForFusion(ext, 300),
		})
	}

	// Layer 3: Historical patterns (causal chains)
	if fe.causal != nil && len(hybridResults) > 0 {
		var ids []uuid.UUID
		for _, hr := range hybridResults[:min(5, len(hybridResults))] {
			ids = append(ids, hr.Memory.ID)
		}
		causalCtx := fe.causal.GetCausalContext(ctx, ids)
		if causalCtx != "" {
			allEvidence = append(allEvidence, EvidenceMass{
				Belief:      0.7,
				Disbelief:   0.05,
				Uncertainty: 0.25,
				Source:      "historical:causal",
				Content:     truncateForFusion(causalCtx, 300),
			})
		}
	}

	if len(allEvidence) == 0 {
		return &FusionResponse{
			Insight:    "No relevant evidence found from any source.",
			Confidence: 0,
		}, nil
	}

	// Group similar evidence and apply Dempster's combination rule
	facts, totalConflict := fe.combineEvidence(allEvidence)

	// Sort by belief (descending)
	sort.Slice(facts, func(i, j int) bool {
		return facts[i].Belief > facts[j].Belief
	})

	if len(facts) > 10 {
		facts = facts[:10]
	}

	// Compute overall confidence
	overallConfidence := 0.0
	if len(facts) > 0 {
		for _, f := range facts {
			overallConfidence += f.Belief
		}
		overallConfidence /= float64(len(facts))
	}

	// Collect unique sources
	sourceSet := make(map[string]bool)
	for _, e := range allEvidence {
		sourceSet[e.Source] = true
	}
	var sources []string
	for s := range sourceSet {
		sources = append(sources, s)
	}

	// LLM synthesis
	insight := fe.synthesizeInsight(ctx, req.Query, facts, totalConflict)

	return &FusionResponse{
		Facts:      facts,
		Insight:    insight,
		Conflict:   totalConflict,
		Confidence: overallConfidence,
		Sources:    sources,
	}, nil
}

// combineEvidence groups similar evidence and applies Dempster's rule.
func (fe *FusionEngine) combineEvidence(evidence []EvidenceMass) ([]FusedFact, float64) {
	var facts []FusedFact
	used := make(map[int]bool)
	totalConflict := 0.0
	conflictCount := 0

	for i, e1 := range evidence {
		if used[i] {
			continue
		}
		used[i] = true

		fact := FusedFact{
			Content:    e1.Content,
			Belief:     e1.Belief,
			Uncertainty: e1.Uncertainty,
			Provenance: []string{e1.Source},
			Agreement:  1,
		}

		for j := i + 1; j < len(evidence); j++ {
			if used[j] {
				continue
			}

			e2 := evidence[j]
			similarity := contentSimilarity(e1.Content, e2.Content)

			if similarity > 0.3 {
				// Combine using Dempster's rule
				combined, conflict := dempsterCombine(
					EvidenceMass{Belief: fact.Belief, Disbelief: 1 - fact.Belief - fact.Uncertainty, Uncertainty: fact.Uncertainty},
					e2,
				)
				fact.Belief = combined.Belief
				fact.Uncertainty = combined.Uncertainty
				fact.Provenance = append(fact.Provenance, e2.Source)
				fact.Agreement++
				totalConflict += conflict
				conflictCount++
				used[j] = true
			}
		}

		facts = append(facts, fact)
	}

	avgConflict := 0.0
	if conflictCount > 0 {
		avgConflict = totalConflict / float64(conflictCount)
	}

	return facts, avgConflict
}

// dempsterCombine applies Dempster's rule of combination for two evidence masses.
func dempsterCombine(m1, m2 EvidenceMass) (EvidenceMass, float64) {
	// Conflict: K = m1(T)·m2(F) + m1(F)·m2(T)
	K := m1.Belief*m2.Disbelief + m1.Disbelief*m2.Belief

	if K >= 1.0 {
		return EvidenceMass{
			Belief:      0.5,
			Disbelief:   0.5,
			Uncertainty: 0.0,
		}, 1.0
	}

	norm := 1.0 / (1.0 - K)

	// Combined belief = norm * (m1(T)·m2(T) + m1(T)·m2(Θ) + m1(Θ)·m2(T))
	belief := norm * (m1.Belief*m2.Belief + m1.Belief*m2.Uncertainty + m1.Uncertainty*m2.Belief)

	// Combined disbelief = norm * (m1(F)·m2(F) + m1(F)·m2(Θ) + m1(Θ)·m2(F))
	disbelief := norm * (m1.Disbelief*m2.Disbelief + m1.Disbelief*m2.Uncertainty + m1.Uncertainty*m2.Disbelief)

	// Combined uncertainty = norm * m1(Θ)·m2(Θ)
	uncertainty := norm * m1.Uncertainty * m2.Uncertainty

	// Normalize to sum=1
	total := belief + disbelief + uncertainty
	if total > 0 {
		belief /= total
		disbelief /= total
		uncertainty /= total
	}

	return EvidenceMass{
		Belief:      belief,
		Disbelief:   disbelief,
		Uncertainty: uncertainty,
	}, K
}

// contentSimilarity is a fast word-overlap similarity (Jaccard-like).
func contentSimilarity(a, b string) float64 {
	wordsA := strings.Fields(strings.ToLower(a))
	wordsB := strings.Fields(strings.ToLower(b))

	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0
	}

	setA := make(map[string]bool, len(wordsA))
	for _, w := range wordsA {
		if len(w) > 2 {
			setA[w] = true
		}
	}

	intersection := 0
	setB := make(map[string]bool, len(wordsB))
	for _, w := range wordsB {
		if len(w) > 2 {
			setB[w] = true
			if setA[w] {
				intersection++
			}
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func (fe *FusionEngine) synthesizeInsight(ctx context.Context, query string, facts []FusedFact, conflict float64) string {
	if fe.llm == nil || len(facts) == 0 {
		return fe.fallbackSynthesis(facts, conflict)
	}

	var factsText strings.Builder
	for i, f := range facts {
		factsText.WriteString(fmt.Sprintf("%d. [belief=%.2f, sources=%d] %s\n",
			i+1, f.Belief, f.Agreement, f.Content))
	}

	conflictNote := ""
	if conflict > 0.3 {
		conflictNote = "\nWARNING: High conflict detected between sources. Note contradictions explicitly."
	}

	prompt := fmt.Sprintf(`Synthesize the following evidence into a concise, actionable insight for the query.

Query: %s

Evidence (ranked by combined belief):
%s
Conflict level: %.2f%s

Produce:
1. SYNTHESIS: One paragraph combining key evidence
2. CONFIDENCE: How confident are we? (based on agreement between sources)
3. ACTION: What should be done based on this evidence?
4. GAPS: What's still unknown?`, query, factsText.String(), conflict, conflictNote)

	result, err := fe.llm.Chat(ctx, []domain.ChatMessage{
		{Role: "user", Content: prompt},
	}, domain.ChatOptions{Temperature: 0.3, MaxTokens: 800})
	if err != nil {
		fe.logger.Warn("fusion synthesis LLM failed", "error", err)
		return fe.fallbackSynthesis(facts, conflict)
	}

	return result
}

func (fe *FusionEngine) fallbackSynthesis(facts []FusedFact, conflict float64) string {
	if len(facts) == 0 {
		return "No evidence found."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d evidence clusters (conflict=%.2f):\n\n", len(facts), conflict))
	for i, f := range facts {
		if i >= 5 {
			break
		}
		sb.WriteString(fmt.Sprintf("- [belief=%.2f] %s\n", f.Belief, truncateForFusion(f.Content, 200)))
	}
	return sb.String()
}

func truncateForFusion(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

