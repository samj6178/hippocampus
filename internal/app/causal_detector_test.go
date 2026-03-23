package app

import (
	"testing"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

func TestCausalPatternMatching(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantRel  domain.CausalRelation
		wantHit  bool
	}{
		{
			name:    "caused by pattern",
			content: "Deployment failure caused by: missing environment variable DATABASE_URL.",
			wantRel: domain.RelCaused,
			wantHit: true,
		},
		{
			name:    "because pattern",
			content: "The build failed because the Go version was too old.",
			wantRel: domain.RelCaused,
			wantHit: true,
		},
		{
			name:    "led to pattern",
			content: "Missing index led to slow query performance on the episodic table.",
			wantRel: domain.RelCaused,
			wantHit: true,
		},
		{
			name:    "fixed pattern",
			content: "Fixed: added the missing import for filepath package.",
			wantRel: domain.RelPrevented,
			wantHit: true,
		},
		{
			name:    "prevented pattern",
			content: "Prevented data loss by adding a backup step before migration.",
			wantRel: domain.RelPrevented,
			wantHit: true,
		},
		{
			name:    "requires pattern",
			content: "This feature requires the pgvector extension to be installed.",
			wantRel: domain.RelRequired,
			wantHit: true,
		},
		{
			name:    "breaks pattern",
			content: "Changing the schema breaks the existing migration scripts.",
			wantRel: domain.RelDegraded,
			wantHit: true,
		},
		{
			name:    "enables pattern",
			content: "Adding pgvector enabled semantic similarity search across memories.",
			wantRel: domain.RelEnabled,
			wantHit: true,
		},
		{
			name:    "no causal pattern",
			content: "Updated the README with new installation instructions.",
			wantHit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit := false
			for _, cp := range causalPatterns {
				matches := cp.re.FindAllStringSubmatch(tt.content, 3)
				if len(matches) > 0 {
					hit = true
					if tt.wantHit && cp.relation != tt.wantRel {
						t.Errorf("matched relation %s, want %s", cp.relation, tt.wantRel)
					}
					break
				}
			}
			if hit != tt.wantHit {
				t.Errorf("pattern match = %v, want %v", hit, tt.wantHit)
			}
		})
	}
}

func TestTagOverlap(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want float64
	}{
		{"identical", []string{"a", "b", "c"}, []string{"a", "b", "c"}, 1.0},
		{"no overlap", []string{"a", "b"}, []string{"c", "d"}, 0.0},
		{"partial", []string{"a", "b", "c"}, []string{"b", "c", "d"}, 0.5},
		{"empty a", nil, []string{"a"}, 0.0},
		{"empty b", []string{"a"}, nil, 0.0},
		{"both empty", nil, nil, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tagOverlap(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("tagOverlap = %f, want %f", got, tt.want)
			}
		})
	}
}
