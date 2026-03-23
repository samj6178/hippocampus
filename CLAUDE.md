<!-- HIPPOCAMPUS:BEGIN -->
# Hippocampus MOS — Learned Knowledge

*Auto-generated: 2026-03-23 21:26 | 16 patterns from 17 topics*

Memory: 77 episodic, 948 semantic

## Critical Patterns (DO NOT REPEAT)

### Mcp (3 patterns)

- BUG: MCP stdio transport uses newline-delimited JSON, NOT Content-Length framing like LSP. CAUSE: copied LSP pattern blindly. FIX: changed to bufio.Scanner + json.Marshal + newline. FILE: internal/ada...
- The MCP tools not appearing in Cursor was caused by: Cursor caching old tool definitions from a previous hippocampus.exe binary. This led to hours of debugging. Fixed: always build to bin/ directory a...
- MCP transport was using Content-Length framing (LSP-style) but MCP stdio spec requires newline-delimited JSON. Fixed in internal/adapter/mcp/server.go: replaced bufio.Reader+readMessage with bufio.Sca...

### Decision (3 patterns)

- DECISION: LLM reranking blend ratio 0.6*cosine + 0.4*LLM (not 0.4/0.6 as initially coded). Reason: qwen2.5:7b is too weak as a relevance judge — initial 0.6 LLM weight caused false negatives (releva...
- DECISION: Switched recall rejection pipeline to use project-scoped fast path. When projectID is set and top-3 candidates include a memory from that project with similarity >= 0.40, skip entropy/spread...
- DECISION: Switched from pgx v4 to pgx v5 in hippocampus PostgreSQL repository layer. The pgxpool.Pool API changed — Connect() now returns (*Pool, error) instead of requiring Config first. Migration ...

### Production (2 patterns)

- Production Readiness Plan COMPLETE. All 9 phases shipped in one session:
- CRITICAL BUG: Production crash after deploying v0.8 — panic in recall_service.go line 156. Had to rollback immediately. Root cause: nil pointer dereference when embedding provider returned empty vec...

### Go-Compilation (2 patterns)

- ERROR: MCP binary path mismatch: Cursor starts bin/hippocampus.exe but 'go build' outputs to project root hippocampus.exe
- ERROR: undefined: app.ContextWriter

### Architecture (2 patterns)

- ARCHITECTURE UPGRADE SESSION (2026-03-20): Major improvements to hippocampus quality:
- ARCHITECTURE UPGRADE SESSION (2026-03-20): Transformed hippocampus from memory store to learning system. Changes:

### General (2 patterns)

- ERROR: LLM reranking with 0.6 LLM weight caused false negatives — relevant query 'how does consolidation work in hippocampus' was rejected as irrelevant
- ERROR: panic: runtime error: index out of range [3] with length 3

### Go (2 patterns)

- ERROR: goroutine leak: http.Client without timeout causes goroutine to hang forever on slow responses
- CRITICAL BUG: Production crash in hippocampus MCP server. Fatal panic in goroutine during concurrent recall — data race on working memory map. Root cause: missing mutex lock in WorkingMemory.Snapsho...

## MCP Integration

This project uses Hippocampus MOS for persistent memory.
- `mos_init` — call at session start with workspace path
- `mos_learn_error` — call on ANY error
- `mos_recall` — deep search across all memories
- `mos_remember` — store decisions, patterns, gotchas
- `mos_session_end` — call at session end with summary

<!-- HIPPOCAMPUS:END -->
