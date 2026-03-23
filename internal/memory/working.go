package memory

import (
	"container/heap"
	"context"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// WorkingMemory is the hot-path in-memory buffer. It corresponds to the
// prefrontal cortex — holding the task-relevant subset of all memories.
//
// Design invariants:
//  - Fixed capacity N (configurable, default 50)
//  - O(1) Get, O(log N) Put with eviction, O(N) Snapshot
//  - Eviction is importance-weighted: lowest composite score is dropped
//  - Thread-safe via RWMutex (single-writer, multiple-reader pattern)
//  - No persistence: restart = empty working memory (by design)
type WorkingMemory struct {
	mu       sync.RWMutex
	capacity int
	items    map[uuid.UUID]*workingEntry
	minHeap  entryHeap
	decayλ   float64 // ln(2) / half_life_seconds
}

type workingEntry struct {
	item         *domain.MemoryItem
	heapIndex    int
	insertedAt   time.Time
	lastAccessed time.Time
	accessCount  int
}

func (e *workingEntry) effectiveScore(now time.Time, decayλ float64) float64 {
	age := now.Sub(e.lastAccessed).Seconds()
	recency := math.Exp(-decayλ * age)
	accessBoost := math.Log2(float64(e.accessCount) + 1) * 0.1
	return e.item.Importance*0.6 + recency*0.3 + accessBoost*0.1
}

type entryHeap []*workingEntry

func (h entryHeap) Len() int      { return len(h) }
func (h entryHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i]; h[i].heapIndex = i; h[j].heapIndex = j }

func (h entryHeap) Less(i, j int) bool {
	now := time.Now()
	λ := 0.000011 // ~18h half-life default; overridden at runtime
	return h[i].effectiveScore(now, λ) < h[j].effectiveScore(now, λ)
}

func (h *entryHeap) Push(x any) {
	entry := x.(*workingEntry)
	entry.heapIndex = len(*h)
	*h = append(*h, entry)
}

func (h *entryHeap) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	entry.heapIndex = -1
	*h = old[:n-1]
	return entry
}

type WorkingMemoryConfig struct {
	Capacity         int
	DecayHalfLifeSec float64
}

func NewWorkingMemory(cfg WorkingMemoryConfig) *WorkingMemory {
	if cfg.Capacity <= 0 {
		cfg.Capacity = 50
	}
	if cfg.DecayHalfLifeSec <= 0 {
		cfg.DecayHalfLifeSec = 3600 // 1 hour default
	}

	wm := &WorkingMemory{
		capacity: cfg.Capacity,
		items:    make(map[uuid.UUID]*workingEntry, cfg.Capacity),
		minHeap:  make(entryHeap, 0, cfg.Capacity),
		decayλ:   math.Ln2 / cfg.DecayHalfLifeSec,
	}
	heap.Init(&wm.minHeap)
	return wm
}

// Put adds or updates a memory item in working memory.
// If capacity is exceeded, the item with the lowest effective score is evicted.
// Returns the evicted item (nil if no eviction occurred).
func (wm *WorkingMemory) Put(_ context.Context, item *domain.MemoryItem) *domain.MemoryItem {
	now := time.Now()

	wm.mu.Lock()
	defer wm.mu.Unlock()

	if existing, ok := wm.items[item.ID]; ok {
		existing.item = item
		existing.lastAccessed = now
		existing.accessCount++
		heap.Fix(&wm.minHeap, existing.heapIndex)
		return nil
	}

	entry := &workingEntry{
		item:         item,
		insertedAt:   now,
		lastAccessed: now,
		accessCount:  1,
	}

	var evicted *domain.MemoryItem
	if len(wm.items) >= wm.capacity {
		evicted = wm.evictLowest()
	}

	heap.Push(&wm.minHeap, entry)
	wm.items[item.ID] = entry

	return evicted
}

// Get retrieves a memory item by ID and refreshes its access time.
func (wm *WorkingMemory) Get(_ context.Context, id uuid.UUID) (*domain.MemoryItem, bool) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	entry, ok := wm.items[id]
	if !ok {
		return nil, false
	}

	entry.lastAccessed = time.Now()
	entry.accessCount++
	heap.Fix(&wm.minHeap, entry.heapIndex)

	return entry.item, true
}

// Peek retrieves without refreshing access time (read-only inspection).
func (wm *WorkingMemory) Peek(_ context.Context, id uuid.UUID) (*domain.MemoryItem, bool) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	entry, ok := wm.items[id]
	if !ok {
		return nil, false
	}
	return entry.item, true
}

// Remove explicitly removes a memory item from working memory.
func (wm *WorkingMemory) Remove(_ context.Context, id uuid.UUID) bool {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	entry, ok := wm.items[id]
	if !ok {
		return false
	}

	heap.Remove(&wm.minHeap, entry.heapIndex)
	delete(wm.items, id)
	return true
}

// Snapshot returns all current working memory items sorted by effective score (descending).
func (wm *WorkingMemory) Snapshot(_ context.Context) []*domain.ScoredMemory {
	now := time.Now()

	wm.mu.RLock()
	defer wm.mu.RUnlock()

	result := make([]*domain.ScoredMemory, 0, len(wm.items))
	for _, entry := range wm.items {
		score := entry.effectiveScore(now, wm.decayλ)
		result = append(result, &domain.ScoredMemory{
			Memory: entry.item,
			Score: domain.ImportanceScore{
				Composite: score,
			},
		})
	}

	// Sort descending by composite score
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j].Score.Composite > result[j-1].Score.Composite; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}

	return result
}

// Size returns current number of items in working memory.
func (wm *WorkingMemory) Size() int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	return len(wm.items)
}

// Capacity returns the maximum number of items.
func (wm *WorkingMemory) Capacity() int {
	return wm.capacity
}

// Clear removes all items from working memory.
func (wm *WorkingMemory) Clear() {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.items = make(map[uuid.UUID]*workingEntry, wm.capacity)
	wm.minHeap = make(entryHeap, 0, wm.capacity)
	heap.Init(&wm.minHeap)
}

func (wm *WorkingMemory) evictLowest() *domain.MemoryItem {
	if wm.minHeap.Len() == 0 {
		return nil
	}
	entry := heap.Pop(&wm.minHeap).(*workingEntry)
	delete(wm.items, entry.item.ID)
	return entry.item
}
