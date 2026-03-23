package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ABBenchmark struct {
	logger *slog.Logger
}

func NewABBenchmark(logger *slog.Logger) *ABBenchmark {
	return &ABBenchmark{logger: logger}
}

type ABScenario struct {
	ID           string
	Description  string
	Category     string // resource_leak, injection, concurrency, error_handling, architecture, protocol
	RuleContent  string // WHEN/WATCH/BECAUSE/DO/ANTIPATTERN format
	RuleFiles    []string
	RuleKeywords []string
	AntiPattern  string // regex matched against BuggyDiff added lines
	TaskFile     string
	TaskQuery    string
	BuggyDiff    string // unified diff added lines with the bug
	CorrectDiff  string // unified diff added lines without the bug
}

type ABCondition string

const (
	ConditionControl   ABCondition = "control"
	ConditionTreatment ABCondition = "treatment"
)

type ABScenarioResult struct {
	ScenarioID       string        `json:"scenario_id"`
	Category         string        `json:"category"`
	Condition        ABCondition   `json:"condition"`
	RuleMatched      bool          `json:"rule_matched"`
	MatchSignal      string        `json:"match_signal"`
	MatchConf        float64       `json:"match_confidence"`
	FalsePositives   int           `json:"false_positives"`
	AntiPatternFound bool          `json:"anti_pattern_found"`
	Prevented        bool          `json:"prevented"`
	MatchLatency     time.Duration `json:"match_latency"`
}

type ABReport struct {
	Timestamp             time.Time                    `json:"timestamp"`
	Duration              time.Duration                `json:"duration"`
	Scenarios             int                          `json:"scenarios"`
	TruePositives         int                          `json:"true_positives"`
	FalsePositives        int                          `json:"false_positives"`
	FalseNegatives        int                          `json:"false_negatives"`
	WarningPrecision      float64                      `json:"warning_precision"`
	WarningRecall         float64                      `json:"warning_recall"`
	PreventedWithWarning  int                          `json:"prevented_with_warning"`
	IgnoredWithWarning    int                          `json:"ignored_with_warning"`
	BuggyWithoutWarning   int                          `json:"buggy_without_warning"`
	CorrectWithoutWarning int                          `json:"correct_without_warning"`
	PreventionLift        float64                      `json:"prevention_lift"`
	MeanMatchLatency      time.Duration                `json:"mean_match_latency"`
	P95MatchLatency       time.Duration                `json:"p95_match_latency"`
	Details               []ABScenarioResult           `json:"details"`
	ByCategory            map[string]*ABCategoryResult `json:"by_category"`
	Formatted             string                       `json:"formatted_report"`
}

type ABCategoryResult struct {
	Category       string  `json:"category"`
	Total          int     `json:"total"`
	TruePositives  int     `json:"true_positives"`
	PreventedCount int     `json:"prevented_count"`
	PreventionRate float64 `json:"prevention_rate"`
}

