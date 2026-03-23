package mcp

import (
	"sync"
	"time"

	"github.com/google/uuid"
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
	s.tasks[task.ID] = task
	s.mu.Unlock()
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
