package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditLog provides an append-only JSONL write-ahead log for all memory
// mutations (remember, learn_error, session_end, kg operations).
// Not used for crash recovery (SQLite handles that), but for debugging
// and transparency: "what exactly was written to MOS in the last N sessions".
type AuditLog struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

// AuditEntry represents a single write operation recorded in the log.
type AuditEntry struct {
	Timestamp string `json:"ts"`
	Operation string `json:"op"`
	Project   string `json:"project,omitempty"`
	Summary   string `json:"summary"`
	MemoryID  string `json:"memory_id,omitempty"`
}

// NewAuditLog creates or opens an append-only JSONL file at dir/audit.jsonl.
// The directory is created if it doesn't exist (0700 permissions).
func NewAuditLog(dir string) (*AuditLog, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("audit log mkdir: %w", err)
	}
	path := filepath.Join(dir, "audit.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("audit log open: %w", err)
	}
	return &AuditLog{
		file: f,
		enc:  json.NewEncoder(f),
	}, nil
}

// Log appends an entry to the audit log. Thread-safe.
// Errors are swallowed — audit logging must never block the main path.
func (a *AuditLog) Log(op, project, summary, memoryID string) {
	if a == nil || a.file == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	entry := AuditEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Operation: op,
		Project:   project,
		Summary:   truncateAudit(summary, 500),
		MemoryID:  memoryID,
	}
	_ = a.enc.Encode(entry)
}

// Close flushes and closes the underlying file.
func (a *AuditLog) Close() error {
	if a == nil || a.file == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.file.Close()
}

func truncateAudit(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
