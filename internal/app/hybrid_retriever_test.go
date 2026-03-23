package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

func testRetriever(ep domain.EpisodicRepo, sem domain.SemanticRepo) *HybridRetriever {
	return NewHybridRetriever(
		ep, sem, nil,
		&mockEmbedding{embeddings: [][]float32{{0.1, 0.2}}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

// --- Mocks specific to retriever tests ---

type retrieverEpisodicRepo struct {
	mockEpisodicRepo
	vecResults  []*domain.EpisodicMemory
	bm25Results []*domain.EpisodicMemory
	vecErr      error
	bm25Err     error
}

func (m *retrieverEpisodicRepo) SearchSimilar(_ context.Context, _ []float32, _ *uuid.UUID, _ int) ([]*domain.EpisodicMemory, error) {
	return m.vecResults, m.vecErr
}

func (m *retrieverEpisodicRepo) SearchBM25(_ context.Context, _ string, _ *uuid.UUID, _ int) ([]*domain.EpisodicMemory, error) {
	return m.bm25Results, m.bm25Err
}

type retrieverSemanticRepo struct {
	mockSemanticRepo
	vecResults  []*domain.SemanticMemory
	bm25Results []*domain.SemanticMemory
	vecErr      error
	bm25Err     error
}

func (m *retrieverSemanticRepo) SearchSimilar(_ context.Context, _ []float32, _ *uuid.UUID, _ int) ([]*domain.SemanticMemory, error) {
	return m.vecResults, m.vecErr
}

func (m *retrieverSemanticRepo) SearchBM25(_ context.Context, _ string, _ *uuid.UUID, _ int) ([]*domain.SemanticMemory, error) {
	return m.bm25Results, m.bm25Err
}

// --- fuseRRF tests ---

func TestFuseRRF_Empty(t *testing.T) {
	hr := testRetriever(&mockEpisodicRepo{}, &mockSemanticRepo{})
	result := hr.fuseRRF(nil, nil)
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result))
	}
}

func TestFuseRRF_OnlyVector(t *testing.T) {
	hr := testRetriever(&mockEpisodicRepo{}, &mockSemanticRepo{})
	id := uuid.New()
	vec := []*domain.MemoryItem{{ID: id, Content: "vec only"}}

	result := hr.fuseRRF(vec, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].VecRank != 1 {
		t.Errorf("VecRank = %d, want 1", result[0].VecRank)
	}
	if result[0].BM25Rank != 0 {
		t.Errorf("BM25Rank = %d, want 0 (absent)", result[0].BM25Rank)
	}
	// Missing BM25 rank should use maxRank penalty
	// score = 0.6/(60+1) + 0.4/(60+maxRank)
	// maxRank = len(vec)+len(bm25)+1 = 1+0+1 = 2
	expected := 0.6/(60.0+1.0) + 0.4/(60.0+2.0)
	if math.Abs(result[0].RRFScore-expected) > 1e-9 {
		t.Errorf("RRFScore = %f, want %f", result[0].RRFScore, expected)
	}
}

func TestFuseRRF_OnlyBM25(t *testing.T) {
	hr := testRetriever(&mockEpisodicRepo{}, &mockSemanticRepo{})
	id := uuid.New()
	bm25 := []*domain.MemoryItem{{ID: id, Content: "bm25 only"}}

	result := hr.fuseRRF(nil, bm25)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].VecRank != 0 {
		t.Errorf("VecRank = %d, want 0 (absent)", result[0].VecRank)
	}
	if result[0].BM25Rank != 1 {
		t.Errorf("BM25Rank = %d, want 1", result[0].BM25Rank)
	}
}

func TestFuseRRF_Dedup(t *testing.T) {
	hr := testRetriever(&mockEpisodicRepo{}, &mockSemanticRepo{})
	id := uuid.New()
	item := &domain.MemoryItem{ID: id, Content: "shared"}

	result := hr.fuseRRF([]*domain.MemoryItem{item}, []*domain.MemoryItem{item})

	if len(result) != 1 {
		t.Fatalf("expected 1 deduplicated result, got %d", len(result))
	}
	if result[0].VecRank != 1 || result[0].BM25Rank != 1 {
		t.Errorf("ranks = (%d, %d), want (1, 1)", result[0].VecRank, result[0].BM25Rank)
	}
	// Both rank 1: score = 0.6/(60+1) + 0.4/(60+1) = 1.0/61
	expected := 1.0 / 61.0
	if math.Abs(result[0].RRFScore-expected) > 1e-9 {
		t.Errorf("RRFScore = %f, want %f", result[0].RRFScore, expected)
	}
}

