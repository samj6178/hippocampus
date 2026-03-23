package app

import (
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"
)

// EvalFramework measures how well Hippocampus actually improves agent performance.
//
// Key insight: nobody in the AI memory space has formal evaluation.
// Mem0, Zep, MemoryBank — all claim to "help" but none PROVE it.
//
// We measure 4 things:
//   1. Recall Precision: do recalled memories actually help? (feedback-based)
//   2. Error Prevention Rate: how many errors were prevented by warnings?
//   3. Prediction Calibration: Brier score across domains
//   4. Knowledge Convergence: is the system converging to useful knowledge?
//
// Information-theoretic foundation:
//   Quality(S) = H(Task) - H(Task|S)  // mutual information between context S and task
//   We approximate this via recall relevance scores and feedback signals.
type EvalFramework struct {
	logger *slog.Logger

	mu      sync.Mutex
	trials  []EvalTrial
	windows []WindowMetrics
}

// EvalTrial records a single recall + outcome observation.
type EvalTrial struct {
	Timestamp   time.Time `json:"timestamp"`
	Query       string    `json:"query"`
	Domain      string    `json:"domain"`
	Candidates  int       `json:"candidates"`
	Confidence  float64   `json:"confidence"`
	Useful      *bool     `json:"useful,omitempty"`      // nil = no feedback yet
	Latency     time.Duration `json:"latency"`
	TokensUsed  int       `json:"tokens_used"`
}

// WindowMetrics aggregates metrics over a time window.
type WindowMetrics struct {
	WindowStart    time.Time `json:"window_start"`
	WindowEnd      time.Time `json:"window_end"`
	TotalRecalls   int       `json:"total_recalls"`
	UsefulRecalls  int       `json:"useful_recalls"`
	UselessRecalls int       `json:"useless_recalls"`
	NoFeedback     int       `json:"no_feedback"`
	AvgConfidence  float64   `json:"avg_confidence"`
	AvgLatency     float64   `json:"avg_latency_ms"`
	BrierScore     float64   `json:"brier_score"` // lower = better calibration
	Precision      float64   `json:"precision"`    // useful / (useful + useless)
	ErrorsPrevented int      `json:"errors_prevented"`
}

func NewEvalFramework(logger *slog.Logger) *EvalFramework {
	return &EvalFramework{
		logger: logger,
	}
}

// RecordRecall logs a recall event for evaluation.
func (ef *EvalFramework) RecordRecall(query string, domain string, candidates int, confidence float64, latency time.Duration, tokens int) {
	ef.mu.Lock()
	defer ef.mu.Unlock()

	ef.trials = append(ef.trials, EvalTrial{
		Timestamp:  time.Now(),
		Query:      query,
		Domain:     domain,
		Candidates: candidates,
		Confidence: confidence,
		Latency:    latency,
		TokensUsed: tokens,
	})
}

// RecordFeedback marks the most recent recall matching this query as useful/not.
func (ef *EvalFramework) RecordFeedback(useful bool) {
	ef.mu.Lock()
	defer ef.mu.Unlock()

	for i := len(ef.trials) - 1; i >= 0; i-- {
		if ef.trials[i].Useful == nil {
			ef.trials[i].Useful = &useful
			return
		}
	}
}

// RecordErrorPrevented records an instance where memory prevented a repeated error.
func (ef *EvalFramework) RecordErrorPrevented() {
	ef.mu.Lock()
	defer ef.mu.Unlock()

	if len(ef.windows) == 0 {
		ef.windows = append(ef.windows, WindowMetrics{
			WindowStart: time.Now(),
		})
	}
	ef.windows[len(ef.windows)-1].ErrorsPrevented++
}

// Evaluate computes comprehensive metrics from accumulated trials.
func (ef *EvalFramework) Evaluate() *EvalReport {
	ef.mu.Lock()
	defer ef.mu.Unlock()

	report := &EvalReport{
		Timestamp:   time.Now(),
		TotalTrials: len(ef.trials),
	}

	if len(ef.trials) == 0 {
		report.Summary = "No evaluation data yet. Use mos_recall and provide feedback via mos_feedback to start measuring."
		return report
	}

	var (
		useful, useless, noFeedback int
		totalConf, totalLatency     float64
		brierSum                    float64
		brierCount                  int
	)

	for _, t := range ef.trials {
		totalConf += t.Confidence
		totalLatency += float64(t.Latency.Milliseconds())

		if t.Useful == nil {
			noFeedback++
		} else if *t.Useful {
			useful++
			brierSum += math.Pow(t.Confidence-1.0, 2)
			brierCount++
		} else {
			useless++
			brierSum += math.Pow(t.Confidence-0.0, 2)
			brierCount++
		}
	}

	report.UsefulRecalls = useful
	report.UselessRecalls = useless
	report.NoFeedback = noFeedback
	report.AvgConfidence = totalConf / float64(len(ef.trials))
	report.AvgLatencyMs = totalLatency / float64(len(ef.trials))

	if useful+useless > 0 {
		report.Precision = float64(useful) / float64(useful+useless)
	}

	if brierCount > 0 {
		report.BrierScore = brierSum / float64(brierCount)
	}

	report.LearningCurve = ef.computeLearningCurve()

	report.DomainBreakdown = ef.computeDomainBreakdown()

	report.Summary = ef.generateSummary(report)

	return report
}

