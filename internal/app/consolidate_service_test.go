package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/pkg/vecutil"
)

// --- Mocks ---

type mockEpisodicRepo struct {
	unconsolidated []*domain.EpisodicMemory
	listErr        error
	markErr        error
	markedIDs      []uuid.UUID
	decayedCount   int
	decayErr       error
}

func (m *mockEpisodicRepo) ListUnconsolidated(_ context.Context, _ *uuid.UUID, _ int) ([]*domain.EpisodicMemory, error) {
	return m.unconsolidated, m.listErr
}

func (m *mockEpisodicRepo) MarkConsolidated(_ context.Context, ids []uuid.UUID) error {
	m.markedIDs = append(m.markedIDs, ids...)
	return m.markErr
}

func (m *mockEpisodicRepo) DecayImportance(_ context.Context, _ time.Duration, _ float64, _ float64) (int, error) {
	return m.decayedCount, m.decayErr
}

func (m *mockEpisodicRepo) Insert(context.Context, *domain.EpisodicMemory) error            { return nil }
func (m *mockEpisodicRepo) GetByID(context.Context, uuid.UUID) (*domain.EpisodicMemory, error) {
	return nil, nil
}
func (m *mockEpisodicRepo) SearchSimilar(context.Context, []float32, *uuid.UUID, int) ([]*domain.EpisodicMemory, error) {
	return nil, nil
}
func (m *mockEpisodicRepo) SearchBM25(context.Context, string, *uuid.UUID, int) ([]*domain.EpisodicMemory, error) {
	return nil, nil
}
func (m *mockEpisodicRepo) ListBySession(context.Context, uuid.UUID) ([]*domain.EpisodicMemory, error) {
	return nil, nil
}
func (m *mockEpisodicRepo) UpdateImportance(context.Context, uuid.UUID, float64) error { return nil }
func (m *mockEpisodicRepo) ListByTags(context.Context, *uuid.UUID, []string, int) ([]*domain.EpisodicMemory, error) {
	return nil, nil
}
func (m *mockEpisodicRepo) Delete(context.Context, uuid.UUID) error          { return nil }
func (m *mockEpisodicRepo) Count(context.Context, *uuid.UUID) (int, error)   { return 0, nil }

type mockSemanticRepo struct {
	inserted     []*domain.SemanticMemory
	insertErr    error
	decayedCount int
	decayErr     error
}

func (m *mockSemanticRepo) Insert(_ context.Context, mem *domain.SemanticMemory) error {
	m.inserted = append(m.inserted, mem)
	return m.insertErr
}

func (m *mockSemanticRepo) DecayImportance(_ context.Context, _ time.Duration, _ float64, _ float64) (int, error) {
	return m.decayedCount, m.decayErr
}

func (m *mockSemanticRepo) GetByID(context.Context, uuid.UUID) (*domain.SemanticMemory, error) {
	return nil, nil
}
func (m *mockSemanticRepo) SearchSimilar(context.Context, []float32, *uuid.UUID, int) ([]*domain.SemanticMemory, error) {
	return nil, nil
}
func (m *mockSemanticRepo) SearchBM25(context.Context, string, *uuid.UUID, int) ([]*domain.SemanticMemory, error) {
	return nil, nil
}
func (m *mockSemanticRepo) SearchGlobal(context.Context, []float32, int) ([]*domain.SemanticMemory, error) {
	return nil, nil
}
func (m *mockSemanticRepo) ListByProject(context.Context, *uuid.UUID, int) ([]*domain.SemanticMemory, error) {
	return nil, nil
}
func (m *mockSemanticRepo) ListGlobal(context.Context, int) ([]*domain.SemanticMemory, error) {
	return nil, nil
}
func (m *mockSemanticRepo) ListByEntityType(context.Context, *uuid.UUID, string, int) ([]*domain.SemanticMemory, error) {
	return nil, nil
}
func (m *mockSemanticRepo) Update(context.Context, *domain.SemanticMemory) error        { return nil }
func (m *mockSemanticRepo) UpdateImportance(context.Context, uuid.UUID, float64) error  { return nil }
func (m *mockSemanticRepo) Delete(context.Context, uuid.UUID) error                     { return nil }
func (m *mockSemanticRepo) Count(context.Context, *uuid.UUID) (int, error)              { return 0, nil }

type mockEmbedding struct {
	embeddings [][]float32
	err        error
}

func (m *mockEmbedding) Embed(_ context.Context, _ string) ([]float32, error) {
	if len(m.embeddings) > 0 {
		return m.embeddings[0], m.err
	}
	return nil, m.err
}

func (m *mockEmbedding) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	if len(m.embeddings) >= len(texts) {
		return m.embeddings[:len(texts)], nil
	}
	result := make([][]float32, len(texts))
	for i := range texts {
		if i < len(m.embeddings) {
			result[i] = m.embeddings[i]
		} else {
			result[i] = []float32{0, 0, 0}
		}
	}
	return result, nil
}

