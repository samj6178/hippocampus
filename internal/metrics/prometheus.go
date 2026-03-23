package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	RecallTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "recall_total",
		Help:      "Total number of recall operations",
	})
	RecallHits = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "recall_hits_total",
		Help:      "Recall operations that returned results",
	})
	RecallMisses = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "recall_misses_total",
		Help:      "Recall operations that returned no results (rejected)",
	})
	RecallLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "hippocampus",
		Name:      "recall_latency_seconds",
		Help:      "Recall operation latency in seconds",
		Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
	})
	RecallTokens = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "hippocampus",
		Name:      "recall_tokens",
		Help:      "Token count in recall responses",
		Buckets:   []float64{100, 250, 500, 750, 1000, 1500, 2000, 3000},
	})
	RecallConfidence = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "hippocampus",
		Name:      "recall_confidence",
		Help:      "Confidence of recall responses",
		Buckets:   []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9},
	})

	EncodeTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "encode_total",
		Help:      "Total number of encode (remember) operations",
	})

	EmbeddingLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "hippocampus",
		Name:      "embedding_latency_seconds",
		Help:      "Embedding provider latency in seconds",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0},
	})
	EmbeddingErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "embedding_errors_total",
		Help:      "Total embedding provider errors",
	})

	ConsolidationRuns = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "consolidation_runs_total",
		Help:      "Total number of consolidation runs",
	})
	ConsolidationPromoted = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "consolidation_promoted_total",
		Help:      "Total semantic memories created by consolidation",
	})

	LLMRerankLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "hippocampus",
		Name:      "llm_rerank_latency_seconds",
		Help:      "LLM reranking latency in seconds",
		Buckets:   []float64{0.5, 1.0, 2.0, 4.0, 8.0},
	})

	MemoryCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "hippocampus",
		Name:      "memory_count",
		Help:      "Current number of memories by tier and project",
	}, []string{"tier", "project"})

	FeedbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "feedback_total",
		Help:      "Total feedback events by type",
	}, []string{"useful"})

	MCPToolCalls = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "mcp_tool_calls_total",
		Help:      "Total MCP tool calls by tool name",
	}, []string{"tool"})

	LearningLoopBroken = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "learning_loop_broken_total",
		Help:      "Number of times learning loop was detected as broken",
	})

	RejectionsByReason = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "rejections_total",
		Help:      "Query rejections by reason category",
	}, []string{"reason"})

	// Learning system metrics
	AutoCapturedErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "auto_captured_errors_total",
		Help:      "Errors automatically captured by observation layer",
	})
	RulesGenerated = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "rules_generated_total",
		Help:      "Prevention rules auto-generated from error patterns",
	})
	QueryExpansions = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "query_expansions_total",
		Help:      "Queries expanded via LLM for better recall",
	})

	WarningsExposed = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "warnings_exposed_total",
		Help:      "Total warning exposures injected into recall/file_context responses",
	})

	BugsPrevented = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "bugs_prevented_total",
		Help:      "Anti-patterns NOT found in code after warning exposure",
	})
	WarningsMissed = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "warnings_missed_total",
		Help:      "Anti-patterns found in code despite warning exposure",
	})

	LLMCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "llm_calls_total",
		Help:      "Total LLM API calls",
	}, []string{"caller"})

	LLMCallLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "hippocampus",
		Name:      "llm_call_latency_seconds",
		Help:      "LLM call latency in seconds",
		Buckets:   []float64{0.5, 1, 2, 5, 10, 20, 30, 60},
	}, []string{"caller"})

	LLMCallErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "hippocampus",
		Name:      "llm_call_errors_total",
		Help:      "LLM call errors",
	}, []string{"caller"})
)
