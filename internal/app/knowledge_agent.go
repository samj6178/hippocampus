package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/adapter/source"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// ScientificFinding is a structured extraction from a research paper.
type ScientificFinding struct {
	Title        string   `json:"title"`
	Authors      []string `json:"authors,omitempty"`
	Source       string   `json:"source"`
	URL          string   `json:"url,omitempty"`
	DOI          string   `json:"doi,omitempty"`
	KeyFindings  []string `json:"key_findings"`
	Methods      string   `json:"methods,omitempty"`
	Limitations  string   `json:"limitations,omitempty"`
	Applications []string `json:"applications,omitempty"`
	Domain       string   `json:"domain"`
	Citations    int      `json:"citations"`
	Year         int      `json:"year,omitempty"`
	Venue        string   `json:"venue,omitempty"`
	QualityScore float64  `json:"quality_score"`
	Confidence   float64  `json:"confidence"`
}

// KnowledgeAgent is a domain-specialized research agent.
// Each agent focuses on a specific scientific domain and uses
// domain-appropriate sources and query templates.
type KnowledgeAgent struct {
	name           string
	agentDomain    string
	sources        []source.Adapter
	queryTemplates []string
	llm            domain.LLMProvider
	encode         *EncodeService
	embedding      domain.EmbeddingProvider
	logger         *slog.Logger
}

type KnowledgeAgentConfig struct {
	Name           string
	Domain         string
	Sources        []source.Adapter
	QueryTemplates []string
}

func NewKnowledgeAgent(
	cfg KnowledgeAgentConfig,
	llm domain.LLMProvider,
	encode *EncodeService,
	embedding domain.EmbeddingProvider,
	logger *slog.Logger,
) *KnowledgeAgent {
	return &KnowledgeAgent{
		name:           cfg.Name,
		agentDomain:    cfg.Domain,
		sources:        cfg.Sources,
		queryTemplates: cfg.QueryTemplates,
		llm:            llm,
		encode:         encode,
		embedding:      embedding,
		logger:         logger,
	}
}

func (ka *KnowledgeAgent) AgentName() string  { return ka.name }
func (ka *KnowledgeAgent) AgentDomain() string { return ka.agentDomain }

// Research executes a research query across all configured sources,
// extracts structured findings via LLM, scores quality, and stores to MOS.
func (ka *KnowledgeAgent) Research(ctx context.Context, query string, projectID *uuid.UUID, maxPerSource int) ([]ScientificFinding, error) {
	if maxPerSource <= 0 {
		maxPerSource = 5
	}

	start := time.Now()
	ka.logger.Info("research started", "agent", ka.name, "query", query)

	var allRaw []source.RawResult
	for _, src := range ka.sources {
		results, err := src.Search(ctx, query, maxPerSource)
		if err != nil {
			ka.logger.Warn("source search failed", "source", src.Name(), "error", err)
			continue
		}
		allRaw = append(allRaw, results...)
	}

	if len(allRaw) == 0 {
		return nil, nil
	}

	var findings []ScientificFinding
	for _, raw := range allRaw {
		finding := ka.extractFinding(ctx, raw, query)
		if finding != nil {
			findings = append(findings, *finding)
		}
	}

	stored := 0
	for _, f := range findings {
		if f.QualityScore < 0.3 {
			continue
		}
		if err := ka.storeFinding(ctx, f, projectID); err != nil {
			ka.logger.Warn("failed to store finding", "title", f.Title, "error", err)
			continue
		}
		stored++
	}

	ka.logger.Info("research completed",
		"agent", ka.name,
		"raw_results", len(allRaw),
		"findings", len(findings),
		"stored", stored,
		"duration", time.Since(start),
	)

	return findings, nil
}

func (ka *KnowledgeAgent) extractFinding(ctx context.Context, raw source.RawResult, query string) *ScientificFinding {
	if raw.Abstract == "" && raw.Title == "" {
		return nil
	}

	finding := &ScientificFinding{
		Title:     raw.Title,
		Authors:   raw.Authors,
		Source:    raw.Source,
		URL:       raw.URL,
		DOI:       raw.DOI,
		Year:      raw.Year,
		Citations: raw.Citations,
		Venue:     raw.Venue,
		Domain:    ka.agentDomain,
	}

	if ka.llm != nil && raw.Abstract != "" {
		extracted := ka.llmExtract(ctx, raw)
		if extracted != nil {
			finding.KeyFindings = extracted.KeyFindings
			finding.Methods = extracted.Methods
			finding.Limitations = extracted.Limitations
			finding.Applications = extracted.Applications
			if extracted.Domain != "" {
				finding.Domain = extracted.Domain
			}
		}
	}

	if len(finding.KeyFindings) == 0 {
		abstract := raw.Abstract
		if len(abstract) > 300 {
			abstract = abstract[:300]
		}
		finding.KeyFindings = []string{abstract}
	}

	finding.QualityScore = qualityScore(finding, query)
	finding.Confidence = math.Min(finding.QualityScore+0.1, 1.0)

	return finding
}

