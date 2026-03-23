package embedding

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

type cacheEntry struct {
	key       string
	embedding []float32
}

// LRUCache is a concurrency-safe LRU cache for embedding vectors.
// Keys are SHA-256 hashes of (model_id + text) to handle long inputs
// without unbounded memory for map keys.
type LRUCache struct {
	mu       sync.RWMutex
	capacity int
	items    map[string]*list.Element
	order    *list.List
	hits     int64
	misses   int64
}

func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 1000
	}
	return &LRUCache{
		capacity: capacity,
		items:    make(map[string]*list.Element, capacity),
		order:    list.New(),
	}
}

func cacheKey(modelID, text string) string {
	h := sha256.New()
	h.Write([]byte(modelID))
	h.Write([]byte{0})
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

func (c *LRUCache) Get(modelID, text string) ([]float32, bool) {
	key := cacheKey(modelID, text)

	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		c.misses++
		return nil, false
	}

	c.order.MoveToFront(elem)
	c.hits++
	entry := elem.Value.(*cacheEntry)

	out := make([]float32, len(entry.embedding))
	copy(out, entry.embedding)
	return out, true
}

func (c *LRUCache) Put(modelID, text string, embedding []float32) {
	key := cacheKey(modelID, text)

	stored := make([]float32, len(embedding))
	copy(stored, embedding)

	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*cacheEntry).embedding = stored
		return
	}

	elem := c.order.PushFront(&cacheEntry{key: key, embedding: stored})
	c.items[key] = elem

	for c.order.Len() > c.capacity {
		back := c.order.Back()
		if back == nil {
			break
		}
		evicted := c.order.Remove(back).(*cacheEntry)
		delete(c.items, evicted.key)
	}
}

func (c *LRUCache) Stats() (hits, misses int64, size int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits, c.misses, c.order.Len()
}

func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element, c.capacity)
	c.order.Init()
	c.hits = 0
	c.misses = 0
}
