package app

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const maxSummaryLen = 200

// extractSummary produces a concise summary from verbose content using
// extractive summarization: score each sentence, pick the top ones up to
// maxSummaryLen characters, preserving original sentence order.
func extractSummary(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= maxSummaryLen {
		return content
	}

	sentences := splitSentences(content)
	if len(sentences) == 0 {
		return truncateAt(content, maxSummaryLen)
	}
	if len(sentences) == 1 {
		return truncateAt(sentences[0], maxSummaryLen)
	}

	type scored struct {
		text  string
		pos   int
		score float64
	}
	items := make([]scored, len(sentences))
	for i, s := range sentences {
		items[i] = scored{text: s, pos: i, score: scoreSentence(s, i, len(sentences))}
	}

	sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })

	var selected []scored
	total := 0
	for _, s := range items {
		sLen := len(s.text)
		if total+sLen > maxSummaryLen {
			if len(selected) == 0 {
				selected = append(selected, s)
			}
			break
		}
		selected = append(selected, s)
		total += sLen + 1
	}

	sort.Slice(selected, func(i, j int) bool { return selected[i].pos < selected[j].pos })

	parts := make([]string, len(selected))
	for i, s := range selected {
		parts[i] = s.text
	}
	result := strings.Join(parts, " ")
	if len(result) > maxSummaryLen {
		result = truncateAt(result, maxSummaryLen)
	}
	return result
}

func scoreSentence(s string, position, total int) float64 {
	score := 0.0

	if position == 0 {
		score += 3.0
	} else if position == 1 {
		score += 1.0
	} else if position == total-1 {
		score += 0.5
	}

	words := strings.Fields(s)
	n := len(words)
	if n >= 5 && n <= 25 {
		score += 1.0
	} else if n < 3 {
		score -= 1.0
	}

	lower := strings.ToLower(s)
	for _, kw := range sentenceKeywords {
		if strings.Contains(lower, kw) {
			score += 0.5
		}
	}

	if fileRefPattern.MatchString(s) {
		score += 1.0
	}
	if strings.Contains(s, "()") || strings.Contains(s, "func ") {
		score += 0.5
	}

	return score
}

var sentenceKeywords = []string{
	"error", "fix", "bug", "cause", "solution", "decision",
	"because", "changed", "added", "removed", "updated",
	"important", "critical", "pattern", "discovered",
}

var fileRefPattern = regexp.MustCompile(`\w+\.(go|ts|py|rs|js|tsx|jsx|sql|yaml|yml|toml|json)`)

var sentenceSplitPattern = regexp.MustCompile(`(?m)[.!?]\s+|\n\s*\n|\n(?:[A-Z0-9]|\d+\.)`)

func splitSentences(text string) []string {
	locs := sentenceSplitPattern.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return []string{strings.TrimSpace(text)}
	}

	var result []string
	prev := 0
	for _, loc := range locs {
		end := loc[0] + 1
		s := strings.TrimSpace(text[prev:end])
		if len(s) > 0 {
			result = append(result, s)
		}
		prev = loc[0] + 1
		for prev < len(text) && (text[prev] == ' ' || text[prev] == '\n') {
			prev++
		}
	}
	if prev < len(text) {
		s := strings.TrimSpace(text[prev:])
		if len(s) > 0 {
			result = append(result, s)
		}
	}
	return result
}

func truncateAt(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen - 3
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !unicode.IsSpace(rune(s[cut])) {
		cut--
	}
	if cut == 0 {
		cut = maxLen - 3
	}
	return strings.TrimSpace(s[:cut]) + "..."
}

// contentQualityScore estimates information density of content.
// Returns 0.0-1.0 where higher means more information-dense.
func contentQualityScore(content string) float64 {
	content = strings.TrimSpace(content)
	if len(content) < 10 {
		return 0.0
	}

	words := strings.Fields(content)
	if len(words) < 5 {
		return 0.05
	}

	unique := make(map[string]struct{})
	for _, w := range words {
		unique[strings.ToLower(w)] = struct{}{}
	}

	nonStopUnique := 0
	for w := range unique {
		if !isStopWord(w) {
			nonStopUnique++
		}
	}

	if nonStopUnique < 2 {
		return 0.05
	}
	if nonStopUnique < 3 {
		return 0.1
	}

	score := float64(nonStopUnique) / float64(len(words))
	if score > 1.0 {
		score = 1.0
	}
	return score
}

