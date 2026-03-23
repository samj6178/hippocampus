package app

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// ProceduralService manages procedural (how-to) memory: step-by-step action
// patterns with tracked success/failure rates.
//
// Neuroscience basis: procedural memory (basal ganglia) stores "how to do" knowledge.
// Unlike episodic (what happened) or semantic (what is true), procedural memories
// are optimized by repetition and outcome feedback.
//
// Auto-classification: content containing numbered steps, command sequences,
// or "how to" patterns is automatically stored as procedural memory.
type ProceduralService struct {
	procedural domain.ProceduralRepo
	embedding  domain.EmbeddingProvider
	logger     *slog.Logger
}

func NewProceduralService(
	procedural domain.ProceduralRepo,
	embedding domain.EmbeddingProvider,
	logger *slog.Logger,
) *ProceduralService {
	return &ProceduralService{
		procedural: procedural,
		embedding:  embedding,
		logger:     logger,
	}
}

var stepPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^\s*(?:step\s+)?\d+[\.\)]\s+.+`),
	regexp.MustCompile(`(?m)^\s*[-*]\s+(?:First|Then|Next|Finally|After|Before)\b`),
}

var howToPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:how to|steps to|procedure for|workflow for|recipe for|guide to)\b`),
	regexp.MustCompile(`(?i)(?:to deploy|to build|to fix|to migrate|to set up|to configure|to install)\b`),
}

// IsProcedural checks if content describes a procedure (sequence of steps).
func (ps *ProceduralService) IsProcedural(content string) bool {
	for _, re := range howToPatterns {
		if re.MatchString(content) {
			return true
		}
	}

	stepCount := 0
	for _, re := range stepPatterns {
		stepCount += len(re.FindAllString(content, -1))
	}
	return stepCount >= 2
}

// ExtractSteps parses numbered/bulleted steps from content.
func (ps *ProceduralService) ExtractSteps(content string) []domain.Step {
	re := regexp.MustCompile(`(?m)^\s*(?:step\s+)?(\d+)[\.\)]\s+(.+)`)
	matches := re.FindAllStringSubmatch(content, -1)

	if len(matches) == 0 {
		re2 := regexp.MustCompile(`(?m)^\s*[-*]\s+(.+)`)
		matches2 := re2.FindAllStringSubmatch(content, -1)
		var steps []domain.Step
		for i, m := range matches2 {
			steps = append(steps, domain.Step{
				Order:       i + 1,
				Description: strings.TrimSpace(m[1]),
			})
		}
		return steps
	}

	var steps []domain.Step
	for i, m := range matches {
		steps = append(steps, domain.Step{
			Order:       i + 1,
			Description: strings.TrimSpace(m[2]),
		})
	}
	return steps
}

// ClassifyTaskType infers the task category from content.
func (ps *ProceduralService) ClassifyTaskType(content string) string {
	lower := strings.ToLower(content)
	switch {
	case strings.Contains(lower, "deploy") || strings.Contains(lower, "release"):
		return "deployment"
	case strings.Contains(lower, "build") || strings.Contains(lower, "compile"):
		return "build"
	case strings.Contains(lower, "test") || strings.Contains(lower, "verify"):
		return "testing"
	case strings.Contains(lower, "debug") || strings.Contains(lower, "fix"):
		return "debugging"
	case strings.Contains(lower, "migrat") || strings.Contains(lower, "upgrade"):
		return "migration"
	case strings.Contains(lower, "setup") || strings.Contains(lower, "install") || strings.Contains(lower, "configure"):
		return "setup"
	case strings.Contains(lower, "refactor") || strings.Contains(lower, "restructure"):
		return "refactoring"
	default:
		return "general"
	}
}

// StoreIfProcedural checks content and stores as procedural if it's a step sequence.
// Returns the memory ID if stored, nil otherwise.
func (ps *ProceduralService) StoreIfProcedural(ctx context.Context, content string, projectID *uuid.UUID) (*uuid.UUID, error) {
	if !ps.IsProcedural(content) {
		return nil, nil
	}

	steps := ps.ExtractSteps(content)
	if len(steps) < 2 {
		return nil, nil
	}

	taskType := ps.ClassifyTaskType(content)

	emb, err := ps.embedding.Embed(ctx, content)
	if err != nil {
		return nil, fmt.Errorf("embed procedural: %w", err)
	}

	id := uuid.New()
	now := time.Now()
	mem := &domain.ProceduralMemory{
		MemoryItem: domain.MemoryItem{
			ID:           id,
			ProjectID:    projectID,
			Tier:         domain.TierProcedural,
			Content:      content,
			Embedding:    emb,
			Importance:   0.75,
			Confidence:   0.5,
			TokenCount:   estimateTokens(content),
			LastAccessed: now,
			CreatedAt:    now,
			UpdatedAt:    now,
			Tags:         []string{"procedure", taskType},
		},
		TaskType: taskType,
		Steps:    steps,
		Version:  1,
	}

	if err := ps.procedural.Insert(ctx, mem); err != nil {
		return nil, fmt.Errorf("insert procedural: %w", err)
	}

	ps.logger.Info("procedural memory created",
		"id", id,
		"task_type", taskType,
		"steps", len(steps),
	)

	return &id, nil
}

// TrackOutcome records success or failure for the most similar procedure.
func (ps *ProceduralService) TrackOutcome(ctx context.Context, description string, success bool, projectID *uuid.UUID) error {
	emb, err := ps.embedding.Embed(ctx, description)
	if err != nil {
		return fmt.Errorf("embed description: %w", err)
	}

	results, err := ps.procedural.SearchByTaskType(ctx, emb, projectID, 1)
	if err != nil || len(results) == 0 {
		return fmt.Errorf("no matching procedure found")
	}

	best := results[0]
	if best.Similarity < 0.6 {
		return fmt.Errorf("no procedure matches well enough (best sim: %.2f)", best.Similarity)
	}

	if success {
		return ps.procedural.IncrementSuccess(ctx, best.ID)
	}
	return ps.procedural.IncrementFailure(ctx, best.ID)
}
