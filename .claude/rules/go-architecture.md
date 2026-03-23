---
description: Go clean architecture — enterprise-grade patterns, always active for Go files
paths:
  - "**/*.go"
---

# Go Architecture — Enterprise Grade

## Project Structure: Clean Architecture
```
cmd/           → Bootstrap ONLY: config → DI → start (<=150 lines per file)
internal/
  domain/      → Core: pure structs, interfaces, business rules, sentinel errors
  app/         → Use cases: services orchestrate domain logic + repos
  adapter/     → External world: MCP, REST, gRPC, LLM providers
  repo/        → Data access: SQL ONLY here, pgx/v5
  pkg/         → Leaf utilities: no business logic, no internal imports
  embedding/   → Embedding provider abstraction
  memory/      → In-memory stores (WorkingMemory)
  metrics/     → Prometheus counters/histograms (leaf package)
migrations/    → Versioned .up.sql / .down.sql
```

## Dependency Rule (INVIOLABLE)
```
adapter → app/service → domain
              ↓
         repo (SQL)
```
- domain imports NOTHING from adapter/repo/pkg/app
- adapter/mcp does NOT import adapter/rest
- repo does NOT import app
- metrics and pkg are LEAF packages — they import only stdlib and external libs
- Shared instrumentation (metrics, tracing) MUST live in leaf packages to prevent import cycles

## Package Naming (Go Convention)
- Short, lowercase, single word: `repo`, `mcp`, `rest`, `metrics`
- No underscores, no camelCase: `vecutil` not `vec_util` or `vecUtil`
- Package name should describe what it PROVIDES, not what it CONTAINS
- No `util`, `common`, `misc`, `helpers` packages — find a better name
- Internal packages (`internal/`) enforce compiler-level encapsulation

## Interface Design
- **Consumer defines** the interface (Accept interfaces, return structs)
- Small interfaces: 1-3 methods. If >5 methods → split by behavior
- Define interfaces where they're USED, not where they're implemented
- Name interfaces by behavior: `Reader`, `Embedder`, `Encoder` — not `IMemory`, `MemoryInterface`
- Return concrete types from constructors, accept interfaces in functions

## Dependency Injection
- All dependencies via constructor injection: `func NewService(repo Repo, logger *slog.Logger) *Service`
- No global state, no `init()` for wiring, no service locators
- Container (cmd/) is the only place that knows all concrete types
- Dependencies flow DOWN: cmd → adapter/app → domain. Never up.

## Error Handling

### Error Flow
- Lower layers: `return fmt.Errorf("operation context: %w", err)` — wrap with context, never log
- Top layer (handler/main): log ONCE with full chain, return generic message to client
- Use `errors.Is()` / `errors.As()` for type checking — never string comparison

### Error Taxonomy
- **Domain errors** (sentinel): `ErrNotFound`, `ErrGateRejected`, `ErrEmptyContent` — defined in domain/errors.go
- **Infrastructure errors**: DB connection, embedding timeout, LLM unavailable — wrap with context
- **Validation errors**: bad input, missing fields — return early, never reach service layer
- **Retryable vs terminal**: infrastructure errors MAY be retried, domain errors are terminal
- Handlers translate domain errors to HTTP status codes / MCP error codes

### Anti-Patterns
- NEVER swallow errors: `_ = someFunc()` — always handle or document why
- NEVER log AND return the same error — pick one, or the same error appears twice in logs
- NEVER expose SQL details, stack traces, or internal paths to clients
- NEVER use `panic()` in library code — return errors

## Concurrency

### Goroutines
- Every goroutine MUST receive `context.Context` for cancellation
- Use `errgroup.Group` for coordinated work — not bare `go func()`
- Track goroutines with `sync.WaitGroup` if not using errgroup
- NEVER fire-and-forget: every goroutine must have a shutdown path