func (ab *ABBenchmark) scenarios() []ABScenario {
	return []ABScenario{
		{
			ID:          "pgx-pool-leak",
			Description: "pgx connection pool: discard acquire error instead of releasing conn",
			Category:    "resource_leak",
			RuleContent: `WHEN: writing pgx pool acquire calls in Go
WATCH: pool.Acquire calls where the returned connection is discarded or error is ignored
BECAUSE: discarding the connection or its error causes the pool slot to leak; under load the pool exhausts and all queries hang
DO: always assign conn, err := pool.Acquire(ctx); check err; defer conn.Release()
ANTIPATTERN: _ = pool.Acquire`,
			RuleFiles:    []string{"internal/repo/user_repo.go"},
			RuleKeywords: []string{"pgx", "pool", "acquire", "connection", "leak"},
			AntiPattern:  `_\s*=\s*pool\.Acquire`,
			TaskFile:     "internal/repo/user_repo.go",
			TaskQuery:    "how do I acquire a pgx pool connection to run a query",
			BuggyDiff: `+func (r *UserRepo) GetUser(ctx context.Context, id uuid.UUID) (*domain.User, error) {
+	_ = pool.Acquire(ctx)
+	row := r.db.QueryRow(ctx, "SELECT id, name FROM users WHERE id=$1", id)
+	var u domain.User
+	return &u, row.Scan(&u.ID, &u.Name)
+}`,
			CorrectDiff: `+func (r *UserRepo) GetUser(ctx context.Context, id uuid.UUID) (*domain.User, error) {
+	conn, err := pool.Acquire(ctx)
+	if err != nil {
+		return nil, fmt.Errorf("acquire: %w", err)
+	}
+	defer conn.Release()
+	row := conn.QueryRow(ctx, "SELECT id, name FROM users WHERE id=$1", id)
+	var u domain.User
+	return &u, row.Scan(&u.ID, &u.Name)
+}`,
		},
		{
			ID:          "http-no-timeout",
			Description: "http.Client constructed without timeout hangs goroutine on slow server",
			Category:    "resource_leak",
			RuleContent: `WHEN: creating http.Client in Go
WATCH: &http.Client{} with no Timeout field set
BECAUSE: a client with no timeout will block the goroutine indefinitely on slow or hung remote servers, leaking the goroutine until the process restarts
DO: always set Timeout: 30*time.Second (or context-controlled); never use http.DefaultClient in services
ANTIPATTERN: &http\.Client\{\s*\}`,
			RuleFiles:    []string{"internal/adapter/rest/client.go"},
			RuleKeywords: []string{"http", "client", "timeout", "goroutine"},
			AntiPattern:  `&http\.Client\{\s*\}`,
			TaskFile:     "internal/adapter/rest/client.go",
			TaskQuery:    "create an http client to call external API",
			BuggyDiff: `+func newHTTPClient() *http.Client {
+	client := &http.Client{}
+	return client
+}`,
			CorrectDiff: `+func newHTTPClient() *http.Client {
+	client := &http.Client{Timeout: 30 * time.Second}
+	return client
+}`,
		},
		{
			ID:          "sql-injection",
			Description: "SQL query built with string concatenation instead of parameterized placeholder",
			Category:    "injection",
			RuleContent: `WHEN: building SQL queries that incorporate user-controlled input
WATCH: string concatenation or fmt.Sprintf used to construct SELECT/INSERT/UPDATE/DELETE queries
BECAUSE: unsanitized user input in SQL strings enables injection attacks that can read, modify, or destroy the entire database
DO: always use parameterized queries with $1, $2 placeholders; never concatenate user values into SQL
ANTIPATTERN: "SELECT.*"\s*\+`,
			RuleFiles:    []string{"internal/repo/user_repo.go"},
			RuleKeywords: []string{"sql", "query", "injection", "parameterized", "user", "input"},
			AntiPattern:  `"SELECT.*"\s*\+`,
			TaskFile:     "internal/repo/user_repo.go",
			TaskQuery:    "query users table by userID parameter passed from API",
			BuggyDiff: `+func (r *UserRepo) FindByID(ctx context.Context, userID string) (*domain.User, error) {
+	query := "SELECT id, name, email FROM users WHERE id = " + userID
+	row := r.db.QueryRow(ctx, query)
+	var u domain.User
+	return &u, row.Scan(&u.ID, &u.Name, &u.Email)
+}`,
			CorrectDiff: `+func (r *UserRepo) FindByID(ctx context.Context, userID string) (*domain.User, error) {
+	query := "SELECT id, name, email FROM users WHERE id = $1"
+	row := r.db.QueryRow(ctx, query, userID)
+	var u domain.User
+	return &u, row.Scan(&u.ID, &u.Name, &u.Email)
+}`,
		},
		{
			ID:          "goroutine-leak",
			Description: "goroutine launched without context cannot be cancelled on shutdown",
			Category:    "concurrency",
			RuleContent: `WHEN: launching goroutines for background work in Go services
WATCH: go func() { ... }() with no parameters — the goroutine receives no context
BECAUSE: a goroutine with no context has no cancellation path; it leaks when the parent shuts down and accumulates over time under load
DO: always pass ctx context.Context as an explicit parameter: go func(ctx context.Context) { ... }(ctx)
ANTIPATTERN: go func\(\)\s*\{`,
			RuleFiles:    []string{"internal/app/watcher.go"},
			RuleKeywords: []string{"goroutine", "context", "cancel", "leak", "background"},
			AntiPattern:  `go func\(\)\s*\{`,
			TaskFile:     "internal/app/watcher.go",
			TaskQuery:    "start background goroutine for file watcher",
			BuggyDiff: `+func (w *Watcher) Start() {
+	go func() {
+		doWork()
+	}()
+}`,
			CorrectDiff: `+func (w *Watcher) Start(ctx context.Context) {
+	go func(ctx context.Context) {
+		doWork(ctx)
+	}(ctx)
+}`,
		},
		{
			ID:          "nil-map-access",
			Description: "map value read without comma-ok idiom silently returns zero value",
			Category:    "error_handling",
			RuleContent: `WHEN: reading values from a map that may not contain the key
WATCH: single-value map access val := m[key] where the key may be absent
BECAUSE: the single-value form returns the zero value silently; logic downstream interprets a missing entry as a valid empty result, causing subtle data bugs
DO: use the comma-ok idiom: val, ok := m[key]; check ok before using val
ANTIPATTERN: ^\s*\w+\s*:=\s*\w+\[`,
			RuleFiles:    []string{"internal/app/recall_service.go"},
			RuleKeywords: []string{"map", "nil", "key", "lookup", "ok", "cache"},
			AntiPattern:  `^\s*\w+\s*:=\s*\w+\[`,
			TaskFile:     "internal/app/recall_service.go",
			TaskQuery:    "look up a cached embedding for a query string",
			BuggyDiff: `+func (s *RecallService) cachedEmb(key string) []float32 {
+	val := embCache[key]
+	return val
+}`,
			CorrectDiff: `+func (s *RecallService) cachedEmb(key string) ([]float32, bool) {
+	val, ok := embCache[key]
+	return val, ok
+}`,
		},
		{
			ID:          "slice-hardcoded-index",
			Description: "slice accessed with hardcoded numeric index without bounds check",
			Category:    "error_handling",
			RuleContent: `WHEN: accessing slice elements by index in Go
WATCH: hardcoded integer literals used as slice indices, e.g. data[0], data[3]
BECAUSE: hardcoded indices panic at runtime if the slice is shorter than expected; callers rarely guarantee length
DO: always check len(slice) before accessing by index; prefer range iteration or explicit length guards
ANTIPATTERN: \[\d+\]`,
			RuleFiles:    []string{"internal/app/hybrid_retriever.go"},
			RuleKeywords: []string{"slice", "index", "bounds", "panic", "length"},
			AntiPattern:  `\[\d+\]`,
			TaskFile:     "internal/app/hybrid_retriever.go",
			TaskQuery:    "get top result from retriever candidates slice",
			BuggyDiff: `+func topResult(candidates []domain.SemanticMemory) domain.SemanticMemory {
+	return candidates[0]
+}`,
			CorrectDiff: `+func topResult(candidates []domain.SemanticMemory) (domain.SemanticMemory, bool) {
+	if len(candidates) == 0 {
+		return domain.SemanticMemory{}, false
+	}
+	for _, c := range candidates {
+		return c, true
+	}
+	return domain.SemanticMemory{}, false
+}`,
		},
		{
			ID:          "race-raw-map",
			Description: "raw map used for concurrent cache causes data race",
			Category:    "concurrency",
			RuleContent: `WHEN: declaring a shared map that will be accessed from multiple goroutines
WATCH: var cache = map[string]... without a surrounding mutex or sync.Map
BECAUSE: concurrent map read+write without synchronization is a data race in Go; the runtime will panic or corrupt memory
DO: use sync.Map for concurrent access, or protect all map operations with sync.RWMutex
ANTIPATTERN: var cache\s*=\s*map\[`,
			RuleFiles:    []string{"internal/app/encode_service.go"},
			RuleKeywords: []string{"map", "concurrent", "race", "sync", "mutex", "cache"},
			AntiPattern:  `var cache\s*=\s*map\[`,
			TaskFile:     "internal/app/encode_service.go",
			TaskQuery:    "add an in-memory deduplication cache for encode service",
			BuggyDiff: `+var cache = map[string]bool{}
+
+func isDuplicate(key string) bool {
+	return cache[key]
+}`,
			CorrectDiff: `+var cache sync.Map
+
+func isDuplicate(key string) bool {
+	_, ok := cache.Load(key)
+	return ok
+}`,
		},
		{
			ID:          "error-swallow",
			Description: "error logged then dropped — caller cannot propagate failure",
			Category:    "error_handling",
			RuleContent: `WHEN: handling errors inside a function that has a return error signature
WATCH: log.Println(err) or log.Printf used to record an error without returning it
BECAUSE: logging and swallowing an error hides failures from the caller; the operation silently succeeds upstream while the real error is buried in logs
DO: return fmt.Errorf("context: %w", err) from lower layers; log only at the top handler layer
ANTIPATTERN: log\.Print.*err`,
			RuleFiles:    []string{"internal/app/encode_service.go"},
			RuleKeywords: []string{"error", "log", "swallow", "return", "propagate"},
			AntiPattern:  `log\.Print.*err`,
			TaskFile:     "internal/app/encode_service.go",
			TaskQuery:    "handle embedding provider error in encode service",
			BuggyDiff: `+func (s *EncodeService) embed(ctx context.Context, text string) ([]float32, error) {
+	emb, err := s.embedding.Embed(ctx, text)
+	if err != nil {
+		log.Println(err)
+		return nil, nil
+	}
+	return emb, nil
+}`,
			CorrectDiff: `+func (s *EncodeService) embed(ctx context.Context, text string) ([]float32, error) {
+	emb, err := s.embedding.Embed(ctx, text)
+	if err != nil {
+		return nil, fmt.Errorf("embed: %w", err)
+	}
+	return emb, nil
+}`,
		},
		{
			ID:          "file-no-close",
			Description: "os.Open error discarded and file handle never closed",
			Category:    "resource_leak",
			RuleContent: `WHEN: opening files with os.Open or os.Create in Go
WATCH: _ := os.Open(...) where the error is discarded and no defer f.Close() follows
BECAUSE: ignoring the error leaves the caller with a nil file; subsequent reads will panic; even on success, not closing leaks the file descriptor
DO: f, err := os.Open(path); if err != nil { return err }; defer f.Close()
ANTIPATTERN: _\s*:?=\s*os\.Open`,
			RuleFiles:    []string{"internal/app/ingest_service.go"},
			RuleKeywords: []string{"file", "open", "close", "defer", "descriptor", "leak"},
			AntiPattern:  `_\s*:?=\s*os\.Open`,
			TaskFile:     "internal/app/ingest_service.go",
			TaskQuery:    "open a source file for ingestion into memory",
			BuggyDiff: `+func readSource(path string) ([]byte, error) {
+	f, _ := os.Open(path)
+	return io.ReadAll(f)
+}`,
			CorrectDiff: `+func readSource(path string) ([]byte, error) {
+	f, err := os.Open(path)
+	if err != nil {
+		return nil, fmt.Errorf("open %s: %w", path, err)
+	}
+	defer f.Close()
+	return io.ReadAll(f)
+}`,
		},
		{
			ID:          "import-cycle",
			Description: "app package imports embedding adapter, creating an import cycle",
			Category:    "architecture",
			RuleContent: `WHEN: adding imports to any file inside internal/app
WATCH: import paths that reference internal/embedding or internal/adapter packages directly
BECAUSE: app is the orchestration layer; importing adapter or embedding packages inverts the dependency direction and creates import cycles that the Go compiler rejects
DO: define a domain interface (domain.EmbeddingProvider) and accept it via constructor injection; never import concrete adapter packages from app
ANTIPATTERN: ".*internal/embedding"`,
			RuleFiles:    []string{"internal/app/encode_service.go"},
			RuleKeywords: []string{"import", "cycle", "embedding", "adapter", "dependency", "interface"},
			AntiPattern:  `".*internal/embedding"`,
			TaskFile:     "internal/app/encode_service.go",
			TaskQuery:    "use the nomic embedding provider in encode service",
			BuggyDiff: `+import (
+	"context"
+	"github.com/hippocampus-mcp/hippocampus/internal/embedding"
+)
+
+func (s *EncodeService) embedText(ctx context.Context, text string) ([]float32, error) {
+	p := embedding.NewOllamaProvider("http://localhost:11434", "nomic-embed-text")
+	return p.Embed(ctx, text)
+}`,
			CorrectDiff: `+// s.embedding is domain.EmbeddingProvider — injected via constructor
+func (s *EncodeService) embedText(ctx context.Context, text string) ([]float32, error) {
+	return s.embedding.Embed(ctx, text)
+}`,
		},
		{
			ID:          "tsdb-agg-in-tx",
			Description: "TimescaleDB continuous aggregate created inside transaction — always fails",
			Category:    "architecture",
			RuleContent: `WHEN: writing migrations that create TimescaleDB continuous aggregates or materialized views
WATCH: tx.Exec or db.ExecContext calls that run CREATE MATERIALIZED VIEW inside a transaction block
BECAUSE: TimescaleDB continuous aggregates cannot be created inside a transaction; the migration runner wraps everything in BEGIN/COMMIT by default and the statement will always fail with an error
DO: use the @notx migration marker so the runner skips the transaction wrapper for this migration file
ANTIPATTERN: tx\.Exec.*CREATE MATERIALIZED VIEW`,
			RuleFiles:    []string{"migrations/007_add_hourly_agg.up.sql"},
			RuleKeywords: []string{"timescaledb", "continuous", "aggregate", "materialized", "view", "transaction", "migration"},
			AntiPattern:  `tx\.Exec.*CREATE MATERIALIZED VIEW`,
			TaskFile:     "migrations/007_add_hourly_agg.up.sql",
			TaskQuery:    "create a TimescaleDB continuous aggregate for hourly recall stats",
			BuggyDiff: `+func (m *Migrator) applyAgg(ctx context.Context) error {
+	tx, _ := m.db.Begin(ctx)
+	_, err := tx.Exec(ctx, "CREATE MATERIALIZED VIEW recall_hourly WITH (timescaledb.continuous) AS SELECT time_bucket('1h', created_at) FROM recalls")
+	return err
+}`,
			CorrectDiff: `+// @notx — continuous aggregates cannot run inside a transaction
+func (m *Migrator) applyAgg(ctx context.Context) error {
+	_, err := m.db.Exec(ctx, "CREATE MATERIALIZED VIEW recall_hourly WITH (timescaledb.continuous) AS SELECT time_bucket('1h', created_at) FROM recalls")
+	return err
+}`,
		},
		{
			ID:          "mcp-framing",
			Description: "MCP stdio transport uses Content-Length framing instead of newline-delimited JSON",
			Category:    "protocol",
			RuleContent: `WHEN: implementing MCP stdio transport in Go
WATCH: fmt.Fprintf writing Content-Length headers before JSON payloads
BECAUSE: MCP stdio transport uses newline-delimited JSON, NOT the LSP Content-Length framing protocol; clients will fail to parse the response and the tool call hangs
DO: write json.Marshal(msg) + newline (\n) directly to stdout; use bufio.Scanner for reading; never add Content-Length headers
ANTIPATTERN: Content-Length:`,
			RuleFiles:    []string{"internal/adapter/mcp/server.go"},
			RuleKeywords: []string{"mcp", "stdio", "transport", "framing", "content-length", "json", "newline"},
			AntiPattern:  `Content-Length:`,
			TaskFile:     "internal/adapter/mcp/server.go",
			TaskQuery:    "write MCP response message to stdout transport",
			BuggyDiff: `+func writeResponse(w io.Writer, msg any) error {
+	data, err := json.Marshal(msg)
+	if err != nil {
+		return err
+	}
+	fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(data))
+	_, err = w.Write(data)
+	return err
+}`,
			CorrectDiff: `+func writeResponse(w io.Writer, msg any) error {
+	data, err := json.Marshal(msg)
+	if err != nil {
+		return err
+	}
+	data = append(data, '\n')
+	_, err = w.Write(data)
+	return err
+}`,
		},
	}
}