func (ef *EvalFramework) computeLearningCurve() []LearningPoint {
	if len(ef.trials) < 4 {
		return nil
	}

	windowSize := len(ef.trials) / 4
	if windowSize < 2 {
		windowSize = 2
	}

	var points []LearningPoint

	for i := 0; i+windowSize <= len(ef.trials); i += windowSize {
		window := ef.trials[i : i+windowSize]

		var useful, total int
		var avgConf float64
		for _, t := range window {
			if t.Useful != nil {
				total++
				if *t.Useful {
					useful++
				}
			}
			avgConf += t.Confidence
		}
		avgConf /= float64(len(window))

		precision := 0.0
		if total > 0 {
			precision = float64(useful) / float64(total)
		}

		points = append(points, LearningPoint{
			WindowIndex: len(points),
			Timestamp:   window[0].Timestamp,
			Precision:   precision,
			AvgConf:     avgConf,
			Samples:     len(window),
		})
	}

	return points
}

func (ef *EvalFramework) computeDomainBreakdown() map[string]*DomainEval {
	domains := make(map[string]*DomainEval)

	for _, t := range ef.trials {
		d := t.Domain
		if d == "" {
			d = "general"
		}
		de, ok := domains[d]
		if !ok {
			de = &DomainEval{Domain: d}
			domains[d] = de
		}
		de.TotalRecalls++
		if t.Useful != nil {
			if *t.Useful {
				de.UsefulRecalls++
			} else {
				de.UselessRecalls++
			}
		}
	}

	for _, de := range domains {
		total := de.UsefulRecalls + de.UselessRecalls
		if total > 0 {
			de.Precision = float64(de.UsefulRecalls) / float64(total)
		}
	}

	return domains
}

func (ef *EvalFramework) generateSummary(r *EvalReport) string {
	if r.TotalTrials < 5 {
		return fmt.Sprintf("Early stage: %d trials. Need 5+ with feedback for meaningful evaluation.", r.TotalTrials)
	}

	feedbackRate := 1.0 - float64(r.NoFeedback)/float64(r.TotalTrials)
	if feedbackRate < 0.3 {
		return fmt.Sprintf("Low feedback rate (%.0f%%). Use mos_feedback after recalls to improve evaluation quality. %d trials so far.", feedbackRate*100, r.TotalTrials)
	}

	quality := "poor"
	switch {
	case r.Precision >= 0.8:
		quality = "excellent"
	case r.Precision >= 0.6:
		quality = "good"
	case r.Precision >= 0.4:
		quality = "moderate"
	}

	calibration := "well-calibrated"
	if r.BrierScore > 0.25 {
		calibration = "poorly calibrated (overconfident)"
	} else if r.BrierScore > 0.15 {
		calibration = "moderately calibrated"
	}

	improving := ""
	if len(r.LearningCurve) >= 2 {
		first := r.LearningCurve[0].Precision
		last := r.LearningCurve[len(r.LearningCurve)-1].Precision
		delta := last - first
		if delta > 0.1 {
			improving = fmt.Sprintf(" Recall quality improving: +%.0f%% since first window.", delta*100)
		} else if delta < -0.1 {
			improving = fmt.Sprintf(" WARNING: Recall quality degrading: %.0f%% since first window.", delta*100)
		} else {
			improving = " Recall quality stable."
		}
	}

	return fmt.Sprintf("Recall quality: %s (%.0f%% precision). Calibration: %s (Brier=%.3f). %d trials, %.0f%% feedback rate.%s",
		quality, r.Precision*100, calibration, r.BrierScore, r.TotalTrials, feedbackRate*100, improving)
}

// --- Report types ---

type EvalReport struct {
	Timestamp       time.Time                `json:"timestamp"`
	TotalTrials     int                      `json:"total_trials"`
	UsefulRecalls   int                      `json:"useful_recalls"`
	UselessRecalls  int                      `json:"useless_recalls"`
	NoFeedback      int                      `json:"no_feedback"`
	Precision       float64                  `json:"precision"`
	BrierScore      float64                  `json:"brier_score"`
	AvgConfidence   float64                  `json:"avg_confidence"`
	AvgLatencyMs    float64                  `json:"avg_latency_ms"`
	LearningCurve   []LearningPoint          `json:"learning_curve,omitempty"`
	DomainBreakdown map[string]*DomainEval   `json:"domain_breakdown,omitempty"`
	Summary         string                   `json:"summary"`
}

type LearningPoint struct {
	WindowIndex int       `json:"window_index"`
	Timestamp   time.Time `json:"timestamp"`
	Precision   float64   `json:"precision"`
	AvgConf     float64   `json:"avg_confidence"`
	Samples     int       `json:"samples"`
}

type DomainEval struct {
	Domain        string  `json:"domain"`
	TotalRecalls  int     `json:"total_recalls"`
	UsefulRecalls int     `json:"useful_recalls"`
	UselessRecalls int    `json:"useless_recalls"`
	Precision     float64 `json:"precision"`
}

