package app

import (
	"strings"

	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// EmotionDetector automatically extracts emotional signals from content.
// Based on appraisal theory (Scherer, 2001): emotions arise from cognitive
// evaluation of events, not raw sensation. We detect:
//   - danger:      rollback, crash, data loss, production incident
//   - frustration: repeated failures, corrections, "still not working"
//   - surprise:    unexpected behavior, contradictions
//   - success:     fixed, works, resolved, completed
//   - novelty:     new approach, first time, never seen before
type EmotionDetector struct{}

type DetectedEmotion struct {
	Valence   domain.Valence
	Intensity float64
	Signals   domain.Metadata
}

var dangerSignals = []string{
	"rollback", "revert", "data loss", "production incident", "outage",
	"crash", "panic", "fatal", "corruption", "security breach",
	"vulnerability", "exploit", "downtime", "breaking change",
	"cannot recover", "unrecoverable", "emergency",
}

var frustrationSignals = []string{
	"still not working", "again", "still broken", "tried everything",
	"doesn't work", "not working", "keeps failing", "same error",
	"won't compile", "stuck", "no progress", "third attempt",
	"wasted time", "regression", "broke again", "still fails",
}

var surpriseSignals = []string{
	"unexpected", "surprisingly", "turns out", "actually was",
	"didn't expect", "counter-intuitive", "contradicts", "opposite",
	"wrong assumption", "misconception", "plot twist",
	"not what I thought", "was actually", "hidden behavior",
}

var successSignals = []string{
	"fixed", "resolved", "works now", "working correctly",
	"successfully", "completed", "passed all tests", "green build",
	"deployed", "merged", "approved", "all tests pass",
	"performance improved", "bug fixed", "issue closed",
}

var noveltySignals = []string{
	"new approach", "first time", "never seen", "novel",
	"innovative", "breakthrough", "discovered", "prototype",
	"proof of concept", "experiment", "exploration",
	"alternative solution", "creative", "unconventional",
}

// Detect analyzes content and returns all detected emotional signals.
// Multiple emotions can co-occur (e.g., surprise + danger).
func (d *EmotionDetector) Detect(content string) []DetectedEmotion {
	lower := strings.ToLower(content)
	var result []DetectedEmotion

	if e := detectCategory(lower, dangerSignals, domain.ValDanger, 0.8); e != nil {
		result = append(result, *e)
	}
	if e := detectCategory(lower, frustrationSignals, domain.ValFrustration, 0.6); e != nil {
		result = append(result, *e)
	}
	if e := detectCategory(lower, surpriseSignals, domain.ValSurprise, 0.5); e != nil {
		result = append(result, *e)
	}
	if e := detectCategory(lower, successSignals, domain.ValSuccess, 0.4); e != nil {
		result = append(result, *e)
	}
	if e := detectCategory(lower, noveltySignals, domain.ValNovelty, 0.3); e != nil {
		result = append(result, *e)
	}

	if strings.Contains(lower, "error") || strings.Contains(lower, "err:") || strings.Contains(lower, "fail") {
		hasError := false
		for _, e := range result {
			if e.Valence == domain.ValDanger || e.Valence == domain.ValFrustration {
				hasError = true
				break
			}
		}
		if !hasError {
			result = append(result, DetectedEmotion{
				Valence:   domain.ValFrustration,
				Intensity: 0.4,
				Signals:   domain.Metadata{"pattern": "error_keyword"},
			})
		}
	}

	return result
}

func detectCategory(lower string, signals []string, valence domain.Valence, baseIntensity float64) *DetectedEmotion {
	var matched []string
	for _, sig := range signals {
		if strings.Contains(lower, sig) {
			matched = append(matched, sig)
		}
	}
	if len(matched) == 0 {
		return nil
	}

	intensity := baseIntensity + float64(len(matched)-1)*0.1
	if intensity > 1.0 {
		intensity = 1.0
	}

	return &DetectedEmotion{
		Valence:   valence,
		Intensity: intensity,
		Signals:   domain.Metadata{"matched": matched, "count": len(matched)},
	}
}