func (ab *ABBenchmark) Run(ctx context.Context) (*ABReport, error) {
	start := time.Now()
	scenarios := ab.scenarios()
	var results []ABScenarioResult

	for i, sc := range scenarios {
		rule := CachedRule{
			ID:           uuid.New(),
			Content:      sc.RuleContent,
			FilePaths:    sc.RuleFiles,
			WhenKeywords: sc.RuleKeywords,
			AntiPattern:  sc.AntiPattern,
		}

		var distractors []CachedRule
		for j, other := range scenarios {
			if j != i && len(distractors) < 2 {
				distractors = append(distractors, CachedRule{
					ID:           uuid.New(),
					Content:      other.RuleContent,
					FilePaths:    other.RuleFiles,
					WhenKeywords: other.RuleKeywords,
					AntiPattern:  other.AntiPattern,
				})
			}
		}

		signals := MatchSignals{
			FilePath: sc.TaskFile,
			Query:    sc.TaskQuery,
		}

		// --- TREATMENT ARM ---
		wm := NewWarningMatcher(nil, nil, ab.logger)
		wm.mu.Lock()
		wm.cache = append([]CachedRule{rule}, distractors...)
		wm.mu.Unlock()

		matchStart := time.Now()
		matches := wm.Match(ctx, signals)
		matchLatency := time.Since(matchStart)

		ruleMatched := false
		matchSignal := ""
		matchConf := 0.0
		falsePos := 0
		for _, m := range matches {
			if m.Rule.ID == rule.ID {
				ruleMatched = true
				matchSignal = m.Signal
				matchConf = m.Confidence
			} else {
				falsePos++
			}
		}

		correctAdded := extractAddedLines(sc.CorrectDiff)
		apInCorrect, _ := matchAntiPattern(correctAdded, sc.AntiPattern)

		results = append(results, ABScenarioResult{
			ScenarioID:       sc.ID,
			Category:         sc.Category,
			Condition:        ConditionTreatment,
			RuleMatched:      ruleMatched,
			MatchSignal:      matchSignal,
			MatchConf:        matchConf,
			FalsePositives:   falsePos,
			AntiPatternFound: apInCorrect,
			Prevented:        ruleMatched && !apInCorrect,
			MatchLatency:     matchLatency,
		})

		// --- CONTROL ARM ---
		buggyAdded := extractAddedLines(sc.BuggyDiff)
		apInBuggy, _ := matchAntiPattern(buggyAdded, sc.AntiPattern)

		results = append(results, ABScenarioResult{
			ScenarioID:       sc.ID,
			Category:         sc.Category,
			Condition:        ConditionControl,
			RuleMatched:      false,
			AntiPatternFound: apInBuggy,
			Prevented:        false,
		})
	}

	report := ab.computeReport(results, start)
	report.Formatted = ab.formatReport(report)
	return report, nil
}

