package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// AnalogizeService implements the ANALOGIZE memory algebra operation.
//
// Scientific basis: Structure Mapping Theory (Gentner, 1983) argues that
// analogies transfer relational structure, not surface features.
// Two memories are analogous when they share structural relationships
// despite different domains/entities.
//
// In Hippocampus, we implement a practical approximation:
//  1. Embedding similarity captures semantic overlap
//  2. Tag-based structural similarity captures role/pattern overlap
//  3. Cross-project search enables genuine transfer learning
//
// Example: "We used retry + exponential backoff for MQTT reconnection"
// in project A can be analogized to "API client keeps failing on timeout"
// in project B — the structural pattern (retry strategy for unreliable I/O)
// transfers across domains.
type AnalogizeService struct {
	episodic   domain.EpisodicRepo
	semantic   domain.SemanticRepo
	embedding  domain.EmbeddingProvider
	project    *ProjectService
	llm        domain.LLMProvider
	logger     *slog.Logger
}

func NewAnalogizeService(
	episodic domain.EpisodicRepo,
	semantic domain.SemanticRepo,
	embedding domain.EmbeddingProvider,
	project *ProjectService,
	logger *slog.Logger,
	llm ...domain.LLMProvider,
) *AnalogizeService {
	svc := &AnalogizeService{
		episodic:  episodic,
		semantic:  semantic,
		embedding: embedding,
		project:   project,
		logger:    logger,
	}
	if len(llm) > 0 && llm[0] != nil {
		svc.llm = llm[0]
	}
	return svc
}

type AnalogizeRequest struct {
	Query         string     `json:"query"`
	SourceProject *uuid.UUID `json:"source_project,omitempty"`
	TargetProject *uuid.UUID `json:"target_project,omitempty"`
	Limit         int        `json:"limit,omitempty"`
}

type Analogy struct {
	SourceID          uuid.UUID `json:"source_id"`
	SourceProject     string    `json:"source_project"`
	SourceContent     string    `json:"source_content"`
	TargetID          uuid.UUID `json:"target_id"`
	TargetProject     string    `json:"target_project"`
	TargetContent     string    `json:"target_content"`
	SemanticSim       float64   `json:"semantic_similarity"`
	StructuralSim     float64   `json:"structural_similarity"`
	CompositeSim      float64   `json:"composite_similarity"`
	TransferInsight   string    `json:"transfer_insight"`
}

type AnalogizeResponse struct {
	Analogies []Analogy `json:"analogies"`
	Query     string    `json:"query"`
}

func (as *AnalogizeService) Analogize(ctx context.Context, req *AnalogizeRequest) (*AnalogizeResponse, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if req.Limit <= 0 {
		req.Limit = 5
	}

	queryEmb, err := as.embedding.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	projects, err := as.project.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	var sourceMemories []*domain.MemoryItem
	if req.SourceProject != nil {
		eps, _ := as.episodic.SearchSimilar(ctx, queryEmb, req.SourceProject, 10)
		for _, e := range eps {
			sourceMemories = append(sourceMemories, &e.MemoryItem)
		}
		sems, _ := as.semantic.SearchSimilar(ctx, queryEmb, req.SourceProject, 10)
		for _, s := range sems {
			sourceMemories = append(sourceMemories, &s.MemoryItem)
		}
	} else {
		eps, _ := as.episodic.SearchSimilar(ctx, queryEmb, nil, 10)
		for _, e := range eps {
			sourceMemories = append(sourceMemories, &e.MemoryItem)
		}
	}

	if len(sourceMemories) == 0 {
		return &AnalogizeResponse{Query: req.Query}, nil
	}

	projectNames := make(map[uuid.UUID]string)
	for _, p := range projects {
		projectNames[p.ID] = p.DisplayName
	}

	type candidate struct {
		source *domain.MemoryItem
		target *domain.MemoryItem
		semSim float64
		strSim float64
		comp   float64
	}

	var candidates []candidate

	for _, src := range sourceMemories {
		for _, p := range projects {
			if req.SourceProject != nil && p.ID == *req.SourceProject {
				continue
			}
			if req.TargetProject != nil && p.ID != *req.TargetProject {
				continue
			}

			targetEmb := src.Embedding
			if len(targetEmb) == 0 {
				targetEmb = queryEmb
			}

			pID := p.ID
			targets, _ := as.episodic.SearchSimilar(ctx, targetEmb, &pID, 5)
			for _, t := range targets {
				if t.Similarity < 0.50 {
					continue
				}
				strSim := tagOverlap(src.Tags, t.Tags)
				comp := 0.6*t.Similarity + 0.4*strSim
				candidates = append(candidates, candidate{
					source: src,
					target: &t.MemoryItem,
					semSim: t.Similarity,
					strSim: strSim,
					comp:   comp,
				})
			}

			semTargets, _ := as.semantic.SearchSimilar(ctx, targetEmb, &pID, 5)
			for _, t := range semTargets {
				if t.Similarity < 0.50 {
					continue
				}
				strSim := tagOverlap(src.Tags, t.Tags)
				comp := 0.6*t.Similarity + 0.4*strSim
				candidates = append(candidates, candidate{
					source: src,
					target: &t.MemoryItem,
					semSim: t.Similarity,
					strSim: strSim,
					comp:   comp,
				})
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].comp > candidates[j].comp
	})

	seen := make(map[string]bool)
	var analogies []Analogy

	for _, c := range candidates {
		if len(analogies) >= req.Limit {
			break
		}
		key := c.source.ID.String() + "-" + c.target.ID.String()
		if seen[key] {
			continue
		}
		seen[key] = true

		srcProj := "global"
		if c.source.ProjectID != nil {
			if name, ok := projectNames[*c.source.ProjectID]; ok {
				srcProj = name
			}
		}
		tgtProj := "global"
		if c.target.ProjectID != nil {
			if name, ok := projectNames[*c.target.ProjectID]; ok {
				tgtProj = name
			}
		}

		if srcProj == tgtProj {
			continue
		}

		insight := as.generateInsight(ctx, c.source.Content, c.target.Content, c.source.Tags, c.target.Tags, req.Query)

		analogies = append(analogies, Analogy{
			SourceID:        c.source.ID,
			SourceProject:   srcProj,
			SourceContent:   truncateContent(c.source.Content, 200),
			TargetID:        c.target.ID,
			TargetProject:   tgtProj,
			TargetContent:   truncateContent(c.target.Content, 200),
			SemanticSim:     math.Round(c.semSim*1000) / 1000,
			StructuralSim:   math.Round(c.strSim*1000) / 1000,
			CompositeSim:    math.Round(c.comp*1000) / 1000,
			TransferInsight: insight,
		})
	}

	as.logger.Info("analogize completed",
		"query_len", len(req.Query),
		"source_memories", len(sourceMemories),
		"candidates", len(candidates),
		"analogies", len(analogies),
	)

	return &AnalogizeResponse{
		Analogies: analogies,
		Query:     req.Query,
	}, nil
}