func (m *mockEmbedding) Dimensions() int  { return 3 }
func (m *mockEmbedding) ModelID() string   { return "test-model" }

// --- Helpers ---

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func makeEpisode(content string, importance float64, tags []string, emb []float32) *domain.EpisodicMemory {
	return &domain.EpisodicMemory{
		MemoryItem: domain.MemoryItem{
			ID:           uuid.New(),
			Tier:         domain.TierEpisodic,
			Content:      content,
			Embedding:    emb,
			Importance:   importance,
			Confidence:   0.5,
			TokenCount:   len(content) / 4,
			LastAccessed: time.Now(),
			CreatedAt:    time.Now(),
			Tags:         tags,
		},
		AgentID:   "test-agent",
		SessionID: uuid.New(),
	}
}

func newService(epRepo *mockEpisodicRepo, semRepo *mockSemanticRepo, embProv *mockEmbedding, cfg ConsolidateConfig) *ConsolidateService {
	return NewConsolidateService(epRepo, semRepo, embProv, nil, cfg, testLogger())
}

// --- Run() tests ---

func TestRun_EmptyEpisodes(t *testing.T) {
	svc := newService(&mockEpisodicRepo{}, &mockSemanticRepo{}, &mockEmbedding{}, ConsolidateConfig{})
	result, err := svc.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.EpisodesProcessed != 0 {
		t.Errorf("expected 0 episodes, got %d", result.EpisodesProcessed)
	}
	if result.SemanticCreated != 0 {
		t.Errorf("expected 0 semantic, got %d", result.SemanticCreated)
	}
}

func TestRun_BelowMinClusterSize(t *testing.T) {
	ep := makeEpisode("single episode content", 0.5, nil, nil)
	svc := newService(
		&mockEpisodicRepo{unconsolidated: []*domain.EpisodicMemory{ep}},
		&mockSemanticRepo{},
		&mockEmbedding{embeddings: [][]float32{{1, 0, 0}}},
		ConsolidateConfig{MinClusterSize: 2},
	)
	result, err := svc.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.EpisodesProcessed != 1 {
		t.Errorf("expected 1 episode processed, got %d", result.EpisodesProcessed)
	}
	if result.SemanticCreated != 0 {
		t.Errorf("expected 0 semantic created, got %d", result.SemanticCreated)
	}
}

func TestRun_ListUnconsolidatedError(t *testing.T) {
	svc := newService(
		&mockEpisodicRepo{listErr: errors.New("db down")},
		&mockSemanticRepo{},
		&mockEmbedding{},
		ConsolidateConfig{},
	)
	_, err := svc.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "list unconsolidated") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRun_EmbeddingError(t *testing.T) {
	eps := []*domain.EpisodicMemory{
		makeEpisode("error episode 1", 0.5, []string{"error"}, nil),
		makeEpisode("error episode 2", 0.6, []string{"error"}, nil),
	}
	svc := newService(
		&mockEpisodicRepo{unconsolidated: eps},
		&mockSemanticRepo{},
		&mockEmbedding{err: errors.New("ollama unreachable")},
		ConsolidateConfig{},
	)
	_, err := svc.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "embed episodes") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRun_HappyPath_TwoSimilarEpisodes(t *testing.T) {
	eps := []*domain.EpisodicMemory{
		makeEpisode("ERROR: connection timeout to postgres database", 0.7, []string{"error"}, nil),
		makeEpisode("ERROR: connection refused to postgres at port 5432", 0.8, []string{"error"}, nil),
	}
	semRepo := &mockSemanticRepo{}
	epRepo := &mockEpisodicRepo{unconsolidated: eps}
	emb := &mockEmbedding{
		embeddings: [][]float32{
			{0.9, 0.1, 0.1},
			{0.85, 0.15, 0.1},
		},
	}
	svc := newService(epRepo, semRepo, emb, ConsolidateConfig{ClusterThreshold: 0.5})

	result, err := svc.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SemanticCreated != 1 {
		t.Errorf("expected 1 semantic created, got %d", result.SemanticCreated)
	}
	if result.EpisodesMarked != 2 {
		t.Errorf("expected 2 episodes marked, got %d", result.EpisodesMarked)
	}
	if len(semRepo.inserted) != 1 {
		t.Fatalf("expected 1 inserted semantic, got %d", len(semRepo.inserted))
	}

	sem := semRepo.inserted[0]
	if sem.EntityType != "bugfix" {
		t.Errorf("expected entity_type=bugfix, got %q", sem.EntityType)
	}
	if sem.Importance < 0.6 {
		t.Errorf("importance should be >= 0.6, got %f", sem.Importance)
	}
	if len(sem.SourceEpisodes) != 2 {
		t.Errorf("expected 2 source episodes, got %d", len(sem.SourceEpisodes))
	}
	// Confidence: n/(n+2) = 2/4 = 0.5
	expectedConf := 2.0 / 4.0
	if sem.Confidence != expectedConf {
		t.Errorf("expected confidence=%f, got %f", expectedConf, sem.Confidence)
	}
}

