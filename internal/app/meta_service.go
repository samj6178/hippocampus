package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// MetaService implements the META memory algebra operation.
//
// Scientific basis: Metacognition (Flavell, 1979) — "thinking about thinking."
// The system self-assesses:
//  1. Calibration: does it know what it knows? (overconfidence detection)
//  2. Knowledge gaps: what areas are frequently queried but poorly covered?
//  3. Memory health: distribution, decay, consolidation status
//  4. Strategy effectiveness: which memory strategies produce useful results?
//
// This enables the system to say "I am confident about X but uncertain about Y"
// and to proactively identify areas needing more knowledge.
type MetaService struct {
	episodic   domain.EpisodicRepo
	semantic   domain.SemanticRepo
	procedural domain.ProceduralRepo
	prediction *PredictionService
	eval       *EvalFramework
	project    *ProjectService
	logger     *slog.Logger
}

func NewMetaService(
	episodic domain.EpisodicRepo,
	semantic domain.SemanticRepo,
	procedural domain.ProceduralRepo,
	prediction *PredictionService,
	eval *EvalFramework,
	project *ProjectService,
	logger *slog.Logger,
) *MetaService {
	return &MetaService{
		episodic:   episodic,
		semantic:   semantic,
		procedural: procedural,
		prediction: prediction,
		eval:       eval,
		project:    project,
		logger:     logger,
	}
}

type MetaReport struct {
	MemoryDistribution  MemoryDistribution       `json:"memory_distribution"`
	DomainCalibration   map[string]CalibrationInfo `json:"domain_calibration"`
	KnowledgeGaps       []KnowledgeGap            `json:"knowledge_gaps"`
	Strengths           []string                  `json:"strengths"`
	Weaknesses          []string                  `json:"weaknesses"`
	Recommendations     []string                  `json:"recommendations"`
	OverallHealth       float64                   `json:"overall_health"` // 0-1
}

type MemoryDistribution struct {
	Episodic   int `json:"episodic"`
	Semantic   int `json:"semantic"`
	Procedural int `json:"procedural"`
	Total      int `json:"total"`
}

type CalibrationInfo struct {
	Domain            string  `json:"domain"`
	Predictions       int     `json:"predictions"`
	AvgConfidence     float64 `json:"avg_confidence"`
	ActualAccuracy    float64 `json:"actual_accuracy"`
	CalibrationError  float64 `json:"calibration_error"` // |confidence - accuracy|
	IsOverconfident   bool    `json:"is_overconfident"`
}

type KnowledgeGap struct {
	Area        string  `json:"area"`
	Description string  `json:"description"`
	Severity    float64 `json:"severity"` // 0-1
}

func (ms *MetaService) Assess(ctx context.Context, projectID *uuid.UUID) (*MetaReport, error) {
	report := &MetaReport{
		DomainCalibration: make(map[string]CalibrationInfo),
	}

	epCount, _ := ms.episodic.Count(ctx, projectID)
	semCount, _ := ms.semantic.Count(ctx, projectID)
	procCount, _ := ms.procedural.Count(ctx, projectID)
	report.MemoryDistribution = MemoryDistribution{
		Episodic:   epCount,
		Semantic:   semCount,
		Procedural: procCount,
		Total:      epCount + semCount + procCount,
	}

	calibration := ms.prediction.GetCalibration()
	for domain, cal := range calibration {
		calErr := cal.CalibrationOffset
		if calErr < 0 {
			calErr = -calErr
		}
		report.DomainCalibration[domain] = CalibrationInfo{
			Domain:           domain,
			Predictions:      cal.SampleCount,
			AvgConfidence:    cal.PredictedConfidence,
			ActualAccuracy:   cal.ActualAccuracy,
			CalibrationError: calErr,
			IsOverconfident:  cal.CalibrationOffset > 0.1,
		}
	}

	report.KnowledgeGaps = ms.detectGaps(ctx, projectID)

	report.Strengths = ms.identifyStrengths(report)
	report.Weaknesses = ms.identifyWeaknesses(report)
	report.Recommendations = ms.generateRecommendations(report)
	report.OverallHealth = ms.computeHealth(report)

	ms.logger.Info("meta assessment completed",
		"total_memories", report.MemoryDistribution.Total,
		"gaps", len(report.KnowledgeGaps),
		"health", report.OverallHealth,
	)

	return report, nil
}

