# Hippocampus MOS

> Persistent memory for AI coding agents. Remembers every bug, decision, and pattern across sessions. Error Prevention Rate: **100%**.

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev)
[![MCP Tools](https://img.shields.io/badge/MCP_tools-35-5C6BC0)](https://modelcontextprotocol.io)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

---

## The Problem

Your AI coding agent fixes a nil pointer panic on Monday. On Wednesday, it introduces the exact same bug. Every session starts from zero — no memory of past errors, architectural decisions, or hard-won context.

**Hippocampus fixes this.** It runs as a local MCP server that gives your agent persistent memory across sessions.

### Benchmark Results (52 test scenarios)

| Metric | Score |
|--------|-------|
| Error Prevention | **100%** (12/12 — every known bug pattern caught) |
| Knowledge Recall | **90%** (9/10) |
| Semantic Paraphrase Match | **92%** (11/12) |
| Cross-Language (RU query, EN memory) | **66% similarity** |
| Recall | **94.1%** |
| F1 Score | **0.762** |
| vs Random Baseline | **14.1x improvement** |
| Mean Latency | **91ms** |

---

## Quick Start

### Option A: Docker (recommended — one command)

```bash
git clone https://github.com/samj6178/hippocampus.git
cd hippocampus
docker compose up -d
```

This starts **Ollama** (auto-pulls embedding model) + **Hippocampus** (SQLite, MCP + REST). Done.

### Option B: Standalone binary (zero dependencies)

```bash
git clone https://github.com/samj6178/hippocampus.git
cd hippocampus
go build -o bin/hippocampus ./cmd/hippocampus/
./bin/hippocampus -config config.json
```

Works immediately in BM25 mode (keyword search). For semantic search, start Ollama separately:

```bash
ollama serve && ollama pull nomic-embed-text
```

### Connect to Claude Code

Add to `~/.claude/.mcp.json`:

```json
{
  "mcpServers": {
    "hippocampus": {
      "command": "/path/to/hippocampus/bin/hippocampus",
      "args": ["-config", "/path/to/hippocampus/config.json"]
    }
  }
}
```

### Connect to Cursor

Add to `.cursor/mcp.json` (same format as above).

---

## How It Works

### 1. Error Prevention Pipeline

```
error occurs → mos_learn_error → structured pattern stored
                                        │
                                        ▼
                              consolidation clusters similar errors
                                        │
                                        ▼
                              prevention rule generated (WHEN/WATCH/DO)
                                        │
                                        ▼
                              next session: WARNING before touching that code
                                        │
                                        ▼
                              session end: git diff verifies the warning worked
```

**Example:** Agent creates `&http.Client{}` without Timeout. Hippocampus:
- Stores the error with root cause and fix
- Generates rule: "WHEN creating http.Client, WATCH for missing Timeout, DO set 30s timeout"
- Next session: agent sees the warning before writing HTTP code
- Session end: verifies the anti-pattern is absent from the diff

### 2. Four-Tier Memory (inspired by neuroscience)

| Tier | What | Lifetime |
|------|------|----------|
| **Working** | Current session context | Session |
| **Episodic** | Specific events, errors, decisions | Permanent |
| **Semantic** | Consolidated knowledge, facts, rules | Permanent |
| **Procedural** | Workflows learned from outcomes | Permanent |

### 3. Temporal Knowledge Graph

Track facts that change over time:

```
mos_kg_add("auth_service", "uses", "jwt")
# ... months later, after migration:
mos_kg_invalidate("auth_service", "uses", "jwt")
mos_kg_add("auth_service", "uses", "session_tokens")

# Query state at any point in time:
mos_kg_query("auth_service", as_of="2026-03-15")  → uses jwt
mos_kg_query("auth_service")                       → uses session_tokens
mos_kg_timeline("auth_service")                    → full history
```

### 4. Session Continuity

```
Session 1:
  mos_session_end(summary="Fixed recall bug", next_steps="Add room filter tests")

Session 2:
  mos_init → auto_context includes:
    ## Next Steps (from previous session)
    - Add room filter tests
    ## Known Pitfalls (DO NOT REPEAT)
    - ERROR: filterWeakCandidates nil pointer when embedding is nil
    ## Recent Sessions
    - [2h ago] Fixed recall bug...
```

### 5. Hybrid Retrieval

Recall uses **Reciprocal Rank Fusion** combining:
- **Vector search** (cosine similarity via Ollama embeddings)
- **BM25 full-text search** (SQLite FTS5)
- **Keyword overlap scoring**
- **Recency decay** (recent memories weighted higher)
- **Importance scoring** (errors and decisions weighted higher)

Cross-language: Russian queries find English memories (and vice versa) via embedding similarity.

---

## Full Automation (recommended)

Add to `~/.claude/settings.json` — Hippocampus runs fully automatically:

```json
{
  "hooks": {
    "SessionStart": [{
      "hooks": [{
        "type": "command",
        "command": "echo '{\"hookSpecificOutput\":{\"additionalContext\":\"Call mos_init with the current workspace path NOW.\"}}'",
        "statusMessage": "Hippocampus: loading memory"
      }]
    }],
    "PostToolUseFailure": [{
      "hooks": [{
        "type": "command",
        "command": "echo '{\"hookSpecificOutput\":{\"additionalContext\":\"A tool failed. Call mos_learn_error with the error, root cause, and fix.\"}}'",
        "statusMessage": "Hippocampus: capturing error"
      }]
    }],
    "Stop": [{
      "hooks": [{
        "type": "command",
        "command": "echo '{\"hookSpecificOutput\":{\"additionalContext\":\"Session ending. Call mos_session_end with summary and next_steps.\"}}'",
        "statusMessage": "Hippocampus: saving session"
      }]
    }]
  }
}
```

| Hook | Trigger | Effect |
|------|---------|--------|
| SessionStart | New conversation | Loads context, pitfalls, next steps |
| PostToolUseFailure | Any error | Captures bug for prevention |
| Stop | Session ends | Saves summary for next session |

---

## Architecture

Single binary, clean architecture. Dependencies flow inward: `adapter -> app -> domain`.

```
cmd/hippocampus/          entry point, DI container
internal/
  domain/                 entities, interfaces (zero deps)
  app/                    services (encode, recall, consolidate, KG, mining...)
  adapter/
    mcp/                  MCP stdio server (35 tools)
    rest/                 REST API + embedded web dashboard
    llm/                  switchable LLM provider
  embedding/              Ollama/OpenAI-compatible embeddings
  repo/
    sqlite/               SQLite + FTS5 (default, zero-dep)
    (postgres)            PostgreSQL/pgvector (optional)
  memory/                 in-process working memory
  pkg/                    contenthash, roomclass, vecutil
```

**Storage:** SQLite by default (zero external dependencies). PostgreSQL/pgvector supported via config switch.

**Embeddings:** Ollama `nomic-embed-text` (768d, local, free). Any OpenAI-compatible API works (Jina, Voyage, OpenAI).

**LLM:** Agent-delegated by default — your Claude/GPT handles rule generation via two-phase delegation. No local model required.

---

## 35 MCP Tools

### Session Lifecycle
| Tool | Description |
|------|-------------|
| `mos_init` | Initialize for workspace. Auto-detects project. Call first. |
| `mos_session_end` | Save summary + next_steps. Triggers consolidation and prevention analysis. |

### Core Memory
| Tool | Description |
|------|-------------|
| `mos_remember` | Store a fact/decision/pattern. Auto-classified by room. Content-hash dedup. |
| `mos_recall` | Hybrid retrieval across all tiers. Token budget control. Room filter. |
| `mos_learn_error` | Capture error with root cause, fix, prevention. High-importance. |
| `mos_file_context` | Get relevant memories before editing a file. |
| `mos_feedback` | Rate recall usefulness. Adjusts importance scores. |

### Knowledge Graph
| Tool | Description |
|------|-------------|
| `mos_kg_add` | Add temporal fact (subject, predicate, object). |
| `mos_kg_query` | Query facts. Supports point-in-time `as_of` queries. |
| `mos_kg_invalidate` | Soft-delete: fact becomes historical, not removed. |
| `mos_kg_timeline` | Full chronological history of an entity. |

### Learning
| Tool | Description |
|------|-------------|
| `mos_consolidate` | Cluster episodic memories into semantic rules. |
| `mos_predict` / `mos_resolve` | Track prediction accuracy. Surprise = stronger encoding. |
| `mos_track_outcome` | Report procedure success/failure. |
| `mos_mine_conversations` | Extract decisions and errors from past Claude Code sessions. |

### Analysis
| Tool | Description |
|------|-------------|
| `mos_health` | System status, embedding model, memory counts. |
| `mos_benchmark` | 52-scenario reproducible evaluation with precision/recall/F1. |
| `mos_meta` | Metacognition: calibration, gaps, recommendations. |
| `mos_evaluate` | Formal eval: recall precision, Brier score, learning curve. |

### Research
| Tool | Description |
|------|-------------|
| `mos_research` | Search arXiv, GitHub, HN. Synthesize findings. |
| `mos_curate` | Deep research across 6 domain agents. |
| `mos_fuse` | Combine stored memory with web search (Dempster-Shafer). |
| `mos_analogize` | Find cross-project structural analogies. |

### Projects
| Tool | Description |
|------|-------------|
| `mos_create_project` / `mos_list_projects` / `mos_switch_project` | Multi-project memory isolation. |
| `mos_study_project` | Deep-read README, configs, docs into memory. |
| `mos_ingest_codebase` | AST-based extraction (Go, TS, Python, Rust, C++, Java, Ruby, C#). |

---

## A/B Test: Warning Prevention

`mos_ab_test` runs 12 coding scenarios with known anti-patterns:

```
Treatment (warnings ON):   0 bugs, 12 clean    → 100% prevention
Control   (warnings OFF):  12 bugs, 0 clean    → 0% prevention
Lift: +100%
```

Categories: architecture, concurrency, error handling, injection, protocol, resource leaks.

---

## Configuration

```json
{
  "database": {
    "driver": "sqlite",
    "sqlite_path": "hippocampus.db"
  },
  "openai": {
    "mode": "cloud",
    "base_url": "http://localhost:11434/v1",
    "model": "nomic-embed-text",
    "dimensions": 768
  },
  "memory": {
    "gate_threshold": 0.3,
    "recall": {
      "absolute_floor": 0.30,
      "entropy_best": 0.45,
      "kw_check_threshold": 0.60
    }
  },
  "llm": {
    "provider": "none"
  }
}
```

| Mode | Embeddings | LLM | Setup |
|------|-----------|-----|-------|
| **Zero-dep** | None (BM25 only) | Agent-delegated | Just run the binary |
| **Local** | Ollama nomic-embed-text | Agent-delegated | `docker compose up` |
| **Cloud** | OpenAI/Jina/Voyage API | Agent-delegated | Set API key in config |

---

## Observability

- **Health:** `GET /api/v1/health/ready`
- **Metrics:** `GET /metrics` (Prometheus) — recall hit rate, prevention stats, embedding latency
- **Dashboard:** `http://localhost:8080` — memory browser, project switcher, recall search
- **Audit log:** `audit.jsonl` — JSONL of all write operations

---

## Development

```bash
go build ./...           # build
go test ./... -count=1   # unit tests (10 packages)
go vet ./...             # static analysis
```

---

## License

MIT