func TestRun_SingletonsSkipped(t *testing.T) {
	eps := []*domain.EpisodicMemory{
		makeEpisode("ERROR: connection timeout", 0.7, []string{"error"}, nil),
		makeEpisode("DECISION: use postgres over mysql", 0.8, []string{"decision"}, nil),
	}
	emb := &mockEmbedding{
		embeddings: [][]float32{
			{1, 0, 0},
			{0, 1, 0},
		},
	}
	semRepo := &mockSemanticRepo{}
	svc := newService(
		&mockEpisodicRepo{unconsolidated: eps},
		semRepo,
		emb,
		ConsolidateConfig{ClusterThreshold: 0.9},
	)
	result, err := svc.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SingletonsSkipped != 2 {
		t.Errorf("expected 2 singletons skipped, got %d", result.SingletonsSkipped)
	}
	if result.SemanticCreated != 0 {
		t.Errorf("expected 0 semantic created, got %d", result.SemanticCreated)
	}
	if len(semRepo.inserted) != 0 {
		t.Errorf("expected 0 inserts, got %d", len(semRepo.inserted))
	}
}

func TestRun_MarkConsolidatedError(t *testing.T) {
	eps := []*domain.EpisodicMemory{
		makeEpisode("ERROR: same error A", 0.7, []string{"error"}, nil),
		makeEpisode("ERROR: same error B", 0.8, []string{"error"}, nil),
	}
	svc := newService(
		&mockEpisodicRepo{unconsolidated: eps, markErr: errors.New("mark failed")},
		&mockSemanticRepo{},
		&mockEmbedding{embeddings: [][]float32{{0.9, 0.1, 0}, {0.85, 0.15, 0}}},
		ConsolidateConfig{ClusterThreshold: 0.5},
	)
	_, err := svc.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "mark consolidated") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRun_InsertSemanticError_ContinuesToNextCluster(t *testing.T) {
	eps := []*domain.EpisodicMemory{
		makeEpisode("ERROR: cluster1 member A", 0.7, []string{"error"}, nil),
		makeEpisode("ERROR: cluster1 member B", 0.8, []string{"error"}, nil),
		makeEpisode("DECISION: cluster2 member A with important architectural choice", 0.6, []string{"decision"}, nil),
		makeEpisode("DECISION: cluster2 member B decided to use different approach", 0.7, []string{"decision"}, nil),
	}
	callCount := 0
	semRepo := &mockSemanticRepo{}
	origInsert := semRepo.Insert
	_ = origInsert
	// First insert fails, second succeeds
	failOnceRepo := &failOnceSemanticRepo{inner: semRepo, failCount: &callCount}

	epRepo := &mockEpisodicRepo{unconsolidated: eps}
	emb := &mockEmbedding{
		embeddings: [][]float32{
			{0.9, 0.1, 0},
			{0.85, 0.15, 0},
			{0.1, 0.9, 0},
			{0.15, 0.85, 0},
		},
	}
	svc := NewConsolidateService(epRepo, failOnceRepo, emb, nil, ConsolidateConfig{ClusterThreshold: 0.5}, testLogger())
	result, err := svc.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// One cluster insert fails, the other succeeds
	if result.SemanticCreated < 1 {
		t.Errorf("expected at least 1 semantic created despite error, got %d", result.SemanticCreated)
	}
}

type failOnceSemanticRepo struct {
	inner     *mockSemanticRepo
	failCount *int
}

func (f *failOnceSemanticRepo) Insert(ctx context.Context, mem *domain.SemanticMemory) error {
	*f.failCount++
	if *f.failCount == 1 {
		return errors.New("db insert failed")
	}
	return f.inner.Insert(ctx, mem)
}
func (f *failOnceSemanticRepo) DecayImportance(ctx context.Context, d time.Duration, factor, floor float64) (int, error) {
	return f.inner.DecayImportance(ctx, d, factor, floor)
}
func (f *failOnceSemanticRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.SemanticMemory, error) {
	return nil, nil
}
func (f *failOnceSemanticRepo) SearchSimilar(context.Context, []float32, *uuid.UUID, int) ([]*domain.SemanticMemory, error) {
	return nil, nil
}
func (f *failOnceSemanticRepo) SearchBM25(context.Context, string, *uuid.UUID, int) ([]*domain.SemanticMemory, error) {
	return nil, nil
}
func (f *failOnceSemanticRepo) SearchGlobal(context.Context, []float32, int) ([]*domain.SemanticMemory, error) {
	return nil, nil
}
func (f *failOnceSemanticRepo) ListByProject(context.Context, *uuid.UUID, int) ([]*domain.SemanticMemory, error) {
	return nil, nil
}
func (f *failOnceSemanticRepo) ListGlobal(context.Context, int) ([]*domain.SemanticMemory, error) {
	return nil, nil
}
func (f *failOnceSemanticRepo) ListByEntityType(context.Context, *uuid.UUID, string, int) ([]*domain.SemanticMemory, error) {
	return nil, nil
}
func (f *failOnceSemanticRepo) Update(context.Context, *domain.SemanticMemory) error        { return nil }
func (f *failOnceSemanticRepo) UpdateImportance(context.Context, uuid.UUID, float64) error  { return nil }
func (f *failOnceSemanticRepo) Delete(context.Context, uuid.UUID) error                     { return nil }
func (f *failOnceSemanticRepo) Count(context.Context, *uuid.UUID) (int, error)              { return 0, nil }

