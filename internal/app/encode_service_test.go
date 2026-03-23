package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/memory"
)

// --- Mocks specific to encode tests ---

type encodeEpisodicRepo struct {
	mockEpisodicRepo
	inserted          []*domain.EpisodicMemory
	insertErr         error
	updatedImportance map[uuid.UUID]float64
	updateErr         error
	similarResults    []*domain.EpisodicMemory
	similarErr        error
}

func (m *encodeEpisodicRepo) Insert(_ context.Context, mem *domain.EpisodicMemory) error {
	m.inserted = append(m.inserted, mem)
	return m.insertErr
}

func (m *encodeEpisodicRepo) UpdateImportance(_ context.Context, id uuid.UUID, imp float64) error {
	if m.updatedImportance == nil {
		m.updatedImportance = make(map[uuid.UUID]float64)
	}
	m.updatedImportance[id] = imp
	return m.updateErr
}

func (m *encodeEpisodicRepo) SearchSimilar(_ context.Context, _ []float32, _ *uuid.UUID, _ int) ([]*domain.EpisodicMemory, error) {
	return m.similarResults, m.similarErr
}

type encodeEmotionalRepo struct {
	inserted  []*domain.EmotionalTag
	insertErr error
}

func (m *encodeEmotionalRepo) Insert(_ context.Context, tag *domain.EmotionalTag) error {
	m.inserted = append(m.inserted, tag)
	return m.insertErr
}

func (m *encodeEmotionalRepo) GetByMemory(_ context.Context, _ uuid.UUID) ([]*domain.EmotionalTag, error) {
	return nil, nil
}

func (m *encodeEmotionalRepo) GetHighPriority(_ context.Context, _ *uuid.UUID, _ int) ([]*domain.EmotionalTag, error) {
	return nil, nil
}

