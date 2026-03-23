# Hippocampus MOS — Shared Agent Context

## What This Is
Memory Operating System for AI agents. Go + TimescaleDB + pgvector + Ollama embeddings.
MCP (Model Context Protocol) server + REST API + React dashboard.

## Build & Run

### Quick Start
```bash
make build          # build Go binary + web frontend → bin/hippocampus.exe
make build-go       # Go binary only (skip frontend)
make run            # build + run with config.json
```

### Database
```bash
make db-up          # start TimescaleDB via docker-compose
make migrate        # apply migrations via psql
make db-down        # stop TimescaleDB
```

### Docker (full stack)
```bash
make docker-build   # build container image
make docker-up      # TimescaleDB + Hippocampus + Prometheus + Grafana
make docker-down    # stop everything
```

### Build for MCP (Cursor/Claude Code)
```bash
go build -o bin/hippocampus.exe ./cmd/hippocampus/
# ALWAYS build to bin/ — Cursor caches old binaries from other paths
# Restart Cursor/Claude Code after rebuild
```

### Environment Variables
- `DATABASE_URL` — PostgreSQL connection string (required)
- `OPENAI_API_KEY` — for embedding + LLM providers
- `LLM_BASE_URL` — Ollama endpoint (default: http://localhost:11434/v1)
- `LOG_LEVEL` — debug/info/warn/error

## Architecture
```
cmd/hippocampus/     -> bootstrap (main.go + container.go)
internal/
  domain/            -> core structs, interfaces, errors (zero external deps)
  app/               -> services: EncodeService, RecallService, ConsolidateService, etc.
  adapter/mcp/       -> MCP stdio transport (handlers.go, server.go, tools.go)
  adapter/rest/      -> chi router, REST handlers
  adapter/llm/       -> OpenAI-compatible LLM provider, switchable
  adapter/source/    -> Knowledge agents (arxiv, github, hackernews, etc.)
  repo/              -> pgx v5, SQL queries, pgvector
  memory/            -> in-memory WorkingMemory (heap-based, mutex-protected)
  embedding/         -> Ollama/OpenAI embedding providers
  metrics/           -> Prometheus counters/histograms (leaf package)
  pkg/config/        -> JSON config + env var override
  pkg/logger/        -> slog setup
  pkg/tokenizer/     -> token estimation
  pkg/vecutil/       -> vector math utilities
```

## Key Data Flows
- **Encode**: content → qualityGate(min 5 words, non-stop ratio) → embed → noveltyGate(<0.10 = reject) → thalamicGate(importance*0.5 + quality*0.2 + novelty*0.3) → insert → emotionalTag → demoteSimilar → workingMemory
- **Recall**: query → embed → HybridRetriever(vec+BM25, RRF fusion) → score → filterWeak(abs=0.35, rel=0.65) → llmRerank(qwen2.5:7b, 4s timeout) → detectIrrelevant(6-layer, project-scoped fast path) → submodularSelect → assembleContext
- **Consolidate**: ListUnconsolidated → clusterByContentType → synthesizeCluster (LLM) → re-embed → promoteCluster → markConsolidated

## Error Taxonomy

### Domain Errors (internal/domain/errors.go)
Sentinel errors — use `errors.Is()` to check:
- `ErrNotFound`, `ErrMemoryNotFound`, `ErrProjectNotFound`, `ErrPredictionNotFound`
- `ErrGateRejected` — thalamic gate rejected (insufficient novelty/importance)
- `ErrBudgetExceeded` — token budget exceeded during assembly
- `ErrEmbeddingFailed` — embedding provider returned error
- `ErrAlreadyResolved` — prediction already has outcome
- `ErrInvalidTier`, `ErrEmptyContent`, `ErrProjectSlugTaken`

### Error Handling Convention
- Repo/service layer: wrap with `fmt.Errorf("context: %w", err)`, return
- Adapter layer (MCP/REST): log ONCE, return generic message to client
- Never log AND return — pick one per layer

## Database
- PostgreSQL 16 + TimescaleDB + pgvector (768-dim embeddings)
- Tables: episodic_memory, semantic_memory, procedural_memory, emotional_tags, causal_links, predictions, projects
- Migration tool: custom runner with advisory lock (repo/db.go)
- Connection pool: pgx/v5, 25 max / 2 min conns, 30min lifetime

## Testing

### Commands
```bash
make test               # go test -race -short ./...
make test-integration   # go test -run Integration ./...
make test-coverage      # coverage report to coverage.html
```

### Patterns
- **Inline mocks**: each test file defines its own mock structs (no mock library)
- **Table-driven**: descriptive subtest names, test behavior not implementation
- **Logger**: `slog.New(slog.NewTextHandler(io.Discard, nil))`
- **Helper constructors**: `newTestXxxService()` with mocks + discard logger
- Unit tests: no DB needed, mock repos
- Integration tests: need running TimescaleDB (`make db-up`)

## Tech Stack
- Go 1.25+, pgx/v5, chi/v5, slog (stdlib)
- Ollama: nomic-embed-text (embeddings), qwen2.5:7b (synthesis/rerank)
- TimescaleDB + pgvector for storage
- Prometheus for metrics, Grafana for dashboards
- React + Vite + Tailwind for dashboard (embedded in binary)

## Critical Invariants
- Domain layer has ZERO external imports — never break this
- WorkingMemory.Snapshot() MUST hold mutex — prior data race crash
- StoreIfProcedural is called ONCE inside EncodeService — never add a second call
- SQL queries MUST use parameterized args ($1, $2) — never string concat
- AgentID comes from MCP clientInfo.Name via s.agentID() — never hardcode
- Always build to bin/ directory — Cursor caches stale binaries
- MCP transport is JSONL (newline-delimited JSON), NOT Content-Length framing