func tagOverlap(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := make(map[string]bool)
	for _, t := range a {
		set[t] = true
	}
	overlap := 0
	for _, t := range b {
		if set[t] {
			overlap++
		}
	}
	union := len(a) + len(b) - overlap
	if union == 0 {
		return 0
	}
	return float64(overlap) / float64(union)
}

func (as *AnalogizeService) generateInsight(ctx context.Context, srcContent, tgtContent string, srcTags, tgtTags []string, query string) string {
	if as.llm != nil {
		srcSnip := srcContent
		if len(srcSnip) > 300 {
			srcSnip = srcSnip[:300]
		}
		tgtSnip := tgtContent
		if len(tgtSnip) > 300 {
			tgtSnip = tgtSnip[:300]
		}
		prompt := fmt.Sprintf(
			"Given a user query: %q\n\nSource memory (project A):\n%s\n\nTarget memory (project B):\n%s\n\n"+
				"In 1-2 sentences, explain what specific pattern or approach transfers between these two. "+
				"Be concrete about the technique, not generic. If they share nothing useful, say 'No meaningful transfer.'",
			query, srcSnip, tgtSnip,
		)
		insight, err := as.llm.Chat(ctx, []domain.ChatMessage{
			{Role: "system", Content: "You are a senior engineer identifying cross-project patterns. Be specific and actionable."},
			{Role: "user", Content: prompt},
		}, domain.ChatOptions{Temperature: 0.3, MaxTokens: 150})
		if err == nil && len(strings.TrimSpace(insight)) > 10 {
			return strings.TrimSpace(insight)
		}
		as.logger.Debug("LLM insight generation failed, falling back to template", "error", err)
	}
	return generateTransferInsight(srcContent, tgtContent, srcTags, tgtTags)
}

func generateTransferInsight(srcContent, tgtContent string, srcTags, tgtTags []string) string {
	var common []string
	tagSet := make(map[string]bool)
	for _, t := range srcTags {
		tagSet[t] = true
	}
	for _, t := range tgtTags {
		if tagSet[t] {
			common = append(common, t)
		}
	}

	var b strings.Builder
	b.WriteString("Pattern shared across projects")
	if len(common) > 0 {
		b.WriteString(" (common: ")
		b.WriteString(strings.Join(common, ", "))
		b.WriteString(")")
	}
	b.WriteString(". The approach used in one project may apply to the other.")

	srcLower := strings.ToLower(srcContent)
	tgtLower := strings.ToLower(tgtContent)
	if strings.Contains(srcLower, "error") && strings.Contains(tgtLower, "error") {
		b.WriteString(" Both involve error patterns — prevention strategy may transfer.")
	}
	if strings.Contains(srcLower, "decision") && strings.Contains(tgtLower, "decision") {
		b.WriteString(" Both record architectural decisions — reasoning may apply.")
	}

	return b.String()
}

func truncateContent(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