func (ms *MetaService) detectGaps(ctx context.Context, projectID *uuid.UUID) []KnowledgeGap {
	var gaps []KnowledgeGap

	dist := MemoryDistribution{}
	dist.Episodic, _ = ms.episodic.Count(ctx, projectID)
	dist.Semantic, _ = ms.semantic.Count(ctx, projectID)
	dist.Procedural, _ = ms.procedural.Count(ctx, projectID)
	dist.Total = dist.Episodic + dist.Semantic + dist.Procedural

	if dist.Episodic > 20 && dist.Semantic == 0 {
		gaps = append(gaps, KnowledgeGap{
			Area:        "consolidation",
			Description: fmt.Sprintf("%d episodic memories but 0 semantic facts — consolidation may not be running", dist.Episodic),
			Severity:    0.8,
		})
	}

	if dist.Episodic > 0 && dist.Semantic > 0 {
		ratio := float64(dist.Semantic) / float64(dist.Episodic)
		if ratio < 0.05 {
			gaps = append(gaps, KnowledgeGap{
				Area:        "knowledge_extraction",
				Description: fmt.Sprintf("Low consolidation ratio (%.1f%%) — most episodic memories haven't been generalized", ratio*100),
				Severity:    0.6,
			})
		}
	}

	if dist.Total == 0 {
		gaps = append(gaps, KnowledgeGap{
			Area:        "empty_store",
			Description: "No memories stored yet. Use mos_remember, mos_study_project, or mos_learn_error to populate.",
			Severity:    1.0,
		})
	}

	for domain, cal := range ms.prediction.GetCalibration() {
		if cal.CalibrationOffset > 0.2 && cal.SampleCount >= 3 {
			gaps = append(gaps, KnowledgeGap{
				Area:        "calibration:" + domain,
				Description: fmt.Sprintf("Overconfident in domain '%s': avg confidence %.0f%% but actual accuracy %.0f%%", domain, cal.PredictedConfidence*100, cal.ActualAccuracy*100),
				Severity:    0.7,
			})
		}
	}

	evalReport := ms.eval.Evaluate()
	if evalReport.Precision < 0.5 && evalReport.TotalTrials > 5 {
		gaps = append(gaps, KnowledgeGap{
			Area:        "recall_quality",
			Description: fmt.Sprintf("Low recall precision (%.0f%%) — memories being returned are often not useful", evalReport.Precision*100),
			Severity:    0.9,
		})
	}

	sort.Slice(gaps, func(i, j int) bool {
		return gaps[i].Severity > gaps[j].Severity
	})

	return gaps
}

func (ms *MetaService) identifyStrengths(report *MetaReport) []string {
	var strengths []string

	if report.MemoryDistribution.Total > 100 {
		strengths = append(strengths, fmt.Sprintf("Rich knowledge base: %d total memories", report.MemoryDistribution.Total))
	}
	if report.MemoryDistribution.Semantic > 10 {
		strengths = append(strengths, fmt.Sprintf("Good generalization: %d semantic facts extracted", report.MemoryDistribution.Semantic))
	}

	for domain, cal := range report.DomainCalibration {
		if cal.CalibrationError < 0.1 && cal.Predictions >= 3 {
			strengths = append(strengths, fmt.Sprintf("Well-calibrated in domain '%s' (error %.1f%%)", domain, cal.CalibrationError*100))
		}
	}

	if len(report.KnowledgeGaps) == 0 {
		strengths = append(strengths, "No critical knowledge gaps detected")
	}

	return strengths
}

func (ms *MetaService) identifyWeaknesses(report *MetaReport) []string {
	var weaknesses []string

	if report.MemoryDistribution.Total < 10 {
		weaknesses = append(weaknesses, "Very few memories — system hasn't learned much yet")
	}
	if report.MemoryDistribution.Procedural == 0 {
		weaknesses = append(weaknesses, "No procedural memories — system doesn't know 'how to do' anything")
	}

	for domain, cal := range report.DomainCalibration {
		if cal.IsOverconfident {
			weaknesses = append(weaknesses, fmt.Sprintf("Overconfident in '%s' — predictions don't match reality", domain))
		}
	}

	for _, gap := range report.KnowledgeGaps {
		if gap.Severity > 0.7 {
			weaknesses = append(weaknesses, gap.Description)
		}
	}

	return weaknesses
}

func (ms *MetaService) generateRecommendations(report *MetaReport) []string {
	var recs []string

	if report.MemoryDistribution.Episodic > 20 && report.MemoryDistribution.Semantic < 3 {
		recs = append(recs, "Run mos_consolidate to convert episodic memories into semantic facts")
	}
	if report.MemoryDistribution.Total < 20 {
		recs = append(recs, "Run mos_study_project to ingest project documentation and code")
	}
	if report.MemoryDistribution.Procedural == 0 {
		recs = append(recs, "Store common workflows with numbered steps — they'll be auto-classified as procedures")
	}

	for _, cal := range report.DomainCalibration {
		if cal.IsOverconfident {
			recs = append(recs, fmt.Sprintf("Use mos_predict more carefully in '%s' — lower default confidence", cal.Domain))
		}
	}

	if len(recs) == 0 {
		recs = append(recs, "System is healthy. Continue using mos_remember for important changes and mos_learn_error for bugs.")
	}

	return recs
}

func (ms *MetaService) computeHealth(report *MetaReport) float64 {
	score := 0.5 // baseline

	if report.MemoryDistribution.Total > 0 {
		score += 0.1
	}
	if report.MemoryDistribution.Total > 50 {
		score += 0.1
	}
	if report.MemoryDistribution.Semantic > 0 {
		score += 0.1
	}
	if report.MemoryDistribution.Procedural > 0 {
		score += 0.05
	}

	for _, gap := range report.KnowledgeGaps {
		score -= gap.Severity * 0.1
	}

	for _, cal := range report.DomainCalibration {
		if !cal.IsOverconfident && cal.Predictions >= 3 {
			score += 0.05
		}
	}

	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}
