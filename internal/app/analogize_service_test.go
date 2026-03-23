package app

import (
	"testing"
)

func TestTruncateContent(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"this is longer than five", 5, "this ..."},
		{"exact", 5, "exact"},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncateContent(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncateContent(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestGenerateTransferInsight(t *testing.T) {
	insight := generateTransferInsight(
		"ERROR: database connection timeout",
		"ERROR: API timeout on external service",
		[]string{"error", "timeout", "database"},
		[]string{"error", "timeout", "api"},
	)

	if insight == "" {
		t.Fatal("insight should not be empty")
	}
	if len(insight) < 20 {
		t.Errorf("insight too short: %q", insight)
	}
}

func TestGenerateTransferInsight_Decisions(t *testing.T) {
	insight := generateTransferInsight(
		"DECISION: use pgx instead of database/sql",
		"DECISION: use sqlx instead of raw queries",
		[]string{"decision", "database"},
		[]string{"decision", "database"},
	)

	if insight == "" {
		t.Fatal("insight should not be empty")
	}
}
