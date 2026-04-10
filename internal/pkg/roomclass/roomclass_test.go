package roomclass

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			"bug report",
			"ERROR: nil pointer panic in handler.go causing crash under load. Root cause was missing nil check.",
			RoomBugs,
		},
		{
			"architecture decision",
			"Decided to use clean architecture with adapter layer for all external dependencies. The domain model should not import any infrastructure packages.",
			RoomArchitecture, // "architecture", "adapter", "domain", "package" = 4 hits × 2 > "decided" 1 hit × 3
		},
		{
			"pure decision",
			"Decided to go with approach B instead of A because the trade-off favored simplicity. The rationale was clear.",
			RoomDecisions,
		},
		{
			"database migration",
			"Added migration to create episodic_memory table with content_hash column and FTS5 index for full-text search.",
			RoomDatabase,
		},
		{
			"API endpoint",
			"New REST endpoint GET /api/memories returns paginated list with JSON response and proper status codes.",
			RoomAPI,
		},
		{
			"testing strategy",
			"Table-driven tests with testify assertions covering happy path, error cases, and edge cases. Added mock for repository.",
			RoomTesting,
		},
		{
			"deployment config",
			"Updated docker compose with health checks. Nginx reverse proxy handles TLS termination and static file serving.",
			RoomDeployment,
		},
		{
			"security concern",
			"Found SQL injection vulnerability in search endpoint. User input must be parameterized, never concatenated.",
			RoomSecurity,
		},
		{
			"performance tuning",
			"Query latency improved from 200ms to 15ms by adding composite index. Connection pool p99 latency dropped significantly.",
			RoomPerformance,
		},
		{
			"generic content",
			"Today was a good day for coding.",
			RoomGeneral,
		},
		{
			"mixed content picks strongest",
			"Fixed critical panic in the database migration code that caused crash on startup.",
			RoomBugs, // bugs weight=3, database weight=2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.content)
			if got != tt.want {
				t.Errorf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTag(t *testing.T) {
	if got := Tag("architecture"); got != "room:architecture" {
		t.Errorf("Tag() = %q, want %q", got, "room:architecture")
	}
}

func TestFromTag(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"room:architecture", "architecture"},
		{"room:bugs", "bugs"},
		{"not-a-room", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := FromTag(tt.tag); got != tt.want {
			t.Errorf("FromTag(%q) = %q, want %q", tt.tag, got, tt.want)
		}
	}
}