func newTestEncodeService(ep domain.EpisodicRepo, emo domain.EmotionalTagRepo, emb domain.EmbeddingProvider, threshold float64) *EncodeService {
	wm := memory.NewWorkingMemory(memory.WorkingMemoryConfig{Capacity: 10})
	svc := NewEncodeService(ep, emo, emb, wm, EncodeServiceConfig{GateThreshold: threshold},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.emotionDetector = EmotionDetector{}
	return svc
}

// --- estimateTokens ---

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"abcd", 1},
		{"hello world", 2}, // 11/4 = 2
		{"a", 0},           // 1/4 = 0
		{"this is a longer piece of text for testing", 10},
	}
	for _, tt := range tests {
		got := estimateTokens(tt.input)
		if got != tt.want {
			t.Errorf("estimateTokens(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

// --- emotionalImportanceBoost ---

func TestEmotionalImportanceBoost_Valences(t *testing.T) {
	tests := []struct {
		name     string
		emotions []DetectedEmotion
		want     float64
	}{
		{"empty", nil, 0},
		{"danger", []DetectedEmotion{
			{Valence: domain.ValDanger, Intensity: 1.0},
		}, 0.3},
		{"surprise", []DetectedEmotion{
			{Valence: domain.ValSurprise, Intensity: 1.0},
		}, 0.2},
		{"frustration", []DetectedEmotion{
			{Valence: domain.ValFrustration, Intensity: 0.6},
		}, 0.6 * 0.15},
		{"success", []DetectedEmotion{
			{Valence: domain.ValSuccess, Intensity: 1.0},
		}, 0.1},
		{"novelty", []DetectedEmotion{
			{Valence: domain.ValNovelty, Intensity: 1.0},
		}, 0.05},
		{"max_wins", []DetectedEmotion{
			{Valence: domain.ValNovelty, Intensity: 1.0},   // 0.05
			{Valence: domain.ValDanger, Intensity: 0.5},     // 0.15
			{Valence: domain.ValSuccess, Intensity: 1.0},    // 0.1
		}, 0.15}, // danger at 0.5 intensity wins
		{"zero_intensity", []DetectedEmotion{
			{Valence: domain.ValDanger, Intensity: 0.0},
		}, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := emotionalImportanceBoost(tt.emotions)
			if abs(got-tt.want) > 1e-9 {
				t.Errorf("emotionalImportanceBoost() = %f, want %f", got, tt.want)
			}
		})
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// --- Encode ---

func TestEncode_EmptyContent(t *testing.T) {
	svc := newTestEncodeService(&encodeEpisodicRepo{}, nil, &mockEmbedding{}, 0.3)
	_, err := svc.Encode(context.Background(), &EncodeRequest{Content: ""})
	if !errors.Is(err, domain.ErrEmptyContent) {
		t.Errorf("expected ErrEmptyContent, got %v", err)
	}
}

func TestEncode_BelowGateThreshold(t *testing.T) {
	svc := newTestEncodeService(
		&encodeEpisodicRepo{},
		nil,
		&mockEmbedding{embeddings: [][]float32{{0.1}}},
		0.8, // high threshold
	)

	resp, err := svc.Encode(context.Background(), &EncodeRequest{
		Content:    "minor observation about code formatting style preferences",
		Importance: 0.2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Encoded {
		t.Error("expected Encoded=false for below-threshold importance")
	}
}

func TestEncode_ContentQualityGate(t *testing.T) {
	svc := newTestEncodeService(
		&encodeEpisodicRepo{},
		nil,
		&mockEmbedding{embeddings: [][]float32{{0.1}}},
		0.3,
	)

	resp, err := svc.Encode(context.Background(), &EncodeRequest{
		Content:    "ok",
		Importance: 0.9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Encoded {
		t.Error("expected Encoded=false for low quality content")
	}
}

func TestEncode_DefaultValues(t *testing.T) {
	ep := &encodeEpisodicRepo{}
	svc := newTestEncodeService(
		ep, nil,
		&mockEmbedding{embeddings: [][]float32{{0.1, 0.2}}},
		0.3,
	)

	resp, err := svc.Encode(context.Background(), &EncodeRequest{
		Content: "discovered important pattern in goroutine lifecycle management",
		// AgentID, SessionID, Importance all zero/empty
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Encoded {
		t.Error("expected Encoded=true (default importance 0.5 > threshold 0.3)")
	}
	if len(ep.inserted) != 1 {
		t.Fatalf("expected 1 insert, got %d", len(ep.inserted))
	}
	mem := ep.inserted[0]
	if mem.AgentID != "hippocampus-internal" {
		t.Errorf("AgentID = %q, want hippocampus-internal", mem.AgentID)
	}
	if mem.SessionID == uuid.Nil {
		t.Error("SessionID should be auto-generated, not nil")
	}
	if mem.Importance != 0.5 {
		t.Errorf("Importance = %f, want 0.5 (default)", mem.Importance)
	}
	if mem.Confidence != 1.0 {
		t.Errorf("Confidence = %f, want 1.0", mem.Confidence)
	}
}

func TestEncode_EmbeddingError(t *testing.T) {
	svc := newTestEncodeService(
		&encodeEpisodicRepo{},
		nil,
		&mockEmbedding{err: errors.New("embedding service down")},
		0.3,
	)

	_, err := svc.Encode(context.Background(), &EncodeRequest{
		Content:    "database migration failed with schema version mismatch error",
		Importance: 0.5,
	})
	if err == nil {
		t.Fatal("expected error from embedding failure")
	}
}

func TestEncode_InsertError(t *testing.T) {
	ep := &encodeEpisodicRepo{insertErr: errors.New("db down")}
	svc := newTestEncodeService(
		ep, nil,
		&mockEmbedding{embeddings: [][]float32{{0.1}}},
		0.3,
	)

	_, err := svc.Encode(context.Background(), &EncodeRequest{
		Content:    "database connection pool configuration for production deployment",
		Importance: 0.5,
	})
	if err == nil {
		t.Fatal("expected error from insert failure")
	}
}

func TestEncode_SuccessFlow(t *testing.T) {
	ep := &encodeEpisodicRepo{}
	emb := &mockEmbedding{embeddings: [][]float32{{0.1, 0.2, 0.3}}}
	svc := newTestEncodeService(ep, nil, emb, 0.3)

	pid := uuid.New()
	sid := uuid.New()
	resp, err := svc.Encode(context.Background(), &EncodeRequest{
		Content:    "important discovery about Go interfaces",
		ProjectID:  &pid,
		AgentID:    "test-agent",
		SessionID:  sid,
		Importance: 0.8,
		Tags:       []string{"go", "interfaces"},
		Metadata:   domain.Metadata{"source": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Encoded {
		t.Error("expected Encoded=true")
	}
	// GateScore = importance*0.7 + qualityScore*0.3
	if resp.GateScore < 0.5 {
		t.Errorf("GateScore = %f, want > 0.5", resp.GateScore)
	}
	if resp.TokenCount <= 0 {
		t.Error("TokenCount should be positive")
	}
	if resp.MemoryID == uuid.Nil {
		t.Error("MemoryID should be set")
	}

	mem := ep.inserted[0]
	if mem.AgentID != "test-agent" {
		t.Errorf("AgentID = %q, want test-agent", mem.AgentID)
	}
	if mem.SessionID != sid {
		t.Error("SessionID mismatch")
	}
	if *mem.ProjectID != pid {
		t.Error("ProjectID mismatch")
	}
	// Tags include manual + auto-extracted
	if len(mem.Tags) < 2 {
		t.Errorf("Tags len = %d, want >= 2", len(mem.Tags))
	}
	if len(mem.Embedding) != 3 {
		t.Error("Embedding not stored")
	}
}

func TestEncode_WithEmotions(t *testing.T) {
	ep := &encodeEpisodicRepo{}
	emo := &encodeEmotionalRepo{}
	svc := newTestEncodeService(
		ep, emo,
		&mockEmbedding{embeddings: [][]float32{{0.1}}},
		0.3,
	)

	resp, err := svc.Encode(context.Background(), &EncodeRequest{
		Content:    "CRASH in production! Fatal panic caused data loss!",
		Importance: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Encoded {
		t.Error("expected Encoded=true")
	}
	if len(resp.EmotionsFound) == 0 {
		t.Error("expected emotions detected for danger keywords")
	}
	if len(emo.inserted) == 0 {
		t.Error("expected emotional tags to be inserted")
	}

	hasDanger := false
	for _, tag := range emo.inserted {
		if tag.Valence == domain.ValDanger {
			hasDanger = true
		}
		if tag.MemoryID == uuid.Nil {
			t.Error("emotional tag should have memory ID")
		}
	}
	if !hasDanger {
		t.Error("expected danger valence for crash/panic/data loss content")
	}

	// Importance should be boosted
	if len(ep.updatedImportance) == 0 {
		t.Error("expected importance boost from emotional detection")
	}
}

func TestEncode_EmotionalTagInsertError(t *testing.T) {
	ep := &encodeEpisodicRepo{}
	emo := &encodeEmotionalRepo{insertErr: errors.New("tag insert fail")}
	svc := newTestEncodeService(
		ep, emo,
		&mockEmbedding{embeddings: [][]float32{{0.1}}},
		0.3,
	)

	// Should not fail overall — emotional tag errors are logged, not propagated
	resp, err := svc.Encode(context.Background(), &EncodeRequest{
		Content:    "CRASH in production server! Fatal panic caused data loss in database!",
		Importance: 0.5,
	})
	if err != nil {
		t.Fatal("emotional tag insert failure should not propagate")
	}
	if !resp.Encoded {
		t.Error("memory should still be encoded despite tag failure")
	}
}

// --- demoteSimilar ---

func TestDemoteSimilar_HighSimilarity(t *testing.T) {
	newID := uuid.New()
	oldID := uuid.New()

	ep := &encodeEpisodicRepo{
		similarResults: []*domain.EpisodicMemory{
			{MemoryItem: domain.MemoryItem{ID: oldID, Importance: 0.8, Similarity: 0.92}},
			{MemoryItem: domain.MemoryItem{ID: newID, Importance: 0.8, Similarity: 1.0}}, // self
		},
	}
	svc := newTestEncodeService(ep, nil, &mockEmbedding{embeddings: [][]float32{{0.1}}}, 0.3)

	svc.demoteSimilar(context.Background(), []float32{0.1}, nil, newID, 0.8)

	if demoted, ok := ep.updatedImportance[oldID]; !ok {
		t.Error("expected oldID to be demoted")
	} else if abs(demoted-0.56) > 1e-9 { // 0.8 * 0.7 = 0.56
		t.Errorf("demoted importance = %f, want 0.56", demoted)
	}

	if _, ok := ep.updatedImportance[newID]; ok {
		t.Error("newID (self) should not be demoted")
	}
}

func TestDemoteSimilar_LowSimilarity(t *testing.T) {
	newID := uuid.New()
	otherID := uuid.New()

	ep := &encodeEpisodicRepo{
		similarResults: []*domain.EpisodicMemory{
			{MemoryItem: domain.MemoryItem{ID: otherID, Importance: 0.8, Similarity: 0.60}},
		},
	}
	svc := newTestEncodeService(ep, nil, &mockEmbedding{embeddings: [][]float32{{0.1}}}, 0.3)

	svc.demoteSimilar(context.Background(), []float32{0.1}, nil, newID, 0.8)

	if len(ep.updatedImportance) != 0 {
		t.Error("similarity < 0.75 should not be demoted")
	}
}

func TestDemoteSimilar_FloorAt01(t *testing.T) {
	newID := uuid.New()
	oldID := uuid.New()

	ep := &encodeEpisodicRepo{
		similarResults: []*domain.EpisodicMemory{
			{MemoryItem: domain.MemoryItem{ID: oldID, Importance: 0.1, Similarity: 0.90}},
		},
	}
	svc := newTestEncodeService(ep, nil, &mockEmbedding{embeddings: [][]float32{{0.1}}}, 0.3)

	svc.demoteSimilar(context.Background(), []float32{0.1}, nil, newID, 0.5)

	// 0.1 * 0.7 = 0.07, but floor is 0.1
	if demoted, ok := ep.updatedImportance[oldID]; !ok {
		t.Error("expected demotion")
	} else if abs(demoted-0.1) > 1e-9 {
		t.Errorf("demoted = %f, want 0.1 (floor)", demoted)
	}
}

func TestDemoteSimilar_SearchError(t *testing.T) {
	newID := uuid.New()
	ep := &encodeEpisodicRepo{similarErr: errors.New("search fail")}
	svc := newTestEncodeService(ep, nil, &mockEmbedding{embeddings: [][]float32{{0.1}}}, 0.3)

	// Should not panic, just log warning
	svc.demoteSimilar(context.Background(), []float32{0.1}, nil, newID, 0.5)
}

// --- NewEncodeService defaults ---

func TestNewEncodeService_DefaultThreshold(t *testing.T) {
	svc := NewEncodeService(
		&encodeEpisodicRepo{}, nil,
		&mockEmbedding{},
		memory.NewWorkingMemory(memory.WorkingMemoryConfig{}),
		EncodeServiceConfig{GateThreshold: 0}, // zero → default
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if svc.gateThreshold != 0.3 {
		t.Errorf("default gateThreshold = %f, want 0.3", svc.gateThreshold)
	}
}

func TestNewEncodeService_CustomThreshold(t *testing.T) {
	svc := NewEncodeService(
		&encodeEpisodicRepo{}, nil,
		&mockEmbedding{},
		memory.NewWorkingMemory(memory.WorkingMemoryConfig{}),
		EncodeServiceConfig{GateThreshold: 0.7},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if svc.gateThreshold != 0.7 {
		t.Errorf("gateThreshold = %f, want 0.7", svc.gateThreshold)
	}
}

// --- computeNovelty ---

func TestComputeNovelty_NothingSimilar(t *testing.T) {
	ep := &encodeEpisodicRepo{similarResults: nil}
	svc := newTestEncodeService(ep, nil, &mockEmbedding{embeddings: [][]float32{{0.1}}}, 0.3)

	novelty := svc.computeNovelty(context.Background(), []float32{0.1}, nil)
	if novelty != 1.0 {
		t.Errorf("novelty = %f, want 1.0 (nothing similar)", novelty)
	}
}

func TestComputeNovelty_ExactDuplicate(t *testing.T) {
	ep := &encodeEpisodicRepo{
		similarResults: []*domain.EpisodicMemory{
			{MemoryItem: domain.MemoryItem{ID: uuid.New(), Similarity: 0.98}},
		},
	}
	svc := newTestEncodeService(ep, nil, &mockEmbedding{embeddings: [][]float32{{0.1}}}, 0.3)

	novelty := svc.computeNovelty(context.Background(), []float32{0.1}, nil)
	if novelty > 0.05 {
		t.Errorf("novelty = %f, want ~0.02 for near-duplicate", novelty)
	}
}

func TestEncode_NearDuplicateHardReject(t *testing.T) {
	ep := &encodeEpisodicRepo{
		similarResults: []*domain.EpisodicMemory{
			{MemoryItem: domain.MemoryItem{ID: uuid.New(), Similarity: 0.95}},
		},
	}
	svc := newTestEncodeService(
		ep, nil,
		&mockEmbedding{embeddings: [][]float32{{0.1}}},
		0.3,
	)

	resp, err := svc.Encode(context.Background(), &EncodeRequest{
		Content:    "this is a fairly long content that would normally pass all quality checks easily",
		Importance: 1.0, // max importance — should still be rejected
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Encoded {
		t.Error("expected Encoded=false for near-duplicate (novelty < 0.10)")
	}
	if resp.GateScore > 0.10 {
		t.Errorf("GateScore = %f, want < 0.10 for near-duplicate", resp.GateScore)
	}
}

func TestComputeNovelty_CrossTier(t *testing.T) {
	ep := &encodeEpisodicRepo{similarResults: nil} // no episodic match
	sem := &retrieverSemanticRepo{
		vecResults: []*domain.SemanticMemory{
			{MemoryItem: domain.MemoryItem{ID: uuid.New(), Similarity: 0.95}},
		},
	}
	svc := newTestEncodeService(ep, nil, &mockEmbedding{embeddings: [][]float32{{0.1}}}, 0.3)
	svc.SetSemanticRepo(sem)

	novelty := svc.computeNovelty(context.Background(), []float32{0.1}, nil)
	if novelty > 0.1 {
		t.Errorf("novelty = %f, want ~0.05 (semantic match detected)", novelty)
	}
}

// --- cross-tier demoteSimilar ---

func TestDemoteSimilar_CrossTierSemantic(t *testing.T) {
	newID := uuid.New()
	semID := uuid.New()

	ep := &encodeEpisodicRepo{similarResults: nil}
	sem := &encodeSemanticRepo{
		similarResults: []*domain.SemanticMemory{
			{MemoryItem: domain.MemoryItem{ID: semID, Importance: 0.8, Similarity: 0.85}},
		},
	}
	svc := newTestEncodeService(ep, nil, &mockEmbedding{embeddings: [][]float32{{0.1}}}, 0.3)
	svc.SetSemanticRepo(sem)

	svc.demoteSimilar(context.Background(), []float32{0.1}, nil, newID, 0.8)

	if demoted, ok := sem.updatedImportance[semID]; !ok {
		t.Error("expected semantic memory to be demoted")
	} else if abs(demoted-0.64) > 1e-9 { // 0.8 * 0.8 = 0.64
		t.Errorf("semantic demoted = %f, want 0.64", demoted)
	}
}

type encodeSemanticRepo struct {
	mockSemanticRepo
	similarResults    []*domain.SemanticMemory
	updatedImportance map[uuid.UUID]float64
}

func (m *encodeSemanticRepo) SearchSimilar(_ context.Context, _ []float32, _ *uuid.UUID, _ int) ([]*domain.SemanticMemory, error) {
	return m.similarResults, nil
}

func (m *encodeSemanticRepo) UpdateImportance(_ context.Context, id uuid.UUID, imp float64) error {
	if m.updatedImportance == nil {
		m.updatedImportance = make(map[uuid.UUID]float64)
	}
	m.updatedImportance[id] = imp
	return nil
}
