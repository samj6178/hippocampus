package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// CrossEncoder performs LLM-based reranking of retrieval results.
// After the initial hybrid retrieval (BM25 + vector + RRF) produces top-50 candidates,
// the CrossEncoder evaluates each (query, document) pair using the LLM as a judge,
// producing much more accurate relevance scores than embedding cosine alone.
//
// This is the approach used in state-of-the-art RAG systems (ColBERT, BEIR benchmarks).
type CrossEncoder struct {
	llm    domain.LLMProvider
	logger *slog.Logger
}

func NewCrossEncoder(llm domain.LLMProvider, logger *slog.Logger) *CrossEncoder {
	return &CrossEncoder{llm: llm, logger: logger}
}

// Rerank takes hybrid retrieval results and reranks them using LLM-as-judge.
// Processes candidates in batches to minimize LLM calls.
func (ce *CrossEncoder) Rerank(ctx context.Context, query string, candidates []HybridResult, topK int) ([]HybridResult, error) {
	if ce.llm == nil || len(candidates) == 0 {
		return candidates, nil
	}

	if topK <= 0 || topK > len(candidates) {
		topK = len(candidates)
	}

	batchSize := 5
	scored := make([]scoredHybrid, 0, len(candidates))

	for i := 0; i < len(candidates); i += batchSize {
		end := i + batchSize
		if end > len(candidates) {
			end = len(candidates)
		}
		batch := candidates[i:end]

		scores, err := ce.scoreBatch(ctx, query, batch)
		if err != nil {
			ce.logger.Warn("cross-encoder batch failed, using RRF scores", "error", err, "batch_start", i)
			for _, c := range batch {
				scored = append(scored, scoredHybrid{result: c, ceScore: c.RRFScore})
			}
			continue
		}

		for j, c := range batch {
			ceScore := scores[j]
			finalScore := 0.4*c.RRFScore*100 + 0.6*ceScore
			scored = append(scored, scoredHybrid{result: c, ceScore: finalScore})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].ceScore > scored[j].ceScore
	})

	results := make([]HybridResult, 0, topK)
	for i := 0; i < topK && i < len(scored); i++ {
		r := scored[i].result
		r.RRFScore = scored[i].ceScore / 100.0
		results = append(results, r)
	}

	ce.logger.Debug("cross-encoder reranked", "candidates", len(candidates), "topK", topK)
	return results, nil
}

type scoredHybrid struct {
	result  HybridResult
	ceScore float64
}

func (ce *CrossEncoder) scoreBatch(ctx context.Context, query string, batch []HybridResult) ([]float64, error) {
	var prompt strings.Builder
	prompt.WriteString("Rate the relevance of each document to the query on a scale of 0-10.\n")
	prompt.WriteString("Output ONLY the scores as comma-separated numbers, nothing else.\n\n")
	prompt.WriteString(fmt.Sprintf("Query: %s\n\n", query))

	for i, c := range batch {
		content := c.Memory.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		prompt.WriteString(fmt.Sprintf("Document %d: %s\n\n", i+1, content))
	}

	prompt.WriteString(fmt.Sprintf("Scores (exactly %d comma-separated numbers 0-10):", len(batch)))

	result, err := ce.llm.Chat(ctx, []domain.ChatMessage{
		{Role: "user", Content: prompt.String()},
	}, domain.ChatOptions{Temperature: 0.1, MaxTokens: 50})
	if err != nil {
		return nil, fmt.Errorf("cross-encoder LLM call: %w", err)
	}

	return parseScores(result, len(batch))
}

func parseScores(raw string, expected int) ([]float64, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "[]")

	parts := strings.Split(raw, ",")
	scores := make([]float64, 0, expected)

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		s, err := strconv.ParseFloat(p, 64)
		if err != nil {
			continue
		}
		if s < 0 {
			s = 0
		}
		if s > 10 {
			s = 10
		}
		scores = append(scores, s)
	}

	for len(scores) < expected {
		scores = append(scores, 5.0)
	}

	return scores[:expected], nil
}
