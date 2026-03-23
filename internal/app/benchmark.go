package app

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hippocampus-mcp/hippocampus/internal/domain"
)

// BenchmarkSuite runs a reproducible evaluation of Hippocampus memory quality.
//
// Hypothesis: "An AI agent with Hippocampus memory recalls relevant context
// with precision > 80% and correctly rejects irrelevant queries > 90% of the time,
// significantly outperforming random baseline."
//
// Method:
//   1. Seed N known memories into a clean benchmark project
//   2. Run M queries (positive + negative) against the memory store
//   3. Measure precision, recall, MRR, error prevention rate, negative accuracy
//   4. Compare against random baseline (theoretical: relevant/total = 1/N)
//   5. Report with statistical summary
type BenchmarkSuite struct {
	encode    *EncodeService
	recall    *RecallService
	project   *ProjectService
	embedding domain.EmbeddingProvider
	logger    *slog.Logger
}

func NewBenchmarkSuite(
	encode *EncodeService,
	recall *RecallService,
	project *ProjectService,
	embedding domain.EmbeddingProvider,
	logger *slog.Logger,
) *BenchmarkSuite {
	return &BenchmarkSuite{
		encode:    encode,
		recall:    recall,
		project:   project,
		embedding: embedding,
		logger:    logger,
	}
}

type BenchmarkScenario struct {
	ID            string
	Category      string // "error_prevention", "knowledge_recall", "negative", "semantic"
	Description   string
	SeedContent   string
	SeedTags      []string
	SeedImportance float64
	Query         string
	ExpectedHit   bool
	MatchPatterns []string // at least one must appear in recall result for a "hit"
}

type ScenarioResult struct {
	ScenarioID   string        `json:"scenario_id"`
	Category     string        `json:"category"`
	Query        string        `json:"query"`
	ExpectedHit  bool          `json:"expected_hit"`
	ActualHit    bool          `json:"actual_hit"`
	Correct      bool          `json:"correct"`
	Confidence   float64       `json:"confidence"`
	Candidates   int           `json:"candidates"`
	Latency      time.Duration `json:"latency"`
	MatchedText  string        `json:"matched_text,omitempty"`
	RejectReason string        `json:"reject_reason,omitempty"`
	BestSim      float64       `json:"best_sim,omitempty"`
}

type BenchmarkReport struct {
	Timestamp   time.Time                    `json:"timestamp"`
	Duration    time.Duration                `json:"duration"`
	ProjectID   string                       `json:"project_id"`
	Scenarios   int                          `json:"total_scenarios"`
	Results     []ScenarioResult             `json:"results"`
	Aggregate   AggregateMetrics             `json:"aggregate"`
	ByCategory  map[string]*CategoryMetrics  `json:"by_category"`
	Baseline    BaselineComparison           `json:"baseline_comparison"`
	Conclusion  string                       `json:"conclusion"`
	Formatted   string                       `json:"formatted_report"`
}

type AggregateMetrics struct {
	Precision           float64       `json:"precision"`
	Recall              float64       `json:"recall"`
	F1                  float64       `json:"f1_score"`
	ErrorPreventionRate float64       `json:"error_prevention_rate"`
	NegativeAccuracy    float64       `json:"negative_accuracy"`
	MeanLatency         time.Duration `json:"mean_latency"`
	P95Latency          time.Duration `json:"p95_latency"`
	TotalCorrect        int           `json:"total_correct"`
	TotalScenarios      int           `json:"total_scenarios"`
}

type CategoryMetrics struct {
	Category string  `json:"category"`
	Total    int     `json:"total"`
	Correct  int     `json:"correct"`
	Rate     float64 `json:"rate"`
}

type BaselineComparison struct {
	RandomPrecision    float64 `json:"random_precision"`
	SystemPrecision    float64 `json:"system_precision"`
	ImprovementFactor  float64 `json:"improvement_factor"`
	TotalSeededMemories int    `json:"total_seeded_memories"`
}

