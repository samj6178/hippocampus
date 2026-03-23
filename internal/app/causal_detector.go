package app

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// CausalDetector extracts cause-effect relationships from content and
// creates CausalLinks. Runs automatically during ENCODE.
//
// Detection strategy:
//  1. Pattern matching for causal language ("caused by", "because", "led to")
//  2. Semantic proximity: if two memories have high similarity and temporal
//     ordering, infer a potential causal link with low confidence.
//
// Causal links are weighted by confidence and accumulate evidence over time.
type CausalDetector struct {
	causal    domain.CausalRepo
	episodic  domain.EpisodicRepo
	embedding domain.EmbeddingProvider
	logger    *slog.Logger
}

func NewCausalDetector(
	causal domain.CausalRepo,
	episodic domain.EpisodicRepo,
	embedding domain.EmbeddingProvider,
	logger *slog.Logger,
) *CausalDetector {
	return &CausalDetector{
		causal:    causal,
		episodic:  episodic,
		embedding: embedding,
		logger:    logger,
	}
}

var causalPatterns = []struct {
	re       *regexp.Regexp
	relation domain.CausalRelation
}{
	{regexp.MustCompile(`(?i)(?:caused|causes|cause)\s+(?:by|:)\s*(.+?)(?:\.|$)`), domain.RelCaused},
	{regexp.MustCompile(`(?i)(?:because|since|due to)\s+(.+?)(?:,|\.|$)`), domain.RelCaused},
	{regexp.MustCompile(`(?i)(?:led to|leads to|resulted in|results in)\s+(.+?)(?:\.|$)`), domain.RelCaused},
	{regexp.MustCompile(`(?i)(?:fix(?:ed)?|resolv(?:ed|es))\s*:?\s*(.+?)(?:\.|$)`), domain.RelPrevented},
	{regexp.MustCompile(`(?i)(?:prevent(?:ed|s)?|avoid(?:ed|s)?)\s*:?\s*(.+?)(?:\.|$)`), domain.RelPrevented},
	{regexp.MustCompile(`(?i)(?:requir(?:ed|es)|depends on|dependency)\s*:?\s*(.+?)(?:\.|$)`), domain.RelRequired},
	{regexp.MustCompile(`(?i)(?:breaks?|broke|degrades?|degraded)\s+(.+?)(?:\.|$)`), domain.RelDegraded},
	{regexp.MustCompile(`(?i)(?:enabl(?:ed|es)|unlock(?:ed|s)|allows?)\s+(.+?)(?:\.|$)`), domain.RelEnabled},
}

// DetectAndStore analyzes content for causal language and creates links
// to related existing memories.
func (cd *CausalDetector) DetectAndStore(ctx context.Context, memoryID uuid.UUID, content string, emb []float32, projectID *uuid.UUID) int {
	created := 0

	for _, cp := range causalPatterns {
		matches := cp.re.FindAllStringSubmatch(content, 3)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			effectDesc := strings.TrimSpace(m[1])
			if len(effectDesc) < 5 {
				continue
			}

			related := cd.findRelated(ctx, effectDesc, emb, projectID, memoryID)
			if related == nil {
				continue
			}

			link := &domain.CausalLink{
				ID:        uuid.New(),
				CauseID:   memoryID,
				CauseTier: domain.TierEpisodic,
				EffectID:  related.ID,
				EffectTier: related.Tier,
				Relation:  cp.relation,
				Confidence: 0.6,
				Evidence:  []uuid.UUID{memoryID},
				BoundaryConditions: effectDesc,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			if err := cd.causal.Insert(ctx, link); err != nil {
				cd.logger.Warn("causal link insert failed", "error", err)
				continue
			}
			created++
			cd.logger.Info("causal link created",
				"cause", memoryID,
				"effect", related.ID,
				"relation", cp.relation,
				"pattern", effectDesc[:min(len(effectDesc), 50)],
			)
		}
	}

	return created
}

func (cd *CausalDetector) findRelated(ctx context.Context, desc string, sourceEmb []float32, projectID *uuid.UUID, excludeID uuid.UUID) *domain.MemoryItem {
	similar, err := cd.episodic.SearchSimilar(ctx, sourceEmb, projectID, 5)
	if err != nil {
		return nil
	}

	descLower := strings.ToLower(desc)
	for _, ep := range similar {
		if ep.ID == excludeID {
			continue
		}
		if ep.Similarity < 0.5 {
			continue
		}
		contentLower := strings.ToLower(ep.Content)
		if strings.Contains(contentLower, descLower) || ep.Similarity > 0.7 {
			return &ep.MemoryItem
		}
	}
	return nil
}

// GetCausalContext retrieves causal links for memories in a recall result,
// returning a formatted causal reasoning section.
func (cd *CausalDetector) GetCausalContext(ctx context.Context, memoryIDs []uuid.UUID) string {
	if len(memoryIDs) == 0 {
		return ""
	}

	var links []*domain.CausalLink
	seen := make(map[uuid.UUID]bool)

	for _, id := range memoryIDs {
		effects, err := cd.causal.GetEffects(ctx, id)
		if err == nil {
			for _, l := range effects {
				if !seen[l.ID] {
					links = append(links, l)
					seen[l.ID] = true
				}
			}
		}
		causes, err := cd.causal.GetCauses(ctx, id)
		if err == nil {
			for _, l := range causes {
				if !seen[l.ID] {
					links = append(links, l)
					seen[l.ID] = true
				}
			}
		}
	}

	if len(links) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n--- CAUSAL RELATIONSHIPS ---\n")
	for _, l := range links {
		rel := string(l.Relation)
		b.WriteString("• ")
		b.WriteString(l.CauseID.String()[:8])
		b.WriteString(" -[")
		b.WriteString(rel)
		b.WriteString("]-> ")
		b.WriteString(l.EffectID.String()[:8])
		if l.BoundaryConditions != "" {
			b.WriteString(" (")
			cond := l.BoundaryConditions
			if len(cond) > 80 {
				cond = cond[:80] + "..."
			}
			b.WriteString(cond)
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