func TestRun_AdaptiveThresholdForSmallDatasets(t *testing.T) {
	// With < 20 episodes, threshold is lowered by 0.04
	// Create 3 episodes that cluster at threshold 0.68 but NOT at 0.72
	eps := make([]*domain.EpisodicMemory, 3)
	embs := make([][]float32, 3)
	for i := range eps {
		eps[i] = makeEpisode(fmt.Sprintf("ERROR: similar error %d occurred", i), 0.5, []string{"error"}, nil)
		embs[i] = []float32{0.9 - float32(i)*0.05, 0.1 + float32(i)*0.03, 0.1}
	}

	semRepo := &mockSemanticRepo{}
	svc := newService(
		&mockEpisodicRepo{unconsolidated: eps},
		semRepo,
		&mockEmbedding{embeddings: embs},
		ConsolidateConfig{ClusterThreshold: 0.72},
	)
	result, err := svc.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.EpisodesProcessed != 3 {
		t.Errorf("expected 3 processed, got %d", result.EpisodesProcessed)
	}
}

func TestRun_WithProjectID(t *testing.T) {
	pid := uuid.New()
	eps := []*domain.EpisodicMemory{
		makeEpisode("ERROR: project-specific error alpha", 0.7, []string{"error"}, nil),
		makeEpisode("ERROR: project-specific error beta", 0.8, []string{"error"}, nil),
	}
	for _, ep := range eps {
		ep.ProjectID = &pid
	}
	semRepo := &mockSemanticRepo{}
	svc := newService(
		&mockEpisodicRepo{unconsolidated: eps},
		semRepo,
		&mockEmbedding{embeddings: [][]float32{{0.9, 0.1, 0}, {0.85, 0.15, 0}}},
		ConsolidateConfig{ClusterThreshold: 0.5},
	)
	result, err := svc.Run(context.Background(), &pid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProjectID == nil || *result.ProjectID != pid {
		t.Errorf("expected project_id=%s, got %v", pid, result.ProjectID)
	}
	if len(semRepo.inserted) > 0 && semRepo.inserted[0].ProjectID != nil {
		if *semRepo.inserted[0].ProjectID != pid {
			t.Errorf("semantic memory should have project_id=%s", pid)
		}
	}
}

// --- RunAll() tests ---

func TestRunAll_DelegatesAndDecays(t *testing.T) {
	epRepo := &mockEpisodicRepo{decayedCount: 5}
	semRepo := &mockSemanticRepo{decayedCount: 2}
	svc := newService(epRepo, semRepo, &mockEmbedding{}, ConsolidateConfig{})

	results, err := svc.RunAll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No episodes → no results
	if len(results) != 0 {
		t.Errorf("expected 0 results (no episodes), got %d", len(results))
	}
}

// --- classifyContentType tests ---

func TestClassifyContentType(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		tags     []string
		expected string
	}{
		{"error_prefix", "ERROR: something failed", nil, "error"},
		{"bug_prefix", "BUG: null pointer", nil, "error"},
		{"error_tag", "something happened", []string{"error"}, "error"},
		{"bugfix_tag", "fixed the issue", []string{"bugfix"}, "error"},
		{"root_cause", "ROOT CAUSE: missing index", nil, "error"},
		{"sqlstate", "SQLSTATE 42P01: table not found", nil, "error"},

		{"decision_prefix", "DECISION: use RRF for hybrid search", nil, "decision"},
		{"decision_tag", "we will use approach A", []string{"decision"}, "decision"},
		{"architecture_tag", "microservices layout", []string{"architecture"}, "decision"},
		{"decided_keyword", "we decided to split the service", nil, "decision"},
		{"chose_keyword", "chose pgvector over pinecone", nil, "decision"},

		{"code_prefix", "CODE CHANGE: refactored handler", nil, "code"},
		{"code_tag", "updated the file", []string{"code_change"}, "code"},

		{"session_prefix", "SESSION SUMMARY: worked on API", nil, "session"},
		{"session_tag", "today we did stuff", []string{"session"}, "session"},

		{"gotcha_prefix", "GOTCHA: don't forget to flush", nil, "pattern"},
		{"performance_prefix", "Performance bottleneck in query", nil, "pattern"},
		{"gotcha_tag", "watch out for this", []string{"gotcha"}, "pattern"},

		{"project_doc", "PROJECT DOC: architecture overview", nil, "documentation"},
		{"project_knowledge_tag", "codebase uses clean arch", []string{"project_knowledge"}, "documentation"},

		{"prediction", "PREDICTION ERROR (delta=0.5)", nil, "prediction"},
		{"prediction_tag", "model was wrong", []string{"prediction_error"}, "prediction"},

		{"general_fallback", "something else entirely", nil, "general"},
		{"empty_content", "", nil, "general"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyContentType(tt.content, tt.tags)
			if got != tt.expected {
				t.Errorf("classifyContentType(%q, %v) = %q, want %q", tt.content, tt.tags, got, tt.expected)
			}
		})
	}
}