type llmExtraction struct {
	KeyFindings  []string `json:"key_findings"`
	Methods      string   `json:"methods"`
	Limitations  string   `json:"limitations"`
	Applications []string `json:"applications"`
	Domain       string   `json:"domain"`
}

func (ka *KnowledgeAgent) llmExtract(ctx context.Context, raw source.RawResult) *llmExtraction {
	prompt := fmt.Sprintf(`Extract structured information from this paper. Respond in JSON only.

Title: %s
Abstract: %s

{
  "key_findings": ["finding1", "finding2"],
  "methods": "brief description of methods",
  "limitations": "stated or inferred limitations",
  "applications": ["practical application 1"],
  "domain": "primary scientific domain"
}`, raw.Title, raw.Abstract)

	result, err := ka.llm.Chat(ctx, []domain.ChatMessage{
		{Role: "user", Content: prompt},
	}, domain.ChatOptions{Temperature: 0.1, MaxTokens: 500})
	if err != nil {
		ka.logger.Warn("LLM extraction failed", "title", raw.Title, "error", err)
		return nil
	}

	result = strings.TrimSpace(result)
	jsonStart := strings.Index(result, "{")
	jsonEnd := strings.LastIndex(result, "}")
	if jsonStart >= 0 && jsonEnd > jsonStart {
		result = result[jsonStart : jsonEnd+1]
	}

	var extracted llmExtraction
	if err := json.Unmarshal([]byte(result), &extracted); err != nil {
		ka.logger.Debug("LLM extraction parse failed", "error", err)
		return nil
	}

	return &extracted
}

// qualityScore computes a quality metric for a scientific finding.
// Q(f) = w_cite * log(1+citations)/log(1+maxCitations) + w_recent * exp(-0.1*age) + w_prestige * prestige(source) + w_relevance * 0.5
func qualityScore(f *ScientificFinding, query string) float64 {
	const wCite, wRecent, wPrestige, wRelevance = 0.3, 0.2, 0.2, 0.3

	citationScore := 0.0
	if f.Citations > 0 {
		citationScore = math.Log(1+float64(f.Citations)) / math.Log(1+10000)
		if citationScore > 1 {
			citationScore = 1
		}
	}

	recencyScore := 1.0
	if f.Year > 0 {
		age := float64(time.Now().Year() - f.Year)
		recencyScore = math.Exp(-0.1 * age)
	}

	prestigeScore := sourcePrestige(f.Source, f.Venue)

	relevanceScore := 0.5
	if len(query) > 0 && len(f.Title) > 0 {
		lowerQuery := strings.ToLower(query)
		lowerTitle := strings.ToLower(f.Title)
		words := strings.Fields(lowerQuery)
		matches := 0
		for _, w := range words {
			if len(w) > 2 && strings.Contains(lowerTitle, w) {
				matches++
			}
		}
		if len(words) > 0 {
			relevanceScore = float64(matches) / float64(len(words))
		}
	}

	return wCite*citationScore + wRecent*recencyScore + wPrestige*prestigeScore + wRelevance*relevanceScore
}

func sourcePrestige(source, venue string) float64 {
	venueLower := strings.ToLower(venue)

	topVenues := map[string]float64{
		"nature": 1.0, "science": 1.0,
		"neurips": 0.95, "nips": 0.95, "icml": 0.95, "iclr": 0.95,
		"jmlr": 0.9, "tmlr": 0.9, "cvpr": 0.9, "acl": 0.9, "emnlp": 0.9,
	}
	for k, v := range topVenues {
		if strings.Contains(venueLower, k) {
			return v
		}
	}

	sourcePrestigeMap := map[string]float64{
		"papers_with_code":  0.85,
		"pubmed":            0.8,
		"arxiv":             0.7,
		"semantic_scholar":  0.7,
		"github":            0.5,
		"hackernews":        0.3,
	}
	if p, ok := sourcePrestigeMap[source]; ok {
		return p
	}
	return 0.5
}