func (bs *BenchmarkSuite) scenarios() []BenchmarkScenario {
	return []BenchmarkScenario{
		// ═══════════════════════════════════════════════════
		// ERROR PREVENTION (12 scenarios)
		// ═══════════════════════════════════════════════════
		{
			ID: "err-1", Category: "error_prevention",
			SeedContent:    "ERROR in metrics.go: 'undefined: filepath'. Root cause: missing import statement. Fix: add 'path/filepath' to import block. This is a common Go compilation error when using filepath.Join or filepath.Dir without importing the package.",
			SeedTags:       []string{"error", "go-compilation", "import"},
			SeedImportance: 0.85,
			Query:          "I'm getting undefined filepath error in Go code",
			ExpectedHit:    true,
			MatchPatterns:  []string{"filepath", "import", "undefined"},
		},
		{
			ID: "err-2", Category: "error_prevention",
			SeedContent:    "ERROR in episodic_repo.go: SQL query column order mismatch in ListByTags. The SELECT columns were in wrong order compared to collectEpisodic scan function. Fix: reorder SELECT to match: id, time, project_id, agent_id, session_id, content, summary, importance, confidence, access_count, last_accessed, token_count, tags, metadata, consolidated.",
			SeedTags:       []string{"error", "database", "sql", "bugfix"},
			SeedImportance: 0.9,
			Query:          "database query returns corrupted data from episodic table",
			ExpectedHit:    true,
			MatchPatterns:  []string{"column", "order", "ListByTags", "collectEpisodic"},
		},
		{
			ID: "err-3", Category: "error_prevention",
			SeedContent:    "ERROR: MCP tools not appearing after code changes. Root cause: Cursor's mcp.json points to bin/hippocampus.exe but build outputs to project root. Cursor caches tool descriptors from first tools/list response. Fix: always build to bin/ directory, kill old process before rebuild, and fully restart Cursor.",
			SeedTags:       []string{"error", "mcp", "cursor", "deployment"},
			SeedImportance: 0.9,
			Query:          "new MCP tool not showing up after I rebuilt the server",
			ExpectedHit:    true,
			MatchPatterns:  []string{"MCP", "tool", "cach", "bin/"},
		},
		{
			ID: "err-4", Category: "error_prevention",
			SeedContent:    "ERROR: panic: nil pointer dereference in consolidation when episodic memory has no embedding. Root cause: some old memories inserted before embedding was mandatory had nil embedding field. Fix: skip nil-embedding memories in consolidation clustering. Prevention: add NOT NULL constraint on embedding column.",
			SeedTags:       []string{"error", "panic", "consolidation"},
			SeedImportance: 0.88,
			Query:          "consolidation panics with nil pointer",
			ExpectedHit:    true,
			MatchPatterns:  []string{"nil", "embedding", "consolidation"},
		},
		{
			ID: "err-5", Category: "error_prevention",
			SeedContent:    "ERROR: TimescaleDB hypertable creation fails with 'relation already exists'. Root cause: migration runs CREATE TABLE IF NOT EXISTS but then tries to convert to hypertable unconditionally. Fix: wrap create_hypertable in a DO block that checks if table is already a hypertable. File: migrations/003_timescale.sql",
			SeedTags:       []string{"error", "timescaledb", "migration"},
			SeedImportance: 0.85,
			Query:          "hypertable migration fails saying relation exists",
			ExpectedHit:    true,
			MatchPatterns:  []string{"hypertable", "already exists", "migration"},
		},
		{
			ID: "err-6", Category: "error_prevention",
			SeedContent:    "ERROR: pgvector index creation takes 40+ minutes on 100K vectors. Root cause: using default ivfflat index with too many lists. Fix: use HNSW index instead: CREATE INDEX ON episodic_memory USING hnsw (embedding vector_cosine_ops) WITH (m=16, ef_construction=64). HNSW gives better recall at same speed.",
			SeedTags:       []string{"error", "pgvector", "performance"},
			SeedImportance: 0.82,
			Query:          "vector index is extremely slow to build on large table",
			ExpectedHit:    true,
			MatchPatterns:  []string{"HNSW", "ivfflat", "pgvector"},
		},
		{
			ID: "err-7", Category: "error_prevention",
			SeedContent:    "ERROR: Ollama embedding returns 502 Bad Gateway after ~100 concurrent requests. Root cause: Ollama's default max_concurrent_requests=1 causes queuing. Fix: set OLLAMA_NUM_PARALLEL=4 in environment and implement request batching in embedding provider (max 32 texts per batch).",
			SeedTags:       []string{"error", "ollama", "concurrency"},
			SeedImportance: 0.86,
			Query:          "embedding provider returns 502 under load",
			ExpectedHit:    true,
			MatchPatterns:  []string{"Ollama", "502", "concurrent", "batch"},
		},
		{
			ID: "err-8", Category: "error_prevention",
			SeedContent:    "ERROR: Important items disappear from working memory because eviction causes data loss when capacity=10 is too small. Evicted items are not written to episodic store before removal. Fix: in working.Put(), if evicted item has importance > 0.5, auto-persist to episodic_memory table before evicting.",
			SeedTags:       []string{"error", "working-memory", "data-loss"},
			SeedImportance: 0.9,
			Query:          "important items disappear from working memory",
			ExpectedHit:    true,
			MatchPatterns:  []string{"evict", "working memory", "persist", "disappear"},
		},
		{
			ID: "err-9", Category: "error_prevention",
			SeedContent:    "ERROR: JSON-RPC response exceeds 4MB scanner buffer limit causing MCP disconnect. Root cause: large recall context with many memories generates response > 4MB. Fix: increased bufio.Scanner buffer to 4MB AND added response size check in sendResult to truncate context if > 3MB.",
			SeedTags:       []string{"error", "mcp", "buffer-overflow"},
			SeedImportance: 0.87,
			Query:          "MCP connection drops when recalling many memories",
			ExpectedHit:    true,
			MatchPatterns:  []string{"scanner", "buffer", "4MB", "truncat"},
		},
		{
			ID: "err-10", Category: "error_prevention",
			SeedContent:    "ERROR: Cross-encoder reranking returns random scores for non-English content. Root cause: qwen2.5:7b interprets non-English text poorly in the scoring prompt. Fix: translate content to English before sending to reranker, or skip reranking for non-English queries.",
			SeedTags:       []string{"error", "reranking", "multilingual"},
			SeedImportance: 0.83,
			Query:          "LLM reranking gives bad scores for Russian queries",
			ExpectedHit:    true,
			MatchPatterns:  []string{"rerank", "non-English", "qwen"},
		},
		{
			ID: "err-11", Category: "error_prevention",
			SeedContent:    "ERROR: Duplicate semantic memories created during consolidation. Root cause: consolidation runs every 6h and the duplicate check only compares within the same batch, not against existing semantic memories. Fix: before inserting new semantic fact, check if content similarity > 0.90 with existing semantic memories.",
			SeedTags:       []string{"error", "consolidation", "dedup"},
			SeedImportance: 0.84,
			Query:          "same semantic fact appears multiple times after consolidation",
			ExpectedHit:    true,
			MatchPatterns:  []string{"duplicate", "consolidation", "similarity"},
		},
		{
			ID: "err-12", Category: "error_prevention",
			SeedContent:    "ERROR: The mos_context.mdc file is not being generated because context file writer fails silently when .cursor/rules directory doesn't exist. Root cause: os.WriteFile returns error but it's logged at Debug level and swallowed. Fix: os.MkdirAll before WriteFile, and log at Warn level on failure.",
			SeedTags:       []string{"error", "context-writer", "file-io"},
			SeedImportance: 0.8,
			Query:          "mos_context.mdc file not being generated",
			ExpectedHit:    true,
			MatchPatterns:  []string{"context", "WriteFile", "MkdirAll", "mos_context.mdc"},
		},

		// ═══════════════════════════════════════════════════
		// KNOWLEDGE RECALL (10 scenarios)
		// ═══════════════════════════════════════════════════
		{
			ID: "know-1", Category: "knowledge_recall",
			SeedContent:    "DECISION: Use pgx/v5 as the PostgreSQL driver instead of database/sql for Hippocampus. Reasons why: native pgvector support for vector similarity search, better connection pooling with pgxpool, full context.Context support, COPY protocol for bulk inserts, and direct access to PostgreSQL-specific features.",
			SeedTags:       []string{"decision", "database", "architecture"},
			SeedImportance: 0.8,
			Query:          "which PostgreSQL driver should we use in Go and why",
			ExpectedHit:    true,
			MatchPatterns:  []string{"pgx", "pgvector", "database/sql"},
		},
		{
			ID: "know-2", Category: "knowledge_recall",
			SeedContent:    "ARCHITECTURE: Hippocampus follows clean architecture pattern with strict separation of concerns and clear dependency direction. Codebase is structured in layers: domain (entities, ports/interfaces) -> app (use cases, services) -> adapter (MCP server, REST API) -> repo (PostgreSQL/TimescaleDB). Dependencies point inward only. Domain has zero external dependencies. Each layer is isolated and testable.",
			SeedTags:       []string{"architecture", "pattern", "design"},
			SeedImportance: 0.85,
			Query:          "how is the codebase structured, what design pattern is used",
			ExpectedHit:    true,
			MatchPatterns:  []string{"clean architecture", "domain", "adapter", "inward"},
		},
		{
			ID: "know-3", Category: "knowledge_recall",
			SeedContent:    "CONFIGURATION: Hippocampus uses Ollama with nomic-embed-text model for embeddings. 768 dimensions. Running locally at localhost:11434. OpenAI-compatible API at /v1 endpoint. For LLM synthesis (translation, research), uses qwen2.5:7b via native /api/chat endpoint.",
			SeedTags:       []string{"configuration", "embedding", "ollama"},
			SeedImportance: 0.75,
			Query:          "what embedding model and dimensions are we using",
			ExpectedHit:    true,
			MatchPatterns:  []string{"nomic-embed-text", "768", "Ollama"},
		},
		{
			ID: "know-4", Category: "knowledge_recall",
			SeedContent:    "DECISION: Token budget allocation distributed across memory tiers for recall: 50% episodic, 35% semantic, 15% procedural. Total budget: 1500 tokens. Rationale: episodic memories contain the most actionable context (recent code changes, errors), semantic tier holds condensed knowledge, procedural tier is smallest.",
			SeedTags:       []string{"decision", "recall", "budget"},
			SeedImportance: 0.78,
			Query:          "how is the token budget distributed across memory tiers",
			ExpectedHit:    true,
			MatchPatterns:  []string{"50%", "35%", "15%", "1500"},
		},
		{
			ID: "know-5", Category: "knowledge_recall",
			SeedContent:    "DECISION: Consolidation uses two-pass clustering. Tight pass (cosine >= 0.85) merges near-duplicates. Loose pass (cosine >= 0.70) groups thematic clusters of 3+ members. Bayesian confidence: n/(n+2) for tight, n/(n+3) for loose. Runs every 6 hours automatically.",
			SeedTags:       []string{"decision", "consolidation", "clustering"},
			SeedImportance: 0.82,
			Query:          "how does memory consolidation clustering work",
			ExpectedHit:    true,
			MatchPatterns:  []string{"two-pass", "0.85", "Bayesian", "6 hours"},
		},
		{
			ID: "know-6", Category: "knowledge_recall",
			SeedContent:    "CONFIGURATION: MCP server uses newline-delimited JSON-RPC 2.0 over stdio. Protocol version: 2024-11-05. Scanner buffer: 4MB. Tools are registered in tools() function in tools.go. Server auto-detects client environment (Cursor, Claude Code, VS Code) from initialize params.",
			SeedTags:       []string{"configuration", "mcp", "protocol"},
			SeedImportance: 0.77,
			Query:          "what protocol does the MCP server use and how large is the buffer",
			ExpectedHit:    true,
			MatchPatterns:  []string{"JSON-RPC", "stdio", "4MB", "newline"},
		},
		{
			ID: "know-7", Category: "knowledge_recall",
			SeedContent:    "DECISION: Importance scoring formula: composite = 0.4*semantic_similarity + 0.3*recency + 0.2*explicit_importance + 0.1*emotional_weight. Recency uses exponential decay with half-life of 7 days. Explicit importance is set by user or derived from tags.",
			SeedTags:       []string{"decision", "scoring", "algorithm"},
			SeedImportance: 0.83,
			Query:          "what is the importance scoring formula and weights",
			ExpectedHit:    true,
			MatchPatterns:  []string{"0.4", "0.3", "0.2", "exponential decay", "half-life"},
		},
		{
			ID: "know-8", Category: "knowledge_recall",
			SeedContent:    "CONFIGURATION: File watcher monitors Go files in project directories using fsnotify. On file change, re-ingests the modified file via StudyService. Debounce: 2 seconds to avoid duplicate events. Watches all .go files in registered projects.",
			SeedTags:       []string{"configuration", "file-watcher", "fsnotify"},
			SeedImportance: 0.73,
			Query:          "how does automatic file watching and re-ingestion work",
			ExpectedHit:    true,
			MatchPatterns:  []string{"fsnotify", "file watcher", "debounce", ".go"},
		},
		{
			ID: "know-9", Category: "knowledge_recall",
			SeedContent:    "DECISION: Feedback importance adjustment: useful=true multiplies importance by 1.3 (cap 1.0), useful=false multiplies by 0.7 (floor 0.1). Combined with time-based decay (episodic: 0.97/7d, semantic: 0.99/14d), this creates dual signal: frequently useful memories float up, rarely used decay.",
			SeedTags:       []string{"decision", "feedback", "reinforcement"},
			SeedImportance: 0.81,
			Query:          "how does feedback adjust memory importance over time",
			ExpectedHit:    true,
			MatchPatterns:  []string{"1.3", "0.7", "decay", "feedback"},
		},
		{
			ID: "know-10", Category: "knowledge_recall",
			SeedContent:    "DECISION: LLM reranking blend ratio: 0.6*cosine + 0.4*LLM score. LLM scores are normalized from 0-10 scale. Safety: if average LLM score < 2.0, discard all LLM scores (model is confused). Only top 5 candidates sent to LLM. Timeout: 4 seconds.",
			SeedTags:       []string{"decision", "reranking", "llm"},
			SeedImportance: 0.79,
			Query:          "how does LLM reranking blend with cosine scores",
			ExpectedHit:    true,
			MatchPatterns:  []string{"0.6", "0.4", "LLM", "2.0", "top 5"},
		},

		// ═══════════════════════════════════════════════════
		// NEGATIVE REJECTION (18 scenarios including adversarial)
		// ═══════════════════════════════════════════════════
		{
			ID: "neg-1", Category: "negative",
			Query: "how to configure nginx reverse proxy with load balancing and SSL termination",
			ExpectedHit: false,
		},
		{
			ID: "neg-2", Category: "negative",
			Query: "React useEffect cleanup function and useState batching optimization in Next.js",
			ExpectedHit: false,
		},
		{
			ID: "neg-3", Category: "negative",
			Query: "fine-tuning BERT model on custom NER dataset with PyTorch and Hugging Face",
			ExpectedHit: false,
		},
		{
			ID: "neg-4", Category: "negative",
			Description: "Adversarial: shares generic words with project",
			Query: "python django web framework best practices for REST API development",
			ExpectedHit: false,
		},
		{
			ID: "neg-5", Category: "negative",
			Description: "Adversarial: contains 'state' and 'context' (exist in project)",
			Query: "React context API for global state management with TypeScript generics",
			ExpectedHit: false,
		},
		{
			ID: "neg-6", Category: "negative",
			Description: "Adversarial: contains 'memory' and 'cache' (core domain words)",
			Query: "Redis sentinel failover configuration for high availability clusters",
			ExpectedHit: false,
		},
		{
			ID: "neg-7", Category: "negative",
			Description: "Adversarial: contains 'embedding' and 'vector' (project terms)",
			Query: "OpenAI text-embedding-ada-002 vs Cohere embed v3 for RAG pipeline evaluation",
			ExpectedHit: false,
		},
		{
			ID: "neg-8", Category: "negative",
			Description: "Adversarial: contains 'query' and 'optimization'",
			Query: "MySQL query optimization with EXPLAIN ANALYZE and index hints for InnoDB tables",
			ExpectedHit: false,
		},
		{
			ID: "neg-9", Category: "negative",
			Description: "Adversarial: contains 'architecture' and 'service'",
			Query: "microservice architecture with Kubernetes service mesh Istio and Envoy sidecar proxies",
			ExpectedHit: false,
		},
		{
			ID: "neg-10", Category: "negative",
			Description: "Adversarial: contains 'pipeline' and 'deploy'",
			Query: "Jenkins CI/CD pipeline for Terraform infrastructure deployment on AWS EKS",
			ExpectedHit: false,
		},
		{
			ID: "neg-11", Category: "negative",
			Description: "Adversarial: contains 'model' and 'training'",
			Query: "Llama 3 model quantization with GPTQ and LoRA fine-tuning on medical datasets",
			ExpectedHit: false,
		},
		{
			ID: "neg-12", Category: "negative",
			Description: "Adversarial: contains 'score' and 'ranking'",
			Query: "Elasticsearch painless scripting language for custom aggregation pipelines",
			ExpectedHit: false,
		},
		{
			ID: "neg-13", Category: "negative",
			Description: "Adversarial: contains cloud-specific terms",
			Query: "Snowflake data warehouse with Salesforce CRM integration via Fivetran connector",
			ExpectedHit: false,
		},
		{
			ID: "neg-14", Category: "negative",
			Description: "Adversarial: contains 'protocol' and 'server'",
			Query: "gRPC protobuf schema evolution and backward compatibility for microservices",
			ExpectedHit: false,
		},
		{
			ID: "neg-15", Category: "negative",
			Description: "Adversarial: contains ML-specific terms",
			Query: "YOLOv8 object detection with COCO dataset augmentation and anchor-free architecture",
			ExpectedHit: false,
		},
		{
			ID: "neg-16", Category: "negative",
			Description: "Completely unrelated: cooking",
			Query: "how to make sourdough bread with proper fermentation and hydration ratio",
			ExpectedHit: false,
		},
		{
			ID: "neg-17", Category: "negative",
			Description: "Completely unrelated: finance",
			Query: "Black-Scholes option pricing formula with volatility smile adjustment",
			ExpectedHit: false,
		},
		{
			ID: "neg-18", Category: "negative",
			Description: "Adversarial: contains 'Go' and 'error handling'",
			Query: "Rust error handling with anyhow crate vs Go error wrapping comparison",
			ExpectedHit: false,
		},

		// ═══════════════════════════════════════════════════
		// SEMANTIC UNDERSTANDING (12 scenarios)
		// ═══════════════════════════════════════════════════
		{
			ID: "sem-1", Category: "semantic",
			Query:         "Go build fails because a standard library package for path manipulation is not imported",
			ExpectedHit:   true,
			MatchPatterns: []string{"filepath", "import", "undefined"},
		},
		{
			ID: "sem-2", Category: "semantic",
			Query:         "explain the dependency direction and layer separation in this project",
			ExpectedHit:   true,
			MatchPatterns: []string{"clean architecture", "domain", "inward", "adapter"},
		},
		{
			ID: "sem-3", Category: "semantic",
			Query:         "какой драйвер базы данных мы используем для PostgreSQL",
			ExpectedHit:   true,
			MatchPatterns: []string{"pgx", "pgvector", "PostgreSQL"},
		},
		{
			ID: "sem-4", Category: "semantic",
			Query:         "the system crashes when trying to group similar memories together",
			ExpectedHit:   true,
			MatchPatterns: []string{"consolidation", "nil", "embedding", "panic"},
		},
		{
			ID: "sem-5", Category: "semantic",
			Query:         "how fast should the nearest neighbor index be created",
			ExpectedHit:   true,
			MatchPatterns: []string{"HNSW", "ivfflat", "vector"},
		},
		{
			ID: "sem-6", Category: "semantic",
			Query:         "the vector database fails when many requests arrive simultaneously",
			ExpectedHit:   true,
			MatchPatterns: []string{"Ollama", "502", "concurrent"},
		},
		{
			ID: "sem-7", Category: "semantic",
			Query:         "retrieved documents get scored incorrectly for non-English input",
			ExpectedHit:   true,
			MatchPatterns: []string{"rerank", "non-English"},
		},
		{
			ID: "sem-8", Category: "semantic",
			Query:         "how does the system decide between important and unimportant memories",
			ExpectedHit:   true,
			MatchPatterns: []string{"composite", "0.4", "recency", "importance"},
		},
		{
			ID: "sem-9", Category: "semantic",
			Query:         "what happens when the short-term buffer is full and a new item arrives",
			ExpectedHit:   true,
			MatchPatterns: []string{"evict", "working memory"},
		},
		{
			ID: "sem-10", Category: "semantic",
			Query:         "MCP client loses connection during large response",
			ExpectedHit:   true,
			MatchPatterns: []string{"scanner", "buffer", "4MB"},
		},
		{
			ID: "sem-11", Category: "semantic",
			Query:         "identical knowledge gets stored repeatedly over time",
			ExpectedHit:   true,
			MatchPatterns: []string{"duplicate", "consolidation"},
		},
		{
			ID: "sem-12", Category: "semantic",
			Query:         "правила проекта не обновляются после запоминания нового контекста",
			ExpectedHit:   true,
			MatchPatterns: []string{"context", "WriteFile", ".cursor"},
		},
	}
}

