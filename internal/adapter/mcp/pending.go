package mcp

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	maxPendingTasks = 500
	pendingTaskTTL  = 24 * time.Hour
)

type PendingTask struct {
	ID        string         `json:"task_id"`
	Type      string         `json:"type"` // "synthesize", "generate_rule"
	Prompt    string         `json:"prompt"`
	ProjectID string         `json:"project_id,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type PendingTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*PendingTask
}

func NewPendingTaskStore() *PendingTaskStore {
	return &PendingTaskStore{tasks: make(map[string]*PendingTask)}
}

func (s *PendingTaskStore) Add(task *PendingTask) {
	if task.ID == "" {
		task.ID = uuid.New().String()
	}
	task.CreatedAt = time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupExpiredLocked()

	// Evict oldest if at capacity
	if len(s.tasks) >= maxPendingTasks {
		s.evictOldestLocked()
	}

	s.tasks[task.ID] = task
}

func (s *PendingTaskStore) Get(id string) (*PendingTask, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}

func (s *PendingTaskStore) Remove(id string) {
	s.mu.Lock()
	delete(s.tasks, id)
	s.mu.Unlock()
}

func (s *PendingTaskStore) List() []*PendingTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*PendingTask, 0, len(s.tasks))
	for _, t := range s.tasks {
		result = append(result, t)
	}
	return result
}

func (s *PendingTaskStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tasks)
}

// cleanupExpiredLocked removes tasks older than pendingTaskTTL.
// Caller must hold s.mu write lock.
func (s *PendingTaskStore) cleanupExpiredLocked() {
	cutoff := time.Now().Add(-pendingTaskTTL)
	for id, t := range s.tasks {
		if t.CreatedAt.Before(cutoff) {
			delete(s.tasks, id)
		}
	}
}

// evictOldestLocked removes the single oldest task.
// Caller must hold s.mu write lock.
func (s *PendingTaskStore) evictOldestLocked() {
	var oldestID string
	var oldestTime time.Time
	for id, t := range s.tasks {
		if oldestID == "" || t.CreatedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = t.CreatedAt
		}
	}
	if oldestID != "" {
		delete(s.tasks, oldestID)
	}
}
