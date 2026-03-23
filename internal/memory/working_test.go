package memory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

func makeItem(importance float64) *domain.MemoryItem {
	return &domain.MemoryItem{
		ID:           uuid.New(),
		Tier:         domain.TierWorking,
		Content:      fmt.Sprintf("test item importance=%f", importance),
		Importance:   importance,
		Confidence:   1.0,
		TokenCount:   100,
		LastAccessed: time.Now(),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

func TestWorkingMemory_PutAndGet(t *testing.T) {
	wm := NewWorkingMemory(WorkingMemoryConfig{Capacity: 10})
	ctx := context.Background()

	item := makeItem(0.8)
	evicted := wm.Put(ctx, item)
	if evicted != nil {
		t.Error("should not evict when under capacity")
	}

	got, ok := wm.Get(ctx, item.ID)
	if !ok {
		t.Fatal("item should be found")
	}
	if got.ID != item.ID {
		t.Errorf("expected %s, got %s", item.ID, got.ID)
	}
	if wm.Size() != 1 {
		t.Errorf("expected size 1, got %d", wm.Size())
	}
}

func TestWorkingMemory_EvictsLowestImportance(t *testing.T) {
	wm := NewWorkingMemory(WorkingMemoryConfig{Capacity: 3})
	ctx := context.Background()

	items := []*domain.MemoryItem{
		makeItem(0.9),
		makeItem(0.1), // this one should be evicted
		makeItem(0.7),
	}

	for _, item := range items {
		wm.Put(ctx, item)
	}

	if wm.Size() != 3 {
		t.Fatalf("expected size 3, got %d", wm.Size())
	}

	highItem := makeItem(0.95)
	evicted := wm.Put(ctx, highItem)

	if evicted == nil {
		t.Fatal("expected eviction")
	}

	// The 0.1 importance item should have been evicted
	if evicted.ID != items[1].ID {
		t.Errorf("expected item with importance 0.1 to be evicted, got importance=%f", evicted.Importance)
	}

	if wm.Size() != 3 {
		t.Errorf("expected size 3 after eviction, got %d", wm.Size())
	}

	if _, ok := wm.Get(ctx, items[1].ID); ok {
		t.Error("evicted item should not be found")
	}
}

func TestWorkingMemory_UpdateExisting(t *testing.T) {
	wm := NewWorkingMemory(WorkingMemoryConfig{Capacity: 10})
	ctx := context.Background()

	item := makeItem(0.5)
	wm.Put(ctx, item)

	updated := *item
	updated.Importance = 0.9
	updated.Content = "updated content"
	evicted := wm.Put(ctx, &updated)

	if evicted != nil {
		t.Error("update should not cause eviction")
	}
	if wm.Size() != 1 {
		t.Errorf("update should not change size, got %d", wm.Size())
	}

	got, ok := wm.Get(ctx, item.ID)
	if !ok {
		t.Fatal("item should still exist")
	}
	if got.Importance != 0.9 {
		t.Errorf("importance should be updated to 0.9, got %f", got.Importance)
	}
}

func TestWorkingMemory_Remove(t *testing.T) {
	wm := NewWorkingMemory(WorkingMemoryConfig{Capacity: 10})
	ctx := context.Background()

	item := makeItem(0.8)
	wm.Put(ctx, item)

	if !wm.Remove(ctx, item.ID) {
		t.Error("remove should return true for existing item")
	}
	if wm.Size() != 0 {
		t.Errorf("expected size 0 after remove, got %d", wm.Size())
	}
	if wm.Remove(ctx, item.ID) {
		t.Error("remove should return false for non-existing item")
	}
}

func TestWorkingMemory_Snapshot(t *testing.T) {
	wm := NewWorkingMemory(WorkingMemoryConfig{Capacity: 10})
	ctx := context.Background()

	importances := []float64{0.3, 0.9, 0.1, 0.7}
	for _, imp := range importances {
		wm.Put(ctx, makeItem(imp))
	}

	snapshot := wm.Snapshot(ctx)
	if len(snapshot) != 4 {
		t.Fatalf("expected 4 items, got %d", len(snapshot))
	}

	for i := 1; i < len(snapshot); i++ {
		if snapshot[i].Score.Composite > snapshot[i-1].Score.Composite {
			t.Errorf("snapshot not sorted descending: [%d]=%f > [%d]=%f",
				i, snapshot[i].Score.Composite, i-1, snapshot[i-1].Score.Composite)
		}
	}
}

func TestWorkingMemory_Clear(t *testing.T) {
	wm := NewWorkingMemory(WorkingMemoryConfig{Capacity: 10})
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		wm.Put(ctx, makeItem(float64(i)*0.2))
	}

	wm.Clear()
	if wm.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", wm.Size())
	}
}

func TestWorkingMemory_Peek(t *testing.T) {
	wm := NewWorkingMemory(WorkingMemoryConfig{Capacity: 10})
	ctx := context.Background()

	item := makeItem(0.5)
	wm.Put(ctx, item)

	got, ok := wm.Peek(ctx, item.ID)
	if !ok {
		t.Fatal("peek should find item")
	}
	if got.ID != item.ID {
		t.Error("peek returned wrong item")
	}

	_, ok = wm.Peek(ctx, uuid.New())
	if ok {
		t.Error("peek should return false for missing item")
	}
}

func BenchmarkWorkingMemory_Put(b *testing.B) {
	wm := NewWorkingMemory(WorkingMemoryConfig{Capacity: 50})
	ctx := context.Background()
	items := make([]*domain.MemoryItem, b.N)
	for i := range items {
		items[i] = makeItem(float64(i%100) / 100.0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wm.Put(ctx, items[i])
	}
}

func BenchmarkWorkingMemory_Get(b *testing.B) {
	wm := NewWorkingMemory(WorkingMemoryConfig{Capacity: 50})
	ctx := context.Background()
	ids := make([]uuid.UUID, 50)
	for i := 0; i < 50; i++ {
		item := makeItem(float64(i) / 50.0)
		wm.Put(ctx, item)
		ids[i] = item.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wm.Get(ctx, ids[i%50])
	}
}

func BenchmarkWorkingMemory_Snapshot(b *testing.B) {
	wm := NewWorkingMemory(WorkingMemoryConfig{Capacity: 50})
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		wm.Put(ctx, makeItem(float64(i)/50.0))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wm.Snapshot(ctx)
	}
}