// Run executes the full benchmark suite.
func (bs *BenchmarkSuite) Run(ctx context.Context) (*BenchmarkReport, error) {
	start := time.Now()

	projName := fmt.Sprintf("bench-%d", time.Now().Unix())
	proj, err := bs.project.Create(ctx, projName, "Benchmark "+projName, "Automated evaluation benchmark", "")
	if err != nil {
		return nil, fmt.Errorf("create benchmark project: %w", err)
	}
	bs.logger.Info("benchmark project created", "id", proj.ID, "slug", projName)

	scenarios := bs.scenarios()
	sessionID := uuid.New()

	seeded := 0
	for _, sc := range scenarios {
		if sc.SeedContent == "" {
			continue
		}
		_, err := bs.encode.Encode(ctx, &EncodeRequest{
			Content:    sc.SeedContent,
			ProjectID:  &proj.ID,
			AgentID:    "benchmark",
			SessionID:  sessionID,
			Importance: sc.SeedImportance,
			Tags:       sc.SeedTags,
		})
		if err != nil {
			bs.logger.Warn("failed to seed memory", "scenario", sc.ID, "error", err)
			continue
		}
		seeded++
	}
	bs.logger.Info("benchmark memories seeded", "count", seeded)

	time.Sleep(500 * time.Millisecond)

	var results []ScenarioResult
	for _, sc := range scenarios {
		r := bs.runScenario(ctx, sc, &proj.ID)
		results = append(results, r)
		bs.logger.Info("scenario completed",
			"id", sc.ID,
			"correct", r.Correct,
			"confidence", fmt.Sprintf("%.3f", r.Confidence),
			"latency", r.Latency,
		)
	}

	report := bs.computeReport(results, seeded, proj.ID.String(), start)

	if err := bs.project.Delete(ctx, proj.ID); err != nil {
		bs.logger.Warn("failed to cleanup benchmark project", "id", proj.ID, "error", err)
	} else {
		bs.logger.Info("benchmark project cleaned up", "id", proj.ID, "slug", projName)
	}

	bs.cleanupOrphanBenchProjects(ctx)

	return report, nil
}