### Synchronization — Use the Right Tool
- **Mutex** (`sync.Mutex`/`sync.RWMutex`): protecting shared state (maps, slices, counters). Idiomatic Go.
- **Channels**: communication between goroutines, fan-out/fan-in, signaling
- **sync.Once**: one-time initialization
- **sync/atomic**: simple counters, flags
- Rule: "Share memory by communicating" applies to ORCHESTRATION. For state protection, mutexes are correct and idiomatic.

### Data Race Prevention
- WorkingMemory.Snapshot() MUST hold mutex — prior crash from data race
- Any struct accessed from multiple goroutines → document thread safety in comment
- Run tests with `-race` flag — always

## Graceful Shutdown Pattern
```
Signal (SIGINT/SIGTERM)
  → Stop accepting new work (scheduler, watchers)
  → Wait for in-flight requests (WaitGroup/errgroup)
  → Shutdown HTTP server (http.Server.Shutdown with timeout context)
  → Close DB pool (pool.Close())
  → Exit
```
- Shutdown timeout: 5-10 seconds. Log warning if exceeded.
- Order matters: stop producers before consumers, close connections last
- Context cancellation propagates through the entire call chain

## Context Propagation
- First parameter of every exported function: `ctx context.Context`
- Pass context through the entire chain: handler → service → repo → DB query
- Never store context in a struct — pass it as parameter
- Use `context.WithTimeout` for external calls (DB, HTTP, LLM) — always
- Respect `ctx.Done()` in long-running loops

## Logging (slog)
- Use structured logging: `logger.Info("event", "key", value, "key2", value2)`
- Levels:
  - **DEBUG**: raw data, internal state — OFF in production
  - **INFO**: significant lifecycle events (startup, connection, registration)
  - **WARN**: unusual but recoverable (retry, timeout, degraded mode)
  - **ERROR**: failure requiring attention (lost data, broken invariant)
- NEVER log at INFO in hot paths (per-request, per-message)
- NEVER log sensitive data (passwords, tokens, PII, embeddings)
- Pass `*slog.Logger` via DI — never use global logger
- In tests: `slog.New(slog.NewTextHandler(io.Discard, nil))`

## Configuration
- Secrets ONLY via env vars (`DATABASE_URL`, `OPENAI_API_KEY`) — never in config files
- Config validated at load time — fail fast on missing required values
- Config struct is immutable after load — no runtime mutation
- Timeouts, limits, thresholds should be configurable, not hardcoded

## Resource Management
- DB connections: pool with max conns, health checks, idle timeouts
- HTTP clients: set explicit timeouts, never use `http.DefaultClient`
- File handles, goroutines, channels: always close/clean up via `defer` or shutdown
- Unbounded growth: every slice, map, channel, buffer pool MUST have a max size

## Code Quality Guidelines
| Metric | Warning | Acceptable | Ideal |
|--------|---------|------------|-------|
| Lines per file | >600 | 200-600 | <300 |
| Methods per struct | >12 | 5-12 | <8 |
| Function params | >6 | 3-6 | <=3 |
| Cyclomatic complexity | >15 | 5-15 | <10 |
| SQL in non-repo code | >0 | 0 | 0 |
| Test coverage | <30% | 40-70% | >70% |

Function length: no hard limit. If a function has ONE responsibility and reads top-to-bottom without confusion — it's fine at 80 lines (table-driven tests, SQL builders, switch statements). Split when a function does MULTIPLE distinct things.

## Anti-Patterns
- **God Object**: struct >12 methods or >8 dependencies → split by responsibility
- **God File**: file >600 lines → extract cohesive groups
- **Circular imports**: always a design smell → extract shared types to domain or leaf package
- **Business logic in handlers**: handler = parse → validate → call service → respond
- **SQL outside repo/**: if you write `db.Query` outside `repo/` — stop
- **Logging AND returning error**: pick one per layer
- **Context in struct field**: pass as first parameter instead