func TestFuseRRF_Ordering(t *testing.T) {
	hr := testRetriever(&mockEpisodicRepo{}, &mockSemanticRepo{})
	id1 := uuid.New()
	id2 := uuid.New()
	id3 := uuid.New()

	// id1: vec rank 1, bm25 rank 1 (best)
	// id2: vec rank 2, no bm25 (medium)
	// id3: no vec, bm25 rank 2 (worst — bm25 weight is lower)
	vec := []*domain.MemoryItem{
		{ID: id1, Content: "a"},
		{ID: id2, Content: "b"},
	}
	bm25 := []*domain.MemoryItem{
		{ID: id1, Content: "a"},
		{ID: id3, Content: "c"},
	}

	result := hr.fuseRRF(vec, bm25)

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	if result[0].Memory.ID != id1 {
		t.Errorf("first result should be id1 (in both lists)")
	}
	for i := 1; i < len(result); i++ {
		if result[i].RRFScore > result[i-1].RRFScore {
			t.Errorf("results not sorted descending at index %d: %f > %f",
				i, result[i].RRFScore, result[i-1].RRFScore)
		}
	}
}

func TestFuseRRF_ScoreCalculation(t *testing.T) {
	hr := testRetriever(&mockEpisodicRepo{}, &mockSemanticRepo{})
	id1 := uuid.New()
	id2 := uuid.New()

	vec := []*domain.MemoryItem{
		{ID: id1, Content: "first"},
		{ID: id2, Content: "second"},
	}
	bm25 := []*domain.MemoryItem{
		{ID: id2, Content: "second"},
	}

	result := hr.fuseRRF(vec, bm25)

	scores := make(map[uuid.UUID]HybridResult)
	for _, r := range result {
		scores[r.Memory.ID] = r
	}

	// id1: vecRank=1, bm25Rank absent → maxRank = 2+1+1 = 4
	// score = 0.6/(60+1) + 0.4/(60+4) = 0.6/61 + 0.4/64
	r1 := scores[id1]
	expected1 := 0.6/61.0 + 0.4/64.0
	if math.Abs(r1.RRFScore-expected1) > 1e-9 {
		t.Errorf("id1 score = %f, want %f", r1.RRFScore, expected1)
	}

	// id2: vecRank=2, bm25Rank=1
	// score = 0.6/(60+2) + 0.4/(60+1) = 0.6/62 + 0.4/61
	r2 := scores[id2]
	expected2 := 0.6/62.0 + 0.4/61.0
	if math.Abs(r2.RRFScore-expected2) > 1e-9 {
		t.Errorf("id2 score = %f, want %f", r2.RRFScore, expected2)
	}
}

func TestFuseRRF_ManyItems(t *testing.T) {
	hr := testRetriever(&mockEpisodicRepo{}, &mockSemanticRepo{})

	n := 20
	vec := make([]*domain.MemoryItem, n)
	bm25 := make([]*domain.MemoryItem, n)
	for i := 0; i < n; i++ {
		vec[i] = &domain.MemoryItem{ID: uuid.New(), Content: "v"}
		bm25[i] = &domain.MemoryItem{ID: uuid.New(), Content: "b"}
	}

	result := hr.fuseRRF(vec, bm25)

	if len(result) != 2*n {
		t.Fatalf("expected %d results, got %d", 2*n, len(result))
	}
	for i := 1; i < len(result); i++ {
		if result[i].RRFScore > result[i-1].RRFScore {
			t.Errorf("not sorted at index %d", i)
		}
	}
}

// --- Converter tests ---

func TestEpisodicToItem(t *testing.T) {
	now := time.Now()
	pid := uuid.New()
	ep := &domain.EpisodicMemory{
		MemoryItem: domain.MemoryItem{
			ID:           uuid.New(),
			ProjectID:    &pid,
			Content:      "test content",
			Summary:      "summary",
			Embedding:    []float32{0.1, 0.2, 0.3},
			Importance:   0.8,
			Confidence:   0.9,
			AccessCount:  5,
			TokenCount:   42,
			LastAccessed: now,
			CreatedAt:    now,
			Tags:         []string{"go", "test"},
			Metadata:     domain.Metadata{"key": "val"},
			Similarity:   0.95,
		},
	}

	item := episodicToItem(ep)

	if item.ID != ep.ID {
		t.Error("ID mismatch")
	}
	if item.Tier != domain.TierEpisodic {
		t.Errorf("Tier = %s, want episodic", item.Tier)
	}
	if item.Content != ep.Content {
		t.Error("Content mismatch")
	}
	if item.Importance != ep.Importance {
		t.Error("Importance mismatch")
	}
	if item.Similarity != ep.Similarity {
		t.Error("Similarity mismatch")
	}
	if len(item.Embedding) != 3 {
		t.Error("Embedding not copied")
	}
	if len(item.Tags) != 2 {
		t.Error("Tags not copied")
	}
}

func TestSemanticToItem(t *testing.T) {
	now := time.Now()
	sem := &domain.SemanticMemory{
		MemoryItem: domain.MemoryItem{
			ID:         uuid.New(),
			Content:    "semantic fact",
			Importance: 0.7,
			Confidence: 0.85,
			CreatedAt:  now,
			Similarity: 0.88,
		},
	}

	item := semanticToItem(sem)

	if item.ID != sem.ID {
		t.Error("ID mismatch")
	}
	if item.Tier != domain.TierSemantic {
		t.Errorf("Tier = %s, want semantic", item.Tier)
	}
	if item.Confidence != 0.85 {
		t.Errorf("Confidence = %f, want 0.85", item.Confidence)
	}
}