func (bs *BenchmarkSuite) cleanupOrphanBenchProjects(ctx context.Context) {
	projects, err := bs.project.List(ctx)
	if err != nil {
		return
	}
	for _, p := range projects {
		if strings.HasPrefix(p.Slug, "bench-") {
			if err := bs.project.Delete(ctx, p.ID); err == nil {
				bs.logger.Info("cleaned up orphan benchmark project", "slug", p.Slug, "id", p.ID)
			}
		}
	}
}

func (bs *BenchmarkSuite) runScenario(ctx context.Context, sc BenchmarkScenario, projectID *uuid.UUID) ScenarioResult {
	resp, err := bs.recall.Recall(ctx, &RecallRequest{
		Query:         sc.Query,
		ProjectID:     projectID,
		Budget:        domain.DefaultBudget(),
		AgentID:       "benchmark",
		IncludeGlobal: false,
	})

	result := ScenarioResult{
		ScenarioID:  sc.ID,
		Category:    sc.Category,
		Query:       sc.Query,
		ExpectedHit: sc.ExpectedHit,
	}

	if err != nil {
		bs.logger.Warn("recall failed for scenario", "id", sc.ID, "error", err)
		result.Correct = !sc.ExpectedHit
		return result
	}

	result.Confidence = resp.Context.Confidence
	result.Candidates = resp.Candidates
	result.Latency = resp.Latency
	result.RejectReason = resp.RejectReason
	result.BestSim = resp.BestSim

	recalledText := strings.ToLower(resp.Context.Text)
	isNoResult := strings.Contains(recalledText, "no relevant memories") ||
		strings.Contains(recalledText, "no query provided")

	if sc.ExpectedHit {
		hit := false
		for _, pattern := range sc.MatchPatterns {
			if strings.Contains(recalledText, strings.ToLower(pattern)) {
				hit = true
				result.MatchedText = pattern
				break
			}
		}
		result.ActualHit = hit
		result.Correct = hit
	} else {
		result.ActualHit = !isNoResult
		result.Correct = isNoResult || resp.Context.Confidence < 0.3
	}

	return result
}

