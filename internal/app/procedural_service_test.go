package app

import (
	"testing"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

func TestProceduralService_IsProcedural(t *testing.T) {
	ps := &ProceduralService{}

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "numbered steps",
			content: `How to deploy:
1. Build the binary
2. Copy to server
3. Restart service`,
			want: true,
		},
		{
			name: "bulleted steps with keywords",
			content: `Steps to fix the database migration:
- First, backup the current state
- Then, apply the migration
- Next, verify the schema
- Finally, restart the service`,
			want: true,
		},
		{
			name: "how-to pattern",
			content: "How to configure nginx reverse proxy for the application",
			want: true,
		},
		{
			name: "not procedural: plain fact",
			content: "We use PostgreSQL with TimescaleDB for time-series data",
			want: false,
		},
		{
			name: "not procedural: single step",
			content: "Run 'go build' to compile",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ps.IsProcedural(tt.content)
			if got != tt.want {
				t.Errorf("IsProcedural() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProceduralService_ExtractSteps(t *testing.T) {
	ps := &ProceduralService{}

	content := `Deploy procedure:
1. Run go build -o bin/app ./cmd/app
2. scp bin/app user@server:/opt/app/
3. ssh user@server "systemctl restart app"
4. Verify health endpoint responds`

	steps := ps.ExtractSteps(content)

	if len(steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(steps))
	}

	if steps[0].Order != 1 {
		t.Errorf("step 0 order = %d, want 1", steps[0].Order)
	}
	if steps[0].Description != "Run go build -o bin/app ./cmd/app" {
		t.Errorf("step 0 desc = %q", steps[0].Description)
	}
}

func TestProceduralService_ClassifyTaskType(t *testing.T) {
	ps := &ProceduralService{}

	tests := []struct {
		content string
		want    string
	}{
		{"How to deploy the service to production", "deployment"},
		{"Steps to build the Docker image", "build"},
		{"Procedure for running integration tests", "testing"},
		{"How to debug the MCP connection issue", "debugging"},
		{"Database migration from v2 to v3", "migration"},
		{"Setup development environment", "setup"},
		{"Refactor the handler layer", "refactoring"},
		{"Process for code review", "general"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := ps.ClassifyTaskType(tt.content)
			if got != tt.want {
				t.Errorf("ClassifyTaskType(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

func TestExtractSteps_BulletedList(t *testing.T) {
	ps := &ProceduralService{}

	content := `Fix steps:
- Check the error log
- Identify the failing component
- Apply the hotfix
- Run tests to verify`

	steps := ps.ExtractSteps(content)

	if len(steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(steps))
	}

	for i, step := range steps {
		if step.Order != i+1 {
			t.Errorf("step %d order = %d, want %d", i, step.Order, i+1)
		}
		if step.Description == "" {
			t.Errorf("step %d has empty description", i)
		}
	}
}

func TestProceduralStepDomain(t *testing.T) {
	step := domain.Step{
		Order:       1,
		Description: "Build the binary",
		Tool:        "go build",
		Expected:    "binary in bin/",
	}

	if step.Order != 1 {
		t.Error("step order wrong")
	}
	if step.Tool != "go build" {
		t.Error("step tool wrong")
	}
}
