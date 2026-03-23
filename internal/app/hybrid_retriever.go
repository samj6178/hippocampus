package app

import (
	"context"
	"log/slog"
	"sort"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// HybridRetriever combines vector search (cosine similarity) with BM25 (keyword)
// via Reciprocal Rank Fusion. This is the industry standard approach used by
// Anthropic, Google, OpenAI for production RAG systems.
//
// RRF formula: score(d) = w_vec/(k + rank_vec(d)) + w_bm25/(k + rank_bm25(d))
// where k=60 (standard constant), w_vec=0.6, w_bm25=0.4
type HybridRetriever struct {
	episodic   domain.EpisodicRepo
	semantic   domain.SemanticRepo
	procedural domain.ProceduralRepo
	embedding  domain.EmbeddingProvider
	logger     *slog.Logger

	rrfK     float64
	wVector  float64
	wBM25    float64
}

func NewHybridRetriever(
	episodic domain.EpisodicRepo,
	semantic domain.SemanticRepo,
	procedural domain.ProceduralRepo,
	embedding domain.EmbeddingProvider,
	logger *slog.Logger,
) *HybridRetriever {
	return &HybridRetriever{
		episodic:   episodic,
		semantic:   semantic,
		procedural: procedural,
		embedding:  embedding,
		logger:     logger,
		rrfK:       60.0,
		wVector:    0.6,
		wBM25:     0.4,
	}
}

type HybridResult struct {
	Memory   *domain.MemoryItem
	VecRank  int
	BM25Rank int
	RRFScore float64
}

// Retrieve performs hybrid search: vector + BM25, fused with RRF.
func (hr *HybridRetriever) Retrieve(
	ctx context.Context,
	query string,
	queryEmb []float32,
	projectID *uuid.UUID,
	limit int,
) ([]HybridResult, error) {
	fetchLimit := limit * 3
	if fetchLimit < 50 {
		fetchLimit = 50
	}

	vecResults, bm25Results, err := hr.fetchBothSignals(ctx, query, queryEmb, projectID, fetchLimit)
	if err != nil {
		return nil, err
	}

	fused := hr.fuseRRF(vecResults, bm25Results)

	if len(fused) > limit {
		fused = fused[:limit]
	}

	hr.logger.Debug("hybrid retrieval",
		"query_len", len(query),
		"vec_results", len(vecResults),
		"bm25_results", len(bm25Results),
		"fused", len(fused),
	)

	return fused, nil
}

func (hr *HybridRetriever) fetchBothSignals(
	ctx context.Context,
	query string,
	queryEmb []float32,
	projectID *uuid.UUID,
	limit int,
) ([]*domain.MemoryItem, []*domain.MemoryItem, error) {
	type vecResult struct {
		items []*domain.MemoryItem
		err   error
	}
	type bm25Result struct {
		items []*domain.MemoryItem
		err   error
	}

	vecCh := make(chan vecResult, 1)
	bm25Ch := make(chan bm25Result, 1)

	go func() {
		var items []*domain.MemoryItem

		epis, err := hr.episodic.SearchSimilar(ctx, queryEmb, projectID, limit)
		if err == nil {
			for _, e := range epis {
				items = append(items, episodicToItem(e))
			}
		}

		sems, err2 := hr.semantic.SearchSimilar(ctx, queryEmb, projectID, limit)
		if err2 == nil {
			for _, s := range sems {
				items = append(items, semanticToItem(s))
			}
		}

		if err != nil && err2 != nil {
			vecCh <- vecResult{nil, err}
		} else {
			vecCh <- vecResult{items, nil}
		}
	}()

	go func() {
		var items []*domain.MemoryItem

		epis, err := hr.episodic.SearchBM25(ctx, query, projectID, limit)
		if err == nil {
			for _, e := range epis {
				items = append(items, episodicToItem(e))
			}
		}

		sems, err2 := hr.semantic.SearchBM25(ctx, query, projectID, limit)
		if err2 == nil {
			for _, s := range sems {
				items = append(items, semanticToItem(s))
			}
		}

		if err != nil && err2 != nil {
			bm25Ch <- bm25Result{nil, err}
		} else {
			bm25Ch <- bm25Result{items, nil}
		}
	}()

	vr := <-vecCh
	br := <-bm25Ch

	if vr.err != nil && br.err != nil {
		return nil, nil, vr.err
	}

	return vr.items, br.items, nil
}

func (hr *HybridRetriever) fuseRRF(vecResults, bm25Results []*domain.MemoryItem) []HybridResult {
	type entry struct {
		memory   *domain.MemoryItem
		vecRank  int
		bm25Rank int
	}

	merged := make(map[uuid.UUID]*entry)

	for rank, item := range vecResults {
		id := item.ID
		if e, ok := merged[id]; ok {
			e.vecRank = rank + 1
		} else {
			merged[id] = &entry{memory: item, vecRank: rank + 1, bm25Rank: 0}
		}
	}

	for rank, item := range bm25Results {
		id := item.ID
		if e, ok := merged[id]; ok {
			e.bm25Rank = rank + 1
		} else {
			merged[id] = &entry{memory: item, vecRank: 0, bm25Rank: rank + 1}
		}
	}

	maxRank := len(vecResults) + len(bm25Results) + 1

	var results []HybridResult
	for _, e := range merged {
		vr := e.vecRank
		if vr == 0 {
			vr = maxRank
		}
		br := e.bm25Rank
		if br == 0 {
			br = maxRank
		}

		rrfScore := hr.wVector/(hr.rrfK+float64(vr)) + hr.wBM25/(hr.rrfK+float64(br))

		results = append(results, HybridResult{
			Memory:   e.memory,
			VecRank:  e.vecRank,
			BM25Rank: e.bm25Rank,
			RRFScore: rrfScore,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].RRFScore > results[j].RRFScore
	})

	return results
}

func episodicToItem(e *domain.EpisodicMemory) *domain.MemoryItem {
	return &domain.MemoryItem{
		ID:           e.ID,
		ProjectID:    e.ProjectID,
		Tier:         domain.TierEpisodic,
		Content:      e.Content,
		Summary:      e.Summary,
		Embedding:    e.Embedding,
		Importance:   e.Importance,
		Confidence:   e.Confidence,
		AccessCount:  e.AccessCount,
		TokenCount:   e.TokenCount,
		LastAccessed: e.LastAccessed,
		CreatedAt:    e.CreatedAt,
		Tags:         e.Tags,
		Metadata:     e.Metadata,
		Similarity:   e.Similarity,
	}
}

func semanticToItem(s *domain.SemanticMemory) *domain.MemoryItem {
	return &domain.MemoryItem{
		ID:           s.ID,
		ProjectID:    s.ProjectID,
		Tier:         domain.TierSemantic,
		Content:      s.Content,
		Summary:      s.Summary,
		Embedding:    s.Embedding,
		Importance:   s.Importance,
		Confidence:   s.Confidence,
		AccessCount:  s.AccessCount,
		TokenCount:   s.TokenCount,
		LastAccessed: s.LastAccessed,
		CreatedAt:    s.CreatedAt,
		Tags:         s.Tags,
		Metadata:     s.Metadata,
		Similarity:   s.Similarity,
	}
}