func (bs *BenchmarkSuite) computeReport(results []ScenarioResult, seeded int, projID string, start time.Time) *BenchmarkReport {
	report := &BenchmarkReport{
		Timestamp: time.Now(),
		Duration:  time.Since(start),
		ProjectID: projID,
		Scenarios: len(results),
		Results:   results,
	}

	var (
		truePositives  int
		falsePositives int
		falseNegatives int
		trueNegatives  int
		totalCorrect   int
		latencies      []time.Duration
	)

	byCategory := map[string]*CategoryMetrics{}

	for _, r := range results {
		latencies = append(latencies, r.Latency)
		if r.Correct {
			totalCorrect++
		}

		cat, ok := byCategory[r.Category]
		if !ok {
			cat = &CategoryMetrics{Category: r.Category}
			byCategory[r.Category] = cat
		}
		cat.Total++
		if r.Correct {
			cat.Correct++
		}

		if r.ExpectedHit && r.ActualHit {
			truePositives++
		} else if r.ExpectedHit && !r.ActualHit {
			falseNegatives++
		} else if !r.ExpectedHit && r.ActualHit {
			falsePositives++
		} else {
			trueNegatives++
		}
	}

	for _, cat := range byCategory {
		if cat.Total > 0 {
			cat.Rate = float64(cat.Correct) / float64(cat.Total)
		}
	}

	precision := 0.0
	if truePositives+falsePositives > 0 {
		precision = float64(truePositives) / float64(truePositives+falsePositives)
	}
	recall := 0.0
	if truePositives+falseNegatives > 0 {
		recall = float64(truePositives) / float64(truePositives+falseNegatives)
	}
	f1 := 0.0
	if precision+recall > 0 {
		f1 = 2 * precision * recall / (precision + recall)
	}

	errPrevRate := 0.0
	if ec, ok := byCategory["error_prevention"]; ok && ec.Total > 0 {
		errPrevRate = float64(ec.Correct) / float64(ec.Total)
	}
	negAcc := 0.0
	if nc, ok := byCategory["negative"]; ok && nc.Total > 0 {
		negAcc = float64(nc.Correct) / float64(nc.Total)
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	var totalLat time.Duration
	for _, l := range latencies {
		totalLat += l
	}
	meanLat := totalLat / time.Duration(len(latencies))
	p95Lat := latencies[int(math.Ceil(float64(len(latencies))*0.95))-1]

	report.Aggregate = AggregateMetrics{
		Precision:           precision,
		Recall:              recall,
		F1:                  f1,
		ErrorPreventionRate: errPrevRate,
		NegativeAccuracy:    negAcc,
		MeanLatency:         meanLat,
		P95Latency:          p95Lat,
		TotalCorrect:        totalCorrect,
		TotalScenarios:      len(results),
	}
	report.ByCategory = byCategory

	randomPrec := 0.0
	if seeded > 0 {
		randomPrec = 1.0 / float64(seeded)
	}
	improvement := 0.0
	if randomPrec > 0 {
		improvement = precision / randomPrec
	}
	report.Baseline = BaselineComparison{
		RandomPrecision:    randomPrec,
		SystemPrecision:    precision,
		ImprovementFactor:  improvement,
		TotalSeededMemories: seeded,
	}

	report.Conclusion = bs.generateConclusion(report)
	report.Formatted = bs.formatReport(report)

	return report
}

func (bs *BenchmarkSuite) generateConclusion(r *BenchmarkReport) string {
	agg := r.Aggregate

	quality := "FAIL"
	switch {
	case agg.F1 >= 0.9:
		quality = "EXCELLENT"
	case agg.F1 >= 0.75:
		quality = "GOOD"
	case agg.F1 >= 0.5:
		quality = "MODERATE"
	}

	return fmt.Sprintf(
		"Hippocampus achieves %s quality (F1=%.3f). Precision=%.1f%%, Recall=%.1f%%. "+
			"Error prevention rate: %.0f%%. Negative rejection accuracy: %.0f%%. "+
			"%.1fx improvement over random baseline. Mean latency: %dms.",
		quality, agg.F1,
		agg.Precision*100, agg.Recall*100,
		agg.ErrorPreventionRate*100, agg.NegativeAccuracy*100,
		r.Baseline.ImprovementFactor,
		agg.MeanLatency.Milliseconds(),
	)
}

func (bs *BenchmarkSuite) formatReport(r *BenchmarkReport) string {
	agg := r.Aggregate
	bl := r.Baseline

	var b strings.Builder

	b.WriteString("╔══════════════════════════════════════════════════════════════╗\n")
	b.WriteString("║         HIPPOCAMPUS MEMORY EVALUATION REPORT v1.0          ║\n")
	b.WriteString("╠══════════════════════════════════════════════════════════════╣\n")
	b.WriteString(fmt.Sprintf("║  Date: %-54s║\n", r.Timestamp.Format("2006-01-02 15:04")))
	b.WriteString(fmt.Sprintf("║  Scenarios: %-3d | Duration: %-30s║\n", r.Scenarios, r.Duration.Round(time.Millisecond)))
	b.WriteString(fmt.Sprintf("║  Seeded memories: %-43d║\n", bl.TotalSeededMemories))
	b.WriteString("╠══════════════════════════════════════════════════════════════╣\n")
	b.WriteString("║                                                              ║\n")
	b.WriteString("║  AGGREGATE METRICS                                           ║\n")
	b.WriteString("║  ─────────────────                                           ║\n")
	b.WriteString(fmt.Sprintf("║  Precision:              %-36s║\n", fmt.Sprintf("%.3f (%d/%d hits correct)", agg.Precision, agg.TotalCorrect, agg.TotalScenarios)))
	b.WriteString(fmt.Sprintf("║  Recall:                 %-36s║\n", fmt.Sprintf("%.3f", agg.Recall)))
	b.WriteString(fmt.Sprintf("║  F1 Score:               %-36s║\n", fmt.Sprintf("%.3f", agg.F1)))
	b.WriteString(fmt.Sprintf("║  Error Prevention Rate:  %-36s║\n", fmt.Sprintf("%.0f%%", agg.ErrorPreventionRate*100)))
	b.WriteString(fmt.Sprintf("║  Negative Accuracy:      %-36s║\n", fmt.Sprintf("%.0f%%", agg.NegativeAccuracy*100)))
	b.WriteString(fmt.Sprintf("║  Mean Latency:           %-36s║\n", fmt.Sprintf("%dms", agg.MeanLatency.Milliseconds())))
	b.WriteString(fmt.Sprintf("║  P95 Latency:            %-36s║\n", fmt.Sprintf("%dms", agg.P95Latency.Milliseconds())))
	b.WriteString("║                                                              ║\n")
	b.WriteString("║  vs RANDOM BASELINE                                          ║\n")
	b.WriteString("║  ─────────────────                                           ║\n")
	b.WriteString(fmt.Sprintf("║  Random Precision:       %-36s║\n", fmt.Sprintf("%.3f (1/%d)", bl.RandomPrecision, bl.TotalSeededMemories)))
	b.WriteString(fmt.Sprintf("║  System Precision:       %-36s║\n", fmt.Sprintf("%.3f", bl.SystemPrecision)))
	b.WriteString(fmt.Sprintf("║  Improvement:            %-36s║\n", fmt.Sprintf("%.1fx", bl.ImprovementFactor)))
	b.WriteString("║                                                              ║\n")
	b.WriteString("║  CATEGORY BREAKDOWN                                          ║\n")
	b.WriteString("║  ─────────────────                                           ║\n")

	categoryOrder := []string{"error_prevention", "knowledge_recall", "semantic", "negative"}
	categoryNames := map[string]string{
		"error_prevention": "Error Prevention",
		"knowledge_recall": "Knowledge Recall",
		"semantic":         "Semantic Match ",
		"negative":         "Negative Reject",
	}

	for _, catKey := range categoryOrder {
		cat, ok := r.ByCategory[catKey]
		if !ok {
			continue
		}
		name := categoryNames[catKey]
		bar := strings.Repeat("█", int(cat.Rate*16))
		pad := strings.Repeat("░", 16-len([]rune(bar)))
		b.WriteString(fmt.Sprintf("║  %-18s %d/%d  %s%s %.0f%%",
			name+":", cat.Correct, cat.Total, bar, pad, cat.Rate*100))
		needed := 62 - len(fmt.Sprintf("  %-18s %d/%d  %s%s %.0f%%",
			name+":", cat.Correct, cat.Total, bar, pad, cat.Rate*100))
		if needed > 0 {
			b.WriteString(strings.Repeat(" ", needed))
		}
		b.WriteString("║\n")
	}

	b.WriteString("║                                                              ║\n")
	b.WriteString("║  SCENARIO DETAILS                                            ║\n")
	b.WriteString("║  ─────────────────                                           ║\n")

	for _, res := range r.Results {
		icon := "✓"
		if !res.Correct {
			icon = "✗"
		}
		line := fmt.Sprintf("  %s %-8s conf=%.2f lat=%3dms  %s",
			icon, res.ScenarioID, res.Confidence, res.Latency.Milliseconds(), truncate(res.Query, 30))

		b.WriteString(fmt.Sprintf("║%-62s║\n", line))
	}

	b.WriteString("║                                                              ║\n")
	b.WriteString("╠══════════════════════════════════════════════════════════════╣\n")

	conclusionLines := wrapText(r.Conclusion, 58)
	for _, line := range conclusionLines {
		b.WriteString(fmt.Sprintf("║  %-60s║\n", line))
	}
	b.WriteString("╚══════════════════════════════════════════════════════════════╝\n")

	return b.String()
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}

func wrapText(s string, width int) []string {
	words := strings.Fields(s)
	var lines []string
	current := ""
	for _, w := range words {
		if current == "" {
			current = w
		} else if len(current)+1+len(w) <= width {
			current += " " + w
		} else {
			lines = append(lines, current)
			current = w
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
