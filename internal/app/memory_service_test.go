package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

func newMemoryService(epRepo *mockEpisodicRepo, semRepo *mockSemanticRepo) *MemoryService {
	return &MemoryService{
		episodic: epRepo,
		semantic: semRepo,
		project:  &mockProjectRepo{},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestAdjustImportance(t *testing.T) {
	svc := newMemoryService(&mockEpisodicRepo{}, &mockSemanticRepo{})

	tests := []struct {
		name   string
		old    float64
		useful bool
		want   float64
	}{
		{"useful_boost", 0.5, true, 0.65},
		{"useful_cap", 0.9, true, 1.0},
		{"useful_already_max", 1.0, true, 1.0},
		{"not_useful_decay", 0.5, false, 0.35},
		{"not_useful_floor", 0.1, false, 0.1},
		{"not_useful_below_floor", 0.05, false, 0.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.adjustImportance(tt.old, tt.useful)
			if diff := got - tt.want; diff > 0.001 || diff < -0.001 {
				t.Errorf("adjustImportance(%f, %v) = %f, want %f", tt.old, tt.useful, got, tt.want)
			}
		})
	}
}

func TestFeedback_Episodic(t *testing.T) {
	id := uuid.New()
	epRepo := &mockEpisodicRepoWithGet{
		mem: &domain.EpisodicMemory{
			MemoryItem: domain.MemoryItem{ID: id, Importance: 0.5},
		},
	}
	svc := &MemoryService{
		episodic: epRepo,
		semantic: &mockSemanticRepo{},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	result, err := svc.Feedback(context.Background(), id, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Tier != "episodic" {
		t.Errorf("expected tier=episodic, got %q", result.Tier)
	}
	if result.OldImportance != 0.5 {
		t.Errorf("expected old=0.5, got %f", result.OldImportance)
	}
	if result.NewImportance != 0.65 {
		t.Errorf("expected new=0.65, got %f", result.NewImportance)
	}
}

func TestFeedback_Semantic(t *testing.T) {
	id := uuid.New()
	semRepo := &mockSemanticRepoWithGet{
		mem: &domain.SemanticMemory{
			MemoryItem: domain.MemoryItem{ID: id, Importance: 0.8},
		},
	}
	svc := &MemoryService{
		episodic: &mockEpisodicRepoWithGet{err: errors.New("not found")},
		semantic: semRepo,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	result, err := svc.Feedback(context.Background(), id, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Tier != "semantic" {
		t.Errorf("expected tier=semantic, got %q", result.Tier)
	}
}

func TestFeedback_NotFound(t *testing.T) {
	svc := &MemoryService{
		episodic: &mockEpisodicRepoWithGet{err: errors.New("not found")},
		semantic: &mockSemanticRepoWithGet{err: errors.New("not found")},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	_, err := svc.Feedback(context.Background(), uuid.New(), true)
	if err == nil {
		t.Error("expected error for non-existent memory")
	}
}

func TestList_DefaultLimit(t *testing.T) {
	ep := &domain.EpisodicMemory{
		MemoryItem: domain.MemoryItem{
			ID: uuid.New(), Tier: domain.TierEpisodic,
			Content: "test", Importance: 0.5, CreatedAt: time.Now(),
		},
		AgentID: "test-agent",
	}
	epRepo := &mockEpisodicRepo{unconsolidated: []*domain.EpisodicMemory{ep}}
	svc := newMemoryService(epRepo, &mockSemanticRepo{})

	items, total, err := svc.List(context.Background(), ListMemoriesFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 { // Count returns 0 from mock
		t.Errorf("expected total=0, got %d", total)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}

// --- Extended mocks for MemoryService ---

type mockEpisodicRepoWithGet struct {
	mockEpisodicRepo
	mem         *domain.EpisodicMemory
	err         error
	updatedImps map[uuid.UUID]float64
}

func (m *mockEpisodicRepoWithGet) GetByID(_ context.Context, id uuid.UUID) (*domain.EpisodicMemory, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.mem, nil
}

func (m *mockEpisodicRepoWithGet) UpdateImportance(_ context.Context, id uuid.UUID, imp float64) error {
	if m.updatedImps == nil {
		m.updatedImps = make(map[uuid.UUID]float64)
	}
	m.updatedImps[id] = imp
	return nil
}

type mockSemanticRepoWithGet struct {
	mockSemanticRepo
	mem         *domain.SemanticMemory
	err         error
	updatedImps map[uuid.UUID]float64
}

func (m *mockSemanticRepoWithGet) GetByID(_ context.Context, id uuid.UUID) (*domain.SemanticMemory, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.mem, nil
}

func (m *mockSemanticRepoWithGet) UpdateImportance(_ context.Context, id uuid.UUID, imp float64) error {
	if m.updatedImps == nil {
		m.updatedImps = make(map[uuid.UUID]float64)
	}
	m.updatedImps[id] = imp
	return nil
}

type mockProjectRepo struct {
	projects map[string]*domain.Project
}

func (m *mockProjectRepo) Create(_ context.Context, p *domain.Project) error { return nil }
func (m *mockProjectRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Project, error) {
	return nil, errors.New("not found")
}
func (m *mockProjectRepo) GetBySlug(_ context.Context, slug string) (*domain.Project, error) {
	if m.projects != nil {
		if p, ok := m.projects[slug]; ok {
			return p, nil
		}
	}
	return nil, errors.New("not found")
}
func (m *mockProjectRepo) List(_ context.Context) ([]*domain.Project, error) { return nil, nil }
func (m *mockProjectRepo) Update(_ context.Context, _ *domain.Project) error { return nil }
func (m *mockProjectRepo) Delete(_ context.Context, _ uuid.UUID) error       { return nil }
func (m *mockProjectRepo) GetStats(_ context.Context, _ uuid.UUID) (*domain.ProjectStats, error) {
	return nil, nil
}
