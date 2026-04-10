package contenthash

import "testing"

func TestOf(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
	}{
		{"normal text", "hello world", 32},
		{"empty string", "", 32},
		{"with whitespace", "  hello world  ", 32},
		{"unicode", "привет мир", 32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Of(tt.input)
			if len(got) != tt.wantLen {
				t.Errorf("Of(%q) len = %d, want %d", tt.input, len(got), tt.wantLen)
			}
		})
	}
}

func TestOf_TrimConsistency(t *testing.T) {
	// Content that differs only by leading/trailing whitespace should produce the same hash.
	a := Of("  important finding  ")
	b := Of("important finding")
	if a != b {
		t.Errorf("trimmed content should produce same hash: %q != %q", a, b)
	}
}

func TestOf_Deterministic(t *testing.T) {
	a := Of("test content")
	b := Of("test content")
	if a != b {
		t.Errorf("same input should produce same hash: %q != %q", a, b)
	}
}

func TestOf_DifferentContent(t *testing.T) {
	a := Of("content A")
	b := Of("content B")
	if a == b {
		t.Error("different content should produce different hashes")
	}
}