func (ab *ABBenchmark) computeReport(results []ABScenarioResult, start time.Time) *ABReport {
	report := &ABReport{
		Timestamp:  start,
		Duration:   time.Since(start),
		Scenarios:  0,
		ByCategory: map[string]*ABCategoryResult{},
	}

	scenarioSeen := map[string]bool{}
	var treatmentLatencies []time.Duration

	for _, r := range results {
		if r.Condition == ConditionTreatment {
			if !scenarioSeen[r.ScenarioID] {
				scenarioSeen[r.ScenarioID] = true
				report.Scenarios++
			}
			treatmentLatencies = append(treatmentLatencies, r.MatchLatency)

			// Warning quality: TP = matched the correct rule; FP = matched wrong rules; FN = should have matched but didn't
			if r.RuleMatched {
				report.TruePositives++
			} else {
				report.FalseNegatives++
			}
			report.FalsePositives += r.FalsePositives

			// Prevention effectiveness
			if r.Prevented {
				report.PreventedWithWarning++
			} else if r.RuleMatched && r.AntiPatternFound {
				report.IgnoredWithWarning++
			}

			// Per-category
			cat, ok := report.ByCategory[r.Category]
			if !ok {
				cat = &ABCategoryResult{Category: r.Category}
				report.ByCategory[r.Category] = cat
			}
			cat.Total++
			if r.RuleMatched {
				cat.TruePositives++
			}
			if r.Prevented {
				cat.PreventedCount++
			}
		} else {
			// Control arm
			if r.AntiPatternFound {
				report.BuggyWithoutWarning++
			} else {
				report.CorrectWithoutWarning++
			}
		}
	}

	// Precision and Recall
	if report.TruePositives+report.FalsePositives > 0 {
		report.WarningPrecision = float64(report.TruePositives) / float64(report.TruePositives+report.FalsePositives)
	}
	if report.TruePositives+report.FalseNegatives > 0 {
		report.WarningRecall = float64(report.TruePositives) / float64(report.TruePositives+report.FalseNegatives)
	}

	// Prevention lift: treatment prevention rate minus control "accidental correctness" rate
	treatmentTotal := report.PreventedWithWarning + report.IgnoredWithWarning
	controlTotal := report.BuggyWithoutWarning + report.CorrectWithoutWarning
	var treatmentRate, controlRate float64
	if treatmentTotal > 0 {
		treatmentRate = float64(report.PreventedWithWarning) / float64(treatmentTotal)
	}
	if controlTotal > 0 {
		controlRate = float64(report.CorrectWithoutWarning) / float64(controlTotal)
	}
	report.PreventionLift = treatmentRate - controlRate

	// Latency stats
	if len(treatmentLatencies) > 0 {
		var total time.Duration
		for _, l := range treatmentLatencies {
			total += l
		}
		report.MeanMatchLatency = total / time.Duration(len(treatmentLatencies))

		sorted := make([]time.Duration, len(treatmentLatencies))
		copy(sorted, treatmentLatencies)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		p95idx := int(float64(len(sorted)) * 0.95)
		if p95idx >= len(sorted) {
			p95idx = len(sorted) - 1
		}
		report.P95MatchLatency = sorted[p95idx]
	}

	// Finalize per-category rates
	for _, cat := range report.ByCategory {
		if cat.Total > 0 {
			cat.PreventionRate = float64(cat.PreventedCount) / float64(cat.Total)
		}
	}

	report.Details = results
	return report
}