var stopWords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "being": {}, "have": {}, "has": {}, "had": {},
	"do": {}, "does": {}, "did": {}, "will": {}, "would": {}, "shall": {},
	"should": {}, "may": {}, "might": {}, "can": {}, "could": {},
	"i": {}, "you": {}, "he": {}, "she": {}, "it": {}, "we": {}, "they": {},
	"me": {}, "him": {}, "her": {}, "us": {}, "them": {},
	"my": {}, "your": {}, "his": {}, "its": {}, "our": {}, "their": {},
	"this": {}, "that": {}, "these": {}, "those": {},
	"in": {}, "on": {}, "at": {}, "to": {}, "for": {}, "of": {}, "with": {},
	"by": {}, "from": {}, "up": {}, "about": {}, "into": {}, "over": {},
	"and": {}, "but": {}, "or": {}, "not": {}, "no": {}, "so": {}, "if": {},
	"as": {}, "just": {}, "also": {}, "very": {}, "too": {}, "than": {},
}

func isStopWord(w string) bool {
	_, ok := stopWords[w]
	return ok
}

// extractAutoTags scans content for technology references, file paths,
// error patterns, and decision language to generate tags automatically.
func extractAutoTags(content string) []string {
	lower := strings.ToLower(content)
	seen := make(map[string]struct{})
	var tags []string

	add := func(tag string) {
		if _, ok := seen[tag]; !ok {
			seen[tag] = struct{}{}
			tags = append(tags, tag)
		}
	}

	for _, lang := range techLangs {
		if strings.Contains(lower, lang.keyword) {
			add(lang.tag)
		}
	}

	if fileRefPattern.MatchString(content) {
		matches := fileRefPattern.FindAllStringSubmatch(content, 5)
		for _, m := range matches {
			if len(m) > 1 {
				ext := m[1]
				if tag, ok := extToTag[ext]; ok {
					add(tag)
				}
			}
		}
	}

	for _, cat := range contentCategories {
		for _, kw := range cat.keywords {
			if strings.Contains(lower, kw) {
				add(cat.tag)
				break
			}
		}
	}

	return tags
}

// mergeTagSets merges two tag slices, deduplicating by value.
func mergeTagSets(existing, auto []string) []string {
	seen := make(map[string]struct{}, len(existing))
	result := make([]string, 0, len(existing)+len(auto))
	for _, t := range existing {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			result = append(result, t)
		}
	}
	for _, t := range auto {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			result = append(result, t)
		}
	}
	return result
}

type techLang struct {
	keyword string
	tag     string
}

var techLangs = []techLang{
	{"golang", "go"}, {"go module", "go"}, {"goroutine", "go"},
	{"typescript", "typescript"}, {"react", "react"}, {"tailwind", "tailwind"},
	{"python", "python"}, {"django", "python"}, {"fastapi", "python"},
	{"rust", "rust"}, {"cargo", "rust"},
	{"docker", "docker"}, {"kubernetes", "kubernetes"}, {"k8s", "kubernetes"},
	{"postgres", "postgresql"}, {"pgx", "postgresql"}, {"timescale", "timescaledb"},
	{"redis", "redis"}, {"nginx", "nginx"},
}

var extToTag = map[string]string{
	"go": "go", "ts": "typescript", "tsx": "typescript",
	"py": "python", "rs": "rust", "js": "javascript",
	"sql": "sql", "yaml": "config", "yml": "config",
	"toml": "config", "json": "config",
}

type contentCategory struct {
	tag      string
	keywords []string
}

var contentCategories = []contentCategory{
	{"error", []string{"error", "panic", "crash", "fatal", "exception", "stack trace", "failed"}},
	{"bugfix", []string{"fixed", "fix:", "root cause", "bug fix", "resolved"}},
	{"decision", []string{"decided", "decision", "chose", "chosen", "trade-off", "approach"}},
	{"architecture", []string{"architecture", "design", "refactor", "restructure", "pattern"}},
	{"performance", []string{"latency", "throughput", "bottleneck", "optimization", "benchmark"}},
	{"security", []string{"vulnerability", "security", "auth", "credential", "permission"}},
	{"config", []string{"configuration", "env var", "environment", "setting", ".env"}},
}