func (ka *KnowledgeAgent) storeFinding(ctx context.Context, f ScientificFinding, projectID *uuid.UUID) error {
	var content strings.Builder
	content.WriteString(fmt.Sprintf("[%s] %s\n", f.Source, f.Title))
	if len(f.Authors) > 0 {
		content.WriteString(fmt.Sprintf("Authors: %s\n", strings.Join(f.Authors, ", ")))
	}
	if f.URL != "" {
		content.WriteString(fmt.Sprintf("URL: %s\n", f.URL))
	}
	if f.DOI != "" {
		content.WriteString(fmt.Sprintf("DOI: %s\n", f.DOI))
	}
	content.WriteString(fmt.Sprintf("Quality: %.2f | Citations: %d | Domain: %s\n\n", f.QualityScore, f.Citations, f.Domain))

	if len(f.KeyFindings) > 0 {
		content.WriteString("KEY FINDINGS:\n")
		for _, kf := range f.KeyFindings {
			content.WriteString(fmt.Sprintf("- %s\n", kf))
		}
	}
	if f.Methods != "" {
		content.WriteString(fmt.Sprintf("\nMETHODS: %s\n", f.Methods))
	}
	if f.Limitations != "" {
		content.WriteString(fmt.Sprintf("LIMITATIONS: %s\n", f.Limitations))
	}
	if len(f.Applications) > 0 {
		content.WriteString(fmt.Sprintf("APPLICATIONS: %s\n", strings.Join(f.Applications, "; ")))
	}

	tags := []string{"research", f.Domain, f.Source}
	if f.Venue != "" {
		tags = append(tags, f.Venue)
	}

	importance := f.QualityScore
	if importance < 0.4 {
		importance = 0.4
	}

	_, err := ka.encode.Encode(ctx, &EncodeRequest{
		Content:    content.String(),
		AgentID:    "knowledge-agent-" + ka.name,
		SessionID:  uuid.New(),
		ProjectID:  projectID,
		Tags:       tags,
		Importance: importance,
	})
	return err
}

// DefaultAgentConfigs returns the 6 pre-configured agent specifications.
func DefaultAgentConfigs() []KnowledgeAgentConfig {
	return []KnowledgeAgentConfig{
		{
			Name:   "alpha",
			Domain: "mathematics",
			Sources: []source.Adapter{
				source.NewArxivAdapter("math.OC", "math.NA", "math.CO", "math.ST"),
			},
			QueryTemplates: []string{
				"optimal algorithm for %s",
				"computational complexity of %s",
				"approximation bounds for %s",
				"mathematical foundations of %s",
			},
		},
		{
			Name:   "beta",
			Domain: "computer_science",
			Sources: []source.Adapter{
				source.NewArxivAdapter("cs.AI", "cs.LG", "cs.CL", "cs.CV"),
				source.NewSemanticScholarAdapter(),
				source.NewGitHubAdapter(),
			},
			QueryTemplates: []string{
				"state-of-the-art %s benchmark",
				"neural architecture for %s",
				"efficient implementation of %s",
				"survey of %s methods",
			},
		},
		{
			Name:   "gamma",
			Domain: "physics_engineering",
			Sources: []source.Adapter{
				source.NewArxivAdapter("physics.comp-ph", "eess.SP", "eess.SY"),
				source.NewSemanticScholarAdapter(),
			},
			QueryTemplates: []string{
				"physical model of %s",
				"engineering approach to %s",
				"simulation of %s dynamics",
			},
		},
		{
			Name:   "delta",
			Domain: "biology_chemistry",
			Sources: []source.Adapter{
				source.NewPubMedAdapter(),
			},
			QueryTemplates: []string{
				"mechanism of %s",
				"molecular basis of %s",
				"therapeutic target for %s",
			},
		},
		{
			Name:   "epsilon",
			Domain: "systems_practice",
			Sources: []source.Adapter{
				source.NewGitHubAdapter(),
				source.NewHackerNewsAdapter(),
			},
			QueryTemplates: []string{
				"production deployment of %s",
				"scaling %s in practice",
				"best practices for %s",
				"performance optimization %s",
			},
		},
		{
			Name:   "zeta",
			Domain: "ml_data_science",
			Sources: []source.Adapter{
				source.NewPapersWithCodeAdapter(),
				source.NewArxivAdapter("stat.ML", "cs.LG"),
				source.NewSemanticScholarAdapter("NeurIPS", "ICML", "ICLR", "JMLR", "TMLR"),
			},
			QueryTemplates: []string{
				"state-of-the-art %s benchmark",
				"theoretical analysis of %s",
				"scaling laws for %s",
				"sample complexity of %s",
				"foundation model for %s",
			},
		},
	}
}