func (ab *ABBenchmark) formatReport(r *ABReport) string {
	var b strings.Builder

	b.WriteString("# A/B Test: Warning Prevention Effectiveness\n\n")
	b.WriteString("## Summary\n")
	b.WriteString(fmt.Sprintf("%d scenarios × 2 conditions = %d test runs\n\n", r.Scenarios, len(r.Details)))

	b.WriteString("## Warning Quality\n")
	b.WriteString("| Metric    | Value |\n")
	b.WriteString("|-----------|-------|\n")
	b.WriteString(fmt.Sprintf("| Precision | %.0f%%  |\n", r.WarningPrecision*100))
	b.WriteString(fmt.Sprintf("| Recall    | %.0f%%  |\n", r.WarningRecall*100))
	b.WriteString(fmt.Sprintf("| TP        | %d     |\n", r.TruePositives))
	b.WriteString(fmt.Sprintf("| FP        | %d     |\n", r.FalsePositives))
	b.WriteString(fmt.Sprintf("| FN        | %d     |\n", r.FalseNegatives))
	b.WriteString("\n")

	treatmentTotal := r.PreventedWithWarning + r.IgnoredWithWarning
	controlTotal := r.BuggyWithoutWarning + r.CorrectWithoutWarning
	treatmentRate := 0.0
	controlRate := 0.0
	if treatmentTotal > 0 {
		treatmentRate = float64(r.PreventedWithWarning) / float64(treatmentTotal) * 100
	}
	if controlTotal > 0 {
		controlRate = float64(r.CorrectWithoutWarning) / float64(controlTotal) * 100
	}

	b.WriteString("## Prevention Effectiveness\n")
	b.WriteString("| Condition               | Bugs | Clean | Rate  |\n")
	b.WriteString("|------------------------|------|-------|-------|\n")
	b.WriteString(fmt.Sprintf("| Treatment (warnings ON) | %-4d | %-5d | %.0f%%  |\n", r.IgnoredWithWarning, r.PreventedWithWarning, treatmentRate))
	b.WriteString(fmt.Sprintf("| Control (warnings OFF)  | %-4d | %-5d | %.0f%%  |\n", r.BuggyWithoutWarning, r.CorrectWithoutWarning, controlRate))
	b.WriteString(fmt.Sprintf("| **Lift**               |      |       | %+.0f%% |\n", r.PreventionLift*100))
	b.WriteString("\n")

	b.WriteString("## Latency\n")
	b.WriteString(fmt.Sprintf("Mean: %v | P95: %v\n\n", r.MeanMatchLatency, r.P95MatchLatency))

	// Sort categories for stable output
	cats := make([]string, 0, len(r.ByCategory))
	for c := range r.ByCategory {
		cats = append(cats, c)
	}
	sort.Strings(cats)

	b.WriteString("## By Category\n")
	b.WriteString("| Category        | Scenarios | TP | Prevented | Rate  |\n")
	b.WriteString("|----------------|-----------|-----|-----------|-------|\n")
	for _, c := range cats {
		cat := r.ByCategory[c]
		b.WriteString(fmt.Sprintf("| %-15s | %-9d | %-3d | %-9d | %.0f%%  |\n",
			cat.Category, cat.Total, cat.TruePositives, cat.PreventedCount, cat.PreventionRate*100))
	}
	b.WriteString("\n")

	b.WriteString("## Scenario Details\n")
	b.WriteString("| ID | Cond | Match | Signal | Conf | FP | AP | Prevented |\n")
	b.WriteString("|----|------|-------|--------|------|----|----|----------|\n")
	for _, d := range r.Details {
		if d.Condition == ConditionTreatment {
			matched := "N"
			if d.RuleMatched {
				matched = "Y"
			}
			ap := "N"
			if d.AntiPatternFound {
				ap = "Y"
			}
			prev := "N"
			if d.Prevented {
				prev = "Y"
			}
			b.WriteString(fmt.Sprintf("| %-28s | treat | %-5s | %-14s | %.2f | %-2d | %-2s | %-9s |\n",
				d.ScenarioID, matched, d.MatchSignal, d.MatchConf, d.FalsePositives, ap, prev))
		}
	}

	return b.String()
}