// --- clusterWithThreshold tests ---

func TestClusterWithThreshold_IdenticalEmbeddings(t *testing.T) {
	eps := []*domain.EpisodicMemory{
		makeEpisode("a", 0.5, nil, nil),
		makeEpisode("b", 0.5, nil, nil),
		makeEpisode("c", 0.5, nil, nil),
	}
	embs := [][]float32{
		{1, 0, 0},
		{1, 0, 0},
		{1, 0, 0},
	}
	svc := newService(&mockEpisodicRepo{}, &mockSemanticRepo{}, &mockEmbedding{}, ConsolidateConfig{ClusterThreshold: 0.9})
	clusters := svc.clusterWithThreshold(eps, embs, 0.9)
	if len(clusters) != 1 {
		t.Errorf("expected 1 cluster for identical embeddings, got %d", len(clusters))
	}
	if len(clusters[0].members) != 3 {
		t.Errorf("expected 3 members, got %d", len(clusters[0].members))
	}
}

func TestClusterWithThreshold_OrthogonalEmbeddings(t *testing.T) {
	eps := []*domain.EpisodicMemory{
		makeEpisode("x", 0.5, nil, nil),
		makeEpisode("y", 0.5, nil, nil),
		makeEpisode("z", 0.5, nil, nil),
	}
	embs := [][]float32{
		{1, 0, 0},
		{0, 1, 0},
		{0, 0, 1},
	}
	svc := newService(&mockEpisodicRepo{}, &mockSemanticRepo{}, &mockEmbedding{}, ConsolidateConfig{})
	clusters := svc.clusterWithThreshold(eps, embs, 0.5)
	if len(clusters) != 3 {
		t.Errorf("expected 3 clusters for orthogonal embeddings, got %d", len(clusters))
	}
}

func TestClusterWithThreshold_EmptyInput(t *testing.T) {
	svc := newService(&mockEpisodicRepo{}, &mockSemanticRepo{}, &mockEmbedding{}, ConsolidateConfig{})
	clusters := svc.clusterWithThreshold(nil, nil, 0.5)
	if len(clusters) != 0 {
		t.Errorf("expected 0 clusters for nil input, got %d", len(clusters))
	}
}

func TestClusterWithThreshold_TwoClusters(t *testing.T) {
	eps := []*domain.EpisodicMemory{
		makeEpisode("a1", 0.5, nil, nil),
		makeEpisode("a2", 0.5, nil, nil),
		makeEpisode("b1", 0.5, nil, nil),
		makeEpisode("b2", 0.5, nil, nil),
	}
	embs := [][]float32{
		{0.9, 0.1, 0},
		{0.85, 0.15, 0},
		{0.1, 0.9, 0},
		{0.15, 0.85, 0},
	}
	svc := newService(&mockEpisodicRepo{}, &mockSemanticRepo{}, &mockEmbedding{}, ConsolidateConfig{})
	clusters := svc.clusterWithThreshold(eps, embs, 0.7)
	if len(clusters) != 2 {
		t.Errorf("expected 2 clusters, got %d", len(clusters))
	}
	for _, cl := range clusters {
		if len(cl.members) != 2 {
			t.Errorf("expected 2 members per cluster, got %d", len(cl.members))
		}
	}
}

// --- promoteCluster tests ---

func TestPromoteCluster_ImportanceFloor(t *testing.T) {
	eps := []*domain.EpisodicMemory{
		makeEpisode("ERROR: low importance error one", 0.2, []string{"error"}, nil),
		makeEpisode("ERROR: low importance error two", 0.3, []string{"error"}, nil),
	}
	cl := &cluster{
		members:    eps,
		embeddings: [][]float32{{1, 0, 0}, {1, 0, 0}},
		centroid:   []float32{1, 0, 0},
	}
	semRepo := &mockSemanticRepo{}
	svc := newService(&mockEpisodicRepo{}, semRepo, &mockEmbedding{}, ConsolidateConfig{})
	sem, err := svc.promoteCluster(context.Background(), cl, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sem.Importance < 0.6 {
		t.Errorf("importance should be floored to 0.6, got %f", sem.Importance)
	}
}

func TestPromoteCluster_ConfidenceFormula(t *testing.T) {
	tests := []struct {
		members  int
		expected float64
	}{
		{2, 2.0 / 4.0},   // 0.5
		{3, 3.0 / 5.0},   // 0.6
		{5, 5.0 / 7.0},   // ~0.714
		{10, 10.0 / 12.0}, // ~0.833
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d", tt.members), func(t *testing.T) {
			eps := make([]*domain.EpisodicMemory, tt.members)
			embs := make([][]float32, tt.members)
			for i := range eps {
				eps[i] = makeEpisode(fmt.Sprintf("ERROR: test content %d", i), 0.7, []string{"error"}, nil)
				embs[i] = []float32{1, 0, 0}
			}
			cl := &cluster{members: eps, embeddings: embs, centroid: []float32{1, 0, 0}}
			semRepo := &mockSemanticRepo{}
			svc := newService(&mockEpisodicRepo{}, semRepo, &mockEmbedding{}, ConsolidateConfig{})
			sem, err := svc.promoteCluster(context.Background(), cl, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sem.Confidence != tt.expected {
				t.Errorf("confidence: got %f, want %f", sem.Confidence, tt.expected)
			}
		})
	}
}