// --- Retrieve integration tests ---

func TestRetrieve_LimitEnforced(t *testing.T) {
	ids := make([]uuid.UUID, 10)
	epVec := make([]*domain.EpisodicMemory, 10)
	for i := range ids {
		ids[i] = uuid.New()
		epVec[i] = &domain.EpisodicMemory{
			MemoryItem: domain.MemoryItem{ID: ids[i], Content: "item"},
		}
	}

	ep := &retrieverEpisodicRepo{vecResults: epVec}
	sem := &retrieverSemanticRepo{}
	hr := testRetriever(ep, sem)

	results, err := hr.Retrieve(context.Background(), "query", []float32{0.1}, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Errorf("got %d results, want 3 (limit)", len(results))
	}
}

func TestRetrieve_FetchLimitMinimum(t *testing.T) {
	// limit=1 → fetchLimit should be max(1*3, 50) = 50
	// We verify by checking that the repo receives limit=50
	var receivedLimit int
	ep := &retrieverEpisodicRepo{}
	sem := &retrieverSemanticRepo{}
	// Override SearchSimilar to capture limit
	origEp := &limitCapturingEpisodicRepo{limit: &receivedLimit}

	hr := testRetriever(origEp, sem)
	_, _ = hr.Retrieve(context.Background(), "q", []float32{0.1}, nil, 1)

	if receivedLimit < 50 {
		t.Errorf("fetchLimit = %d, want >= 50", receivedLimit)
	}

	// limit=100 → fetchLimit = max(300, 50) = 300
	receivedLimit = 0
	_, _ = hr.Retrieve(context.Background(), "q", []float32{0.1}, nil, 100)
	if receivedLimit != 300 {
		t.Errorf("fetchLimit = %d, want 300", receivedLimit)
	}

	_ = ep // suppress unused
}

type limitCapturingEpisodicRepo struct {
	mockEpisodicRepo
	limit *int
}

func (m *limitCapturingEpisodicRepo) SearchSimilar(_ context.Context, _ []float32, _ *uuid.UUID, limit int) ([]*domain.EpisodicMemory, error) {
	*m.limit = limit
	return nil, nil
}

func (m *limitCapturingEpisodicRepo) SearchBM25(_ context.Context, _ string, _ *uuid.UUID, limit int) ([]*domain.EpisodicMemory, error) {
	*m.limit = limit
	return nil, nil
}

func TestRetrieve_BothSignalsFail(t *testing.T) {
	ep := &retrieverEpisodicRepo{
		vecErr:  errors.New("vec fail"),
		bm25Err: errors.New("bm25 fail"),
	}
	sem := &retrieverSemanticRepo{
		vecErr:  errors.New("vec fail"),
		bm25Err: errors.New("bm25 fail"),
	}
	hr := testRetriever(ep, sem)

	_, err := hr.Retrieve(context.Background(), "q", []float32{0.1}, nil, 10)
	if err == nil {
		t.Fatal("expected error when both signals fail")
	}
}

func TestRetrieve_PartialFailureReturnsResults(t *testing.T) {
	id := uuid.New()
	ep := &retrieverEpisodicRepo{
		vecResults: []*domain.EpisodicMemory{
			{MemoryItem: domain.MemoryItem{ID: id, Content: "survived"}},
		},
		bm25Err: errors.New("bm25 down"),
	}
	sem := &retrieverSemanticRepo{
		vecErr:  errors.New("sem vec down"),
		bm25Err: errors.New("sem bm25 down"),
	}
	hr := testRetriever(ep, sem)

	results, err := hr.Retrieve(context.Background(), "q", []float32{0.1}, nil, 10)
	if err != nil {
		t.Fatalf("partial failure should not error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result from surviving signal, got %d", len(results))
	}
	if results[0].Memory.ID != id {
		t.Error("wrong result returned")
	}
}

func TestRetrieve_EmptyResults(t *testing.T) {
	hr := testRetriever(&retrieverEpisodicRepo{}, &retrieverSemanticRepo{})

	results, err := hr.Retrieve(context.Background(), "q", []float32{0.1}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestRetrieve_MixedEpisodicAndSemantic(t *testing.T) {
	epID := uuid.New()
	semID := uuid.New()

	ep := &retrieverEpisodicRepo{
		vecResults: []*domain.EpisodicMemory{
			{MemoryItem: domain.MemoryItem{ID: epID, Content: "episodic"}},
		},
	}
	sem := &retrieverSemanticRepo{
		bm25Results: []*domain.SemanticMemory{
			{MemoryItem: domain.MemoryItem{ID: semID, Content: "semantic"}},
		},
	}
	hr := testRetriever(ep, sem)

	results, err := hr.Retrieve(context.Background(), "q", []float32{0.1}, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (1 ep + 1 sem), got %d", len(results))
	}
}
