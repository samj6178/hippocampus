package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
	"github.com/hippocampus-mcp/hippocampus/internal/pkg/roomclass"
)

// ConversationMiner extracts valuable memories from Claude Code session JSONL files.
// Filters for decision markers, error markers, and architecture discussions.
// Stores extracted exchanges as episodic memories with room tags.
type ConversationMiner struct {
	encode *EncodeService
	logger *slog.Logger
}

func NewConversationMiner(encode *EncodeService, logger *slog.Logger) *ConversationMiner {
	return &ConversationMiner{encode: encode, logger: logger}
}

// MineResult holds the summary of a mining operation.
type MineResult struct {
	FilesScanned      int `json:"files_scanned"`
	MessagesRead      int `json:"messages_read"`
	ExchangesExtracted int `json:"exchanges_extracted"`
	MemoriesCreated   int `json:"memories_created"`
	DuplicatesSkipped int `json:"duplicates_skipped"`
}

// claudeMessage represents a single message in a Claude Code JSONL session.
type claudeMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role    string `json:"role"`
		Content any    `json:"content"` // string or []contentBlock
	} `json:"message"`
}

// contentBlock represents a text or thinking block in assistant content.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// MineSessionsDir scans a Claude Code project sessions directory and extracts
// valuable exchanges as memories. Only processes user+assistant pairs that
// contain decision, error, or architecture markers.
func (m *ConversationMiner) MineSessionsDir(ctx context.Context, sessionsDir string, projectID *uuid.UUID) (*MineResult, error) {
	result := &MineResult{}

	files, err := filepath.Glob(filepath.Join(sessionsDir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("glob sessions: %w", err)
	}

	for _, f := range files {
		r, err := m.mineFile(ctx, f, projectID)
		if err != nil {
			m.logger.Warn("mine file failed", "file", f, "error", err)
			continue
		}
		result.FilesScanned++
		result.MessagesRead += r.MessagesRead
		result.ExchangesExtracted += r.ExchangesExtracted
		result.MemoriesCreated += r.MemoriesCreated
		result.DuplicatesSkipped += r.DuplicatesSkipped
	}

	return result, nil
}

func (m *ConversationMiner) mineFile(ctx context.Context, path string, projectID *uuid.UUID) (*MineResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	result := &MineResult{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024) // 1MB max line

	var lastUserContent string
	var lastUserTime string

	for scanner.Scan() {
		var msg claudeMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}

		if msg.Type == "user" {
			content := extractTextContent(msg.Message.Content)
			// Skip system-generated content that leaks into user messages.
			if isSystemContent(content) {
				continue
			}
			lastUserContent = content
			lastUserTime = msg.Timestamp
			result.MessagesRead++
			continue
		}

		if msg.Type == "assistant" && lastUserContent != "" {
			result.MessagesRead++
			assistantContent := extractTextContent(msg.Message.Content)

			// Skip system-generated assistant responses.
			if isSystemContent(assistantContent) {
				lastUserContent = ""
				continue
			}

			// Form exchange: user question + assistant response
			exchange := lastUserContent + "\n---\n" + assistantContent
			lastUserContent = ""

			// Filter: only keep exchanges with valuable markers
			if !hasValueMarkers(exchange) {
				continue
			}

			// Truncate long exchanges
			if len(exchange) > 2000 {
				exchange = exchange[:2000]
			}

			result.ExchangesExtracted++

			room := roomclass.Classify(exchange)
			tags := []string{"mined_conversation", roomclass.Tag(room)}

			ts, _ := time.Parse(time.RFC3339, lastUserTime)
			if ts.IsZero() {
				ts = time.Now()
			}

			resp, err := m.encode.Encode(ctx, &EncodeRequest{
				Content:    exchange,
				ProjectID:  projectID,
				AgentID:    "conversation-miner",
				SessionID:  uuid.New(),
				Importance: 0.6,
				Tags:       tags,
				Metadata:   domain.Metadata{"source": "claude_code_session", "mined_at": time.Now().UTC().Format(time.RFC3339)},
			})
			if err != nil {
				m.logger.Warn("encode mined exchange failed", "error", err)
				continue
			}

			if resp.Encoded {
				result.MemoriesCreated++
			} else {
				result.DuplicatesSkipped++
			}
		}
	}

	return result, scanner.Err()
}

// extractTextContent handles both string content and []contentBlock content.
func extractTextContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var texts []string
		for _, block := range v {
			if m, ok := block.(map[string]any); ok {
				if m["type"] == "text" {
					if t, ok := m["text"].(string); ok {
						texts = append(texts, t)
					}
				}
			}
		}
		return strings.Join(texts, "\n")
	default:
		return fmt.Sprintf("%v", content)
	}
}

// isSystemContent detects system-generated XML/template content that should not
// be mined as conversation. Catches <task-notification>, <system-reminder>,
// and other Claude Code internal content.
func isSystemContent(text string) bool {
	trimmed := strings.TrimSpace(text)
	for _, prefix := range systemPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

var systemPrefixes = []string{
	"<task-notification>",
	"<system-reminder>",
	"<tool-use-id>",
	"<task-id>",
	"Continue from where you left off",
	"No response requested",
}

// hasValueMarkers checks if an exchange contains decision, error, or architecture markers.
var valueMarkers = []string{
	// Decisions
	"decided", "decision", "chose", "chosen", "because we", "instead of",
	"trade-off", "approach", "opted for", "rationale",
	// Errors
	"error", "panic", "crash", "bug", "fixed", "root cause", "stack trace",
	// Architecture
	"architecture", "refactor", "interface", "pattern", "dependency",
	"clean architecture", "domain model",
	// Important findings
	"discovered", "important", "critical", "gotcha", "caveat", "warning",
	"breaking change",
}

func hasValueMarkers(text string) bool {
	lower := strings.ToLower(text)
	hits := 0
	for _, marker := range valueMarkers {
		if strings.Contains(lower, marker) {
			hits++
			if hits >= 2 {
				return true // need at least 2 markers to avoid false positives
			}
		}
	}
	return false
}