func TestPromoteCluster_SelectsBestMember(t *testing.T) {
	eps := []*domain.EpisodicMemory{
		makeEpisode("ERROR: low importance content", 0.3, []string{"error"}, nil),
		makeEpisode("ERROR: high importance content that should be primary", 0.9, []string{"error"}, nil),
		makeEpisode("ERROR: medium importance content", 0.5, []string{"error"}, nil),
	}
	cl := &cluster{
		members:    eps,
		embeddings: [][]float32{{1, 0, 0}, {1, 0, 0}, {1, 0, 0}},
		centroid:   []float32{1, 0, 0},
	}
	semRepo := &mockSemanticRepo{}
	svc := newService(&mockEpisodicRepo{}, semRepo, &mockEmbedding{}, ConsolidateConfig{})
	sem, err := svc.promoteCluster(context.Background(), cl, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sem.Content, "high importance content that should be primary") {
		t.Errorf("content should start with highest importance member, got: %q", sem.Content[:80])
	}
}

func TestPromoteCluster_ContentTruncation(t *testing.T) {
	longContent := strings.Repeat("ERROR: this is a very long error description. ", 30) // ~1350 chars
	eps := []*domain.EpisodicMemory{
		makeEpisode(longContent, 0.7, []string{"error"}, nil),
		makeEpisode("ERROR: short additional info", 0.5, []string{"error"}, nil),
	}
	cl := &cluster{
		members:    eps,
		embeddings: [][]float32{{1, 0, 0}, {1, 0, 0}},
		centroid:   []float32{1, 0, 0},
	}
	semRepo := &mockSemanticRepo{}
	svc := newService(&mockEpisodicRepo{}, semRepo, &mockEmbedding{}, ConsolidateConfig{})
	sem, err := svc.promoteCluster(context.Background(), cl, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sem.Content) > 800 {
		t.Errorf("content should be truncated to <= 800 chars, got %d", len(sem.Content))
	}
}

func TestPromoteCluster_MergesTags(t *testing.T) {
	eps := []*domain.EpisodicMemory{
		makeEpisode("ERROR: tag test A", 0.7, []string{"error", "postgres"}, nil),
		makeEpisode("ERROR: tag test B", 0.6, []string{"error", "timeout"}, nil),
	}
	cl := &cluster{
		members:    eps,
		embeddings: [][]float32{{1, 0, 0}, {1, 0, 0}},
		centroid:   []float32{1, 0, 0},
	}
	semRepo := &mockSemanticRepo{}
	svc := newService(&mockEpisodicRepo{}, semRepo, &mockEmbedding{}, ConsolidateConfig{})
	sem, err := svc.promoteCluster(context.Background(), cl, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tagSet := make(map[string]bool)
	for _, t := range sem.Tags {
		tagSet[t] = true
	}
	for _, expected := range []string{"error", "postgres", "timeout"} {
		if !tagSet[expected] {
			t.Errorf("missing tag %q in merged tags: %v", expected, sem.Tags)
		}
	}
}

// --- inferEntityType tests ---

func TestInferEntityType(t *testing.T) {
	tests := []struct {
		name     string
		tags     []string
		content  string
		expected string
	}{
		{"decision_tag", []string{"decision"}, "", "decision"},
		{"architecture_tag", []string{"architecture"}, "", "decision"},
		{"bugfix_tag", []string{"bugfix"}, "", "bugfix"},
		{"fix_tag", []string{"fix"}, "", "bugfix"},
		{"error_tag", []string{"error"}, "", "bugfix"},
		{"pattern_tag", []string{"pattern"}, "", "pattern"},
		{"best_practice_tag", []string{"best-practice"}, "", "pattern"},

		{"decision_content", nil, "We decided to use gRPC", "decision"},
		{"chose_content", nil, "Team chose option B", "decision"},
		{"fix_content", nil, "Fixed the null pointer bug", "bugfix"},
		{"error_content", nil, "Error handling was missing", "bugfix"},
		{"pattern_content", nil, "This is a common pattern in Go", "pattern"},
		{"always_content", nil, "Always validate input before processing", "pattern"},

		{"fallback_fact", nil, "The service runs on port 8080", "fact"},
		{"empty", nil, "", "fact"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferEntityType(tt.tags, tt.content)
			if got != tt.expected {
				t.Errorf("inferEntityType(%v, %q) = %q, want %q", tt.tags, tt.content, got, tt.expected)
			}
		})
	}
}

// --- Helper function tests ---

func TestVecCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{"opposite", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0},
		{"empty", nil, nil, 0},
		{"length_mismatch", []float32{1, 0}, []float32{1, 0, 0}, 0},
		{"zero_vector", []float32{0, 0, 0}, []float32{1, 0, 0}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vecutil.CosineSimilarity(tt.a, tt.b)
			if diff := got - tt.want; diff > 0.001 || diff < -0.001 {
				t.Errorf("vecutil.CosineSimilarity = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestVecAverage(t *testing.T) {
	t.Run("single_vector", func(t *testing.T) {
		avg := vecAverage([][]float32{{2, 4, 6}})
		if avg[0] != 2 || avg[1] != 4 || avg[2] != 6 {
			t.Errorf("expected [2,4,6], got %v", avg)
		}
	})

	t.Run("two_vectors", func(t *testing.T) {
		avg := vecAverage([][]float32{{1, 0, 0}, {0, 1, 0}})
		if avg[0] != 0.5 || avg[1] != 0.5 || avg[2] != 0 {
			t.Errorf("expected [0.5,0.5,0], got %v", avg)
		}
	})

	t.Run("empty", func(t *testing.T) {
		avg := vecAverage(nil)
		if avg != nil {
			t.Errorf("expected nil for empty input, got %v", avg)
		}
	})
}

func TestFirstSentence(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal", "First sentence. Second sentence.", "First sentence."},
		{"no_period", "No period at all", "No period at all"},
		{"long_text", strings.Repeat("word ", 50), ""},  // will be truncated
		{"short", "Hi.", "Hi."},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstSentence(tt.input)
			if tt.name == "long_text" {
				if len(got) > 210 {
					t.Errorf("long text should be truncated, got len=%d", len(got))
				}
			} else if got != tt.expected {
				t.Errorf("firstSentence(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestMergeUniqueTags(t *testing.T) {
	members := []*domain.EpisodicMemory{
		makeEpisode("a", 0.5, []string{"error", "postgres"}, nil),
		makeEpisode("b", 0.5, []string{"error", "timeout"}, nil),
		makeEpisode("c", 0.5, []string{"postgres", "critical"}, nil),
	}
	tags := mergeUniqueTags(members)
	tagSet := make(map[string]bool)
	for _, t := range tags {
		tagSet[t] = true
	}
	expected := []string{"error", "postgres", "timeout", "critical"}
	for _, e := range expected {
		if !tagSet[e] {
			t.Errorf("missing tag %q in %v", e, tags)
		}
	}
	if len(tags) != 4 {
		t.Errorf("expected 4 unique tags, got %d: %v", len(tags), tags)
	}
}

func TestExtractUniqueInfo(t *testing.T) {
	t.Run("no_unique_info", func(t *testing.T) {
		primary := "The database connection timed out after thirty seconds."
		source := "The database connection timed out."
		got := extractUniqueInfo(source, primary)
		if got != "" {
			t.Errorf("expected empty for overlapping content, got %q", got)
		}
	})

	t.Run("has_unique_info", func(t *testing.T) {
		primary := "Connection timeout to the postgres database server."
		source := "The retry mechanism failed after three attempts with exponential backoff."
		got := extractUniqueInfo(source, primary)
		if got == "" {
			t.Error("expected unique info extracted, got empty")
		}
	})

	t.Run("limits_to_three_sentences", func(t *testing.T) {
		primary := "Connection timeout."
		source := "Alpha unique sentence one here. Beta unique sentence two here. Gamma unique sentence three here. Delta unique sentence four here. Epsilon unique sentence five here."
		got := extractUniqueInfo(source, primary)
		parts := strings.Split(strings.TrimSuffix(got, "."), ". ")
		if len(parts) > 3 {
			t.Errorf("expected max 3 unique sentences, got %d", len(parts))
		}
	})
}

func TestMergeClusterContent(t *testing.T) {
	primary := makeEpisode("Primary content about the main topic.", 0.9, nil, nil)
	members := []*domain.EpisodicMemory{
		primary,
		makeEpisode("Different content about retry logic and backoff strategy details.", 0.5, nil, nil),
		primary, // duplicate, should be skipped
	}
	got := mergeClusterContent(members, primary)
	if !strings.HasPrefix(got, "Primary content") {
		t.Error("should start with primary content")
	}
	if strings.Count(got, "Primary content") != 1 {
		t.Error("duplicate should be skipped")
	}
}

func TestWordSet(t *testing.T) {
	set := wordSet("The Quick Brown Fox Jumps At It")
	if !set["quick"] {
		t.Error("expected 'quick' in word set")
	}
	if !set["the"] {
		t.Error("'the' has len=3, should be included (threshold is >2)")
	}
	if set["at"] {
		t.Error("'at' has len=2, should be excluded (threshold is >2)")
	}
	if set["it"] {
		t.Error("'it' has len=2, should be excluded")
	}
}

func TestSetOverlap(t *testing.T) {
	t.Run("full_overlap", func(t *testing.T) {
		a := map[string]bool{"foo": true, "bar": true}
		b := map[string]bool{"foo": true, "bar": true, "baz": true}
		got := setOverlap(a, b)
		if got != 1.0 {
			t.Errorf("expected 1.0, got %f", got)
		}
	})

	t.Run("no_overlap", func(t *testing.T) {
		a := map[string]bool{"foo": true}
		b := map[string]bool{"bar": true}
		got := setOverlap(a, b)
		if got != 0.0 {
			t.Errorf("expected 0.0, got %f", got)
		}
	})

	t.Run("empty_a", func(t *testing.T) {
		got := setOverlap(map[string]bool{}, map[string]bool{"foo": true})
		if got != 0.0 {
			t.Errorf("expected 0.0 for empty a, got %f", got)
		}
	})

	t.Run("partial_overlap", func(t *testing.T) {
		a := map[string]bool{"foo": true, "bar": true}
		b := map[string]bool{"foo": true, "baz": true}
		got := setOverlap(a, b)
		if got != 0.5 {
			t.Errorf("expected 0.5, got %f", got)
		}
	})
}

// --- NewConsolidateService config defaults ---

func TestNewConsolidateService_Defaults(t *testing.T) {
	svc := NewConsolidateService(
		&mockEpisodicRepo{},
		&mockSemanticRepo{},
		&mockEmbedding{},
		nil,
		ConsolidateConfig{},
		testLogger(),
	)
	if svc.clusterThreshold != 0.72 {
		t.Errorf("default threshold: got %f, want 0.72", svc.clusterThreshold)
	}
	if svc.minClusterSize != 2 {
		t.Errorf("default minClusterSize: got %d, want 2", svc.minClusterSize)
	}
}

func TestNewConsolidateService_CustomConfig(t *testing.T) {
	svc := NewConsolidateService(
		&mockEpisodicRepo{},
		&mockSemanticRepo{},
		&mockEmbedding{},
		nil,
		ConsolidateConfig{ClusterThreshold: 0.85, MinClusterSize: 3},
		testLogger(),
	)
	if svc.clusterThreshold != 0.85 {
		t.Errorf("threshold: got %f, want 0.85", svc.clusterThreshold)
	}
	if svc.minClusterSize != 3 {
		t.Errorf("minClusterSize: got %d, want 3", svc.minClusterSize)
	}
}

// --- isAntiPatternTooGeneric tests ---

func TestIsAntiPatternTooGeneric(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    bool
	}{
		{"empty", "", true},
		{"dot star", ".*", true},
		{"dot plus", ".+", true},
		{"anchored dot star", "^.*$", true},
		{"too few literals", `\(\)`, true},
		{"specific function with package", `fmt\.Println`, false},
		{"pool acquire end of line", `pool\.Acquire\([^)]+\)\s*$`, false},
		{"store if procedural", `StoreIfProcedural.*EncodeService`, false},
		{"acquire without context", `http\.Client\{\}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAntiPatternTooGeneric(tt.pattern)
			if got != tt.want {
				t.Errorf("isAntiPatternTooGeneric(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

// --- parseRuleMetadata anti-pattern tests ---

func TestParseRuleMetadata_AntiPatternNone(t *testing.T) {
	rule := "WHEN: using pgx pool\nWATCH: missing defer Release\nBECAUSE: leak\nDO: add defer\nANTIPATTERN: NONE"
	meta := parseRuleMetadata(rule, nil)
	if _, ok := meta["rule_antipattern"]; ok {
		t.Error("ANTIPATTERN: NONE should not set rule_antipattern")
	}
	// WHEN should still be parsed
	if meta["rule_when"] != "using pgx pool" {
		t.Errorf("expected rule_when='using pgx pool', got %v", meta["rule_when"])
	}
}

func TestParseRuleMetadata_GenericRejected(t *testing.T) {
	// This pattern has only 7 literal chars ("Acquire") but the regex `Acquire\(.*\)`
	// should be rejected because it matches too broadly
	rule := "WHEN: pgx pool\nWATCH: leak\nBECAUSE: leak\nDO: fix\nANTIPATTERN: .*"
	meta := parseRuleMetadata(rule, nil)
	if _, ok := meta["rule_antipattern"]; ok {
		t.Error("trivially broad .* should be rejected")
	}

	// Test with anchored broad pattern
	rule2 := "WHEN: pgx pool\nWATCH: leak\nBECAUSE: leak\nDO: fix\nANTIPATTERN: ^.+$"
	meta2 := parseRuleMetadata(rule2, nil)
	if _, ok := meta2["rule_antipattern"]; ok {
		t.Error("anchored .+ should be rejected")
	}
}
