---
name: MOS Architecture Plan
overview: "Hippocampus: a Memory Operating System defining Memory Algebra (8 formal operations), Cognitive State Protocol, Predictive Memory Engine, and multi-resolution temporal memory. Go + TimescaleDB + pgvector + OpenAI embeddings. Targets: Anthropic-grade research contribution + production system + arXiv paper."
todos:
  - id: week1-skeleton
    content: "Week 1: Go project skeleton, go.mod, domain types (Memory Algebra primitives), project structure"
    status: completed
  - id: week1-schemas
    content: "Week 1: TimescaleDB schemas + migrations — all memory tiers + projects + causal_links + emotional_tags + metacognitive_log + predictions"
    status: completed
  - id: week1-embedding
    content: "Week 1: OpenAI embedding provider with LRU cache + embedding versioning"
    status: completed
  - id: week2-working-mem
    content: "Week 2: Working memory ring buffer (lock-free, <100us) with importance-weighted eviction"
    status: completed
  - id: week2-basic-api
    content: "Week 2: Basic ENCODE + RECALL services + MCP server (mos_recall, mos_remember, mos_switch_project)"
    status: completed
  - id: week2-docker
    content: "Week 2: Docker Compose (TimescaleDB + hippocampus binary) + cold start ingestion"
    status: completed
  - id: week3-importance
    content: "Week 3: Full importance scoring I(m,t,q) + submodular greedy context assembly + emotional tagging"
    status: pending
  - id: week3-projects
    content: "Week 3: Multi-project namespacing (switch, list, isolation, cross-project promotion)"
    status: pending
  - id: week4-tiers
    content: "Week 4: Full episodic + semantic + procedural + causal tier implementation"
    status: pending
  - id: week4-predict
    content: "Week 4: Predictive Memory Engine (PREDICT + SURPRISE + delta encoding)"
    status: pending
  - id: week5-consolidation
    content: "Week 5: Consolidation engine with selective replay (surprise-weighted) + multi-resolution compression"
    status: pending
  - id: week5-metacognition
    content: "Week 5: Meta-cognitive layer (confidence calibration, gap detection, strategy evaluation)"
    status: pending
  - id: week6-sleep
    content: "Week 6: Offline 'dreaming' mode (contradiction detection, hypothesis generation, pre-computation)"
    status: pending
  - id: week6-policies
    content: "Week 6: TimescaleDB compression + retention + decay policies + analogical retrieval"
    status: pending
  - id: week7-grpc-rest
    content: "Week 7: gRPC API + REST gateway + CSP protocol implementation + protobuf"
    status: pending
  - id: week7-tests
    content: "Week 7: Integration tests (testcontainers) + benchmarks vs Mem0/Letta"
    status: pending
  - id: week8-paper
    content: "Week 8: arXiv paper — Memory Algebra formalization, CLS theory mapping, benchmarks, safety implications"
    status: pending
  - id: week8-readme-launch
    content: "Week 8: README + Show HN preparation + demo video"
    status: pending
isProject: false
---

# Hippocampus: Memory Operating System for AI Agents

## Vision

Hippocampus is not a memory library. It is an **operating system for cognitive state** — defining a formal algebra of memory operations, a protocol for cognitive state exchange between agents (AI and human), and a predictive memory engine grounded in computational neuroscience. The goal: become the foundational infrastructure for AI memory the way TCP/IP became foundational for data communication and relational algebra became foundational for databases.

## Core Insight

Industrial sensor pipeline (event -> store -> aggregate -> alert) is isomorphic to memory pipeline (experience -> encode -> store -> consolidate -> recall). TimescaleDB's `time_bucket()`, continuous aggregates, and retention policies map directly to cognitive decay, consolidation, and forgetting.

---

## Architecture Overview

```mermaid
graph TB
    subgraph user [User]
        Dev["Developer"]
    end

    subgraph mos [MOS - Cognitive Operating System - Single Go Binary]
        subgraph transport [Transport Layer]
            MCP["MCP Server :3000"]
            GRPC["gRPC Server :9090"]
            REST["REST API :8080"]
        end

        subgraph intake [1 - Thalamic Gate]
            TG["Relevance Filter: novelty * info_gain > threshold"]
        end

        subgraph understand [2 - Understanding]
            CB["Context Builder - Global Workspace"]
            PE["Predictive Engine - PREDICT + SURPRISE"]
        end

        subgraph plan_layer [3 - Prefrontal Cortex - Phase 2]
            TD["Task Decomposer"]
            AR["Agent Registry"]
            SCHED["Scheduler - dependencies + parallelism"]
        end

        subgraph memory_layer [4 - Memory Tiers]
            WM["Working Memory - ring buffer"]
            EM["Episodic - hypertable"]
            SM["Semantic - knowledge graph"]
            PM["Procedural - action patterns"]
            CL["Causal - cause/effect links"]
        end

        subgraph background [5 - Background Workers]
            CONS["Consolidation - every 6h"]
            IMMUNE["Immune System - every 12h"]
            CURIOUS["Curiosity Engine - every 24h"]
            DMN["Idle Processor - when no tasks"]
        end

        subgraph meta_layer [6 - Meta-Cognition]
            MC["Confidence Calibration"]
            ET["Emotional Tagging"]
        end
    end

    subgraph agents [Agent Pool]
        Cursor["Cursor - IDE editing"]
        CC["Claude Code - terminal + reasoning"]
        Custom["Custom Agents"]
    end

    subgraph storage [Storage]
        TS["TimescaleDB + pgvector"]
        OAI["OpenAI Embeddings"]
    end

    Dev --> MCP
    Dev --> REST
    MCP --> TG
    GRPC --> TG
    REST --> TG
    TG --> CB
    CB --> PE
    PE --> TD
    TD --> AR
    AR --> SCHED
    SCHED --> Cursor
    SCHED --> CC
    SCHED --> Custom
    Cursor --> memory_layer
    CC --> memory_layer
    Custom --> memory_layer
    CB --> memory_layer
    memory_layer --> TS
    memory_layer --> OAI
    MC --> CB
    ET --> CONS
    CONS --> SM
```



**Key principle**: Agents never talk directly to each other. All communication flows through MOS shared memory. MOS is the sole mediator — enabling audit, conflict prevention, and safety control.

---

## FOUNDATIONAL THEORY: Memory Algebra

Hippocampus defines 8 primitive operations — the **minimum complete set** for a cognitive memory system. Any future implementation (quantum, neuromorphic, biological-digital hybrid) will implement these operations, just as any database implements relational algebra.


| Operation   | Signature                             | Cognitive Analogue                     |
| ----------- | ------------------------------------- | -------------------------------------- |
| ENCODE      | experience -> memory_item             | Sensory input -> hippocampal trace     |
| RECALL      | (query, budget) -> context            | Retrieval with attention constraints   |
| CONSOLIDATE | episodes[] -> semantic_fact           | Sleep replay -> cortical transfer      |
| FORGET      | (item, reason) -> void                | Active forgetting (interference-based) |
| PREDICT     | task -> expected_outcome              | Predictive processing (Friston)        |
| SURPRISE    | (prediction, actual) -> delta         | Prediction error signal                |
| ANALOGIZE   | (structure, target_domain) -> mapping | Cross-domain structural transfer       |
| META        | memory_system -> quality_assessment   | Metacognitive monitoring               |


**Formal properties**:

- CONSOLIDATE is associative: `CONSOLIDATE(CONSOLIDATE(A, B), C) = CONSOLIDATE(A, B, C)`
- FORGET is irreversible by design: there is no UNFORGET (safety requirement)
- RECALL is submodular: marginal value of adding one more memory to context is diminishing
- PREDICT + SURPRISE = learning signal: `ENCODE(SURPRISE(PREDICT(T), actual))` = store only what was unexpected (information-theoretically optimal by Shannon)

---

## FOUNDATIONAL THEORY: Cognitive State Protocol (CSP)

The universal interface for cognitive state exchange between any agents (AI, human, hybrid). Analogous to TCP/IP for data and HTTP for information.

```
CSP Message = {
    sender:         agent_id,
    type:           EXPERIENCE | FACT | QUERY | PREDICTION | CORRECTION,
    content:        payload,
    confidence:     float,           // calibrated, not self-reported
    evidence:       [source_ids],    // provenance chain
    causal_context: {
        cause_of:   [memory_ids],
        caused_by:  [memory_ids]
    },
    temporal: {
        valid_from:   timestamp,
        valid_until:  timestamp | NULL,  // NULL = permanent
        supersedes:   [memory_ids]       // what this replaces
    }
}
```

Any system implementing CSP can read/write to shared memory. Human notes, AI experiences, and hybrid interactions all flow through the same protocol.

---

## FOUNDATIONAL THEORY: Predictive Memory Engine

Instead of passive store-and-retrieve, Hippocampus actively predicts and learns from prediction errors:

```
For each task T:
  1. predicted_outcome = PREDICT(T)        // based on similar past episodes
  2. actual_outcome = execute(T)           // agent performs the task
  3. delta = SURPRISE(predicted, actual)   // compute prediction error
  4. ENCODE(delta)                         // store ONLY what was unexpected
  5. if |delta| > 2*sigma:                 // if highly surprising
       mark for priority consolidation
       update emotional_tag = 'surprise'
  6. Update PREDICT model for this task type
```

This is information-theoretically optimal: by Shannon's theory, storing prediction errors (surprises) is the minimum description length of new knowledge. It is also neurologically grounded: the hippocampus preferentially encodes prediction errors (Kumaran & Maguire, 2006).

---

## FOUNDATIONAL THEORY: Multi-Resolution Temporal Memory

Memory stored at decreasing resolution over time, like a video codec with keyframes and deltas:

- **Full resolution** (< 7 days): complete episodic memory with all details
- **Medium resolution** (7-30 days): LLM-generated summary preserving key decisions and outcomes
- **Low resolution** (30-180 days): extracted facts + causal links, source episodes compressed
- **Minimal** (> 180 days): embedding + one-sentence summary + importance score

Implementation: TimescaleDB continuous aggregates at each resolution level, with compression policies that automatically downsample over time. Infinite temporal range, finite storage.

---

## FOUNDATIONAL THEORY: Emotional Tagging

Formal (non-anthropomorphic) emotional signals that affect consolidation priority and decay rate:


| Signal      | Detection Rule                                             | Effect                                        |
| ----------- | ---------------------------------------------------------- | --------------------------------------------- |
| frustration | user_corrections > 3 in one task                           | 2x consolidation priority, stored as warning  |
| success     | task completed first try, user confirmed                   | reinforces procedural memory for this pattern |
| surprise    | prediction_error > 2*sigma                                 | priority consolidation, high importance       |
| novelty     | max cosine similarity with existing memories < 0.3         | creates new cluster seed in semantic memory   |
| danger      | user rolled back changes / data loss / production incident | maximum importance, never auto-decayed        |


---

## Improvement 1: Multi-Project Memory Namespacing

**Original spec weakness**: No concept of project isolation. All memories in one flat space.

**Improved model**: Two-level namespace — Global + Project-specific:

```
Memory Space
├── global (always active)
│   ├── Your coding patterns, style, preferences
│   ├── Architectural decisions that apply everywhere  
│   ├── Cross-project learnings
│   └── Tool/framework knowledge (Go idioms, SQL patterns)
│
├── project:energy-monitor
│   ├── episodic: sessions, bugs fixed, refactoring decisions
│   ├── semantic: schemas, device UUIDs, MQTT topics, codebase map
│   └── procedural: deploy commands, debug sequences
│
├── project:pikk
│   ├── episodic: Kafka consumer work, data management sessions
│   ├── semantic: topic structure, MinIO layout, Hive schemas  
│   └── procedural: test/deploy patterns
│
└── project:hippocampus (MOS itself)
    ├── episodic: design decisions, implementation sessions
    ├── semantic: own architecture, API contracts
    └── procedural: build/test/deploy
```

**Implementation**: Each memory item has `project_id` (nullable = global). Context assembly queries: `WHERE project_id = $active OR project_id IS NULL`. Switching projects = one API call changing active `project_id`.

**Cross-project promotion**: When a pattern proves useful across 2+ projects, an agent (or user) can promote it from project-specific to global. Example: "batch COPY protocol for TimescaleDB" started in Energy Monitor, becomes global knowledge.

---

## Improvement 2: Context Assembly as Submodular Maximization (not Knapsack)

**Original spec weakness**: The 0/1 Knapsack formulation ignores information redundancy. If m1 and m2 contain overlapping information, selecting both wastes token budget.

**Improved formulation**:

```
maximize: F(S)
subject to: cost(S) <= B
where F(S) = Σ_i I(m_i) - λ·Σ_{i<j} overlap(m_i, m_j)
```

F is submodular (diminishing returns). The greedy algorithm gives (1 - 1/e) approximation in O(n*k) time, where k = selected items. For n=1000 candidates, k=50 selected: ~50K operations, well under 5ms.

**Practical algorithm**:

1. Score all candidates with I(m, t, q)
2. Greedy selection: pick highest-scoring item, then re-score remaining items penalizing overlap with already-selected (via embedding cosine similarity)
3. Stop when budget exhausted

---

## Improvement 3: Simplified Importance Scoring (v1)

**Original spec weakness**: 5 weighted factors with gradient descent to learn weights is chicken-and-egg — you need retrieval feedback to learn, but you don't have feedback until the system is running.

**v1 formula (3 factors, manually tuned)**:

```
I(m, t, q) = 0.5·S(m,q) + 0.3·R(m,t) + 0.2·E(m)

S = semantic similarity (cosine of embeddings)
R = recency = exp(-λ(t - t_last_access)), λ = ln(2)/τ_half
E = explicit importance (user/agent annotation, default 0.5)
```

**v2 (after 1000+ retrievals)**: Add F (frequency) and C (graph centrality), learn weights via Bayesian optimization on retrieval quality feedback.

---

## Improvement 4: Concrete Procedural Memory for AI Agents

**Original spec weakness**: "Action sequences, reinforcement-updated" is vague for LLM agents.

**For AI agents, procedural memory = three concrete things**:

1. **Tool-use patterns**: "For Energy Monitor deploy: build -> scp -> systemctl stop -> cp -> systemctl start"
2. **Successful prompt templates**: "When user asks about TimescaleDB performance, include EXPLAIN ANALYZE results"
3. **Reasoning chains that worked**: "To debug MQTT data loss: check broker -> check topic subscription -> check parse function -> check SQL column names"

**Schema**: Stored as task-type + step sequence + success rate. Retrieved by task-type similarity.

---

## Improvement 5: Cold Start Solution

**Original spec gap**: No pathway for initial knowledge population.

**Three cold-start strategies**:

1. **Codebase ingestion**: Parse Go files, extract structs/functions/comments, store as semantic memory with embeddings
2. **Document ingestion**: Parse markdown/SQL files (like your `.cursor/rules/`), chunk and embed
3. **Session replay**: Import existing Cursor chat histories as episodic memories

For your case: MOS v0.1 ingests your `.cursor/rules/` (go_architecture_guide, universal_architecture_standard, session_summary) as initial semantic memory. Immediate value on day 1.

---

## Data Architecture

### Core Tables (TimescaleDB + pgvector)

**projects** — namespace registry:

- `id UUID PRIMARY KEY`
- `slug TEXT UNIQUE NOT NULL` (e.g., "energy-monitor", "pikk")
- `display_name TEXT`
- `metadata JSONB`
- `created_at TIMESTAMPTZ`

**episodic_memory** — hypertable, time-partitioned:

- `id UUID`
- `time TIMESTAMPTZ NOT NULL` (partition key)
- `project_id UUID REFERENCES projects` (NULL = global)
- `agent_id TEXT NOT NULL`
- `session_id UUID`
- `content TEXT NOT NULL`
- `summary TEXT` (compressed version for partial retrieval)
- `embedding vector(1536) NOT NULL`
- `importance FLOAT DEFAULT 0.5`
- `access_count INT DEFAULT 0`
- `last_accessed TIMESTAMPTZ`
- `token_count INT NOT NULL`
- `metadata JSONB`
- TimescaleDB: `SELECT create_hypertable('episodic_memory', 'time', chunk_time_interval => INTERVAL '1 week')`
- Indexes: HNSW on embedding (`lists=100, m=16, ef_construction=200`), BRIN on time, btree on project_id

**semantic_memory** — knowledge graph nodes:

- `id UUID PRIMARY KEY`
- `project_id UUID REFERENCES projects` (NULL = global)
- `entity_type TEXT NOT NULL` (fact, concept, pattern, rule)
- `content TEXT NOT NULL`
- `summary TEXT`
- `embedding vector(1536) NOT NULL`
- `importance FLOAT DEFAULT 0.5`
- `confidence FLOAT DEFAULT 1.0`
- `source_episodes UUID[]` (links to episodic memories that generated this)
- `access_count INT DEFAULT 0`
- `last_accessed TIMESTAMPTZ`
- `token_count INT NOT NULL`
- `metadata JSONB`
- `created_at TIMESTAMPTZ`
- `updated_at TIMESTAMPTZ`

**semantic_edges** — knowledge graph relations:

- `id UUID PRIMARY KEY`
- `source_id UUID REFERENCES semantic_memory`
- `target_id UUID REFERENCES semantic_memory`
- `relation TEXT NOT NULL` (e.g., "depends_on", "implements", "contradicts", "derived_from")
- `weight FLOAT DEFAULT 1.0`
- `metadata JSONB`

**procedural_memory** — versioned action patterns:

- `id UUID PRIMARY KEY`
- `project_id UUID REFERENCES projects` (NULL = global)
- `task_type TEXT NOT NULL`
- `description TEXT NOT NULL`
- `steps JSONB NOT NULL` (ordered array of action descriptions)
- `embedding vector(1536) NOT NULL`
- `success_count INT DEFAULT 0`
- `failure_count INT DEFAULT 0`
- `success_rate FLOAT GENERATED ALWAYS AS (success_count::float / GREATEST(success_count + failure_count, 1)) STORED`
- `last_used TIMESTAMPTZ`
- `version INT DEFAULT 1`
- `metadata JSONB`

**causal_links** — causal relationships between memories:

- `id UUID PRIMARY KEY`
- `cause_id UUID NOT NULL` (references any memory tier via metadata)
- `effect_id UUID NOT NULL`
- `cause_tier TEXT NOT NULL` (episodic, semantic, procedural)
- `effect_tier TEXT NOT NULL`
- `relation_type TEXT NOT NULL` (caused, prevented, enabled, degraded, required)
- `confidence FLOAT DEFAULT 0.5`
- `evidence_episodes UUID[]` (episodes that support this causal link)
- `counter_evidence UUID[]` (episodes that contradict)
- `created_at TIMESTAMPTZ`
- `updated_at TIMESTAMPTZ`

**predictions** — prediction log for Predictive Memory Engine:

- `id UUID PRIMARY KEY`
- `task_description TEXT NOT NULL`
- `task_embedding vector(1536) NOT NULL`
- `predicted_outcome TEXT NOT NULL`
- `actual_outcome TEXT` (filled after task completion)
- `prediction_error FLOAT` (computed: embedding distance between predicted and actual)
- `domain TEXT NOT NULL` (go_performance, architecture, sql_optimization, debugging, etc.)
- `agent_id TEXT NOT NULL`
- `project_id UUID REFERENCES projects`
- `created_at TIMESTAMPTZ`
- `resolved_at TIMESTAMPTZ`

**metacognitive_log** — self-assessment and calibration:

- `id UUID PRIMARY KEY`
- `domain TEXT NOT NULL`
- `predicted_confidence FLOAT NOT NULL` (what the agent said its confidence was)
- `actual_accuracy FLOAT` (retrospectively computed: was it right?)
- `calibration_offset FLOAT` (predicted - actual, averaged over domain)
- `strategy_used TEXT` (which retrieval/reasoning strategy was used)
- `strategy_outcome TEXT` (success, partial, failure)
- `agent_id TEXT NOT NULL`
- `created_at TIMESTAMPTZ`
- Enables: "in domain X, my confidence is systematically 15% too high"

**emotional_tags** — affective signals on memory items:

- `memory_id UUID NOT NULL`
- `memory_tier TEXT NOT NULL` (episodic, semantic, procedural)
- `valence TEXT NOT NULL` (success, frustration, surprise, novelty, danger)
- `intensity FLOAT NOT NULL` (0.0 to 1.0)
- `signals JSONB NOT NULL` (detection evidence: {user_corrections: 5, retry_count: 3})
- `created_at TIMESTAMPTZ`
- PRIMARY KEY (memory_id, valence)

**working_memory** — NOT a table. In-process Go ring buffer:

```go
type WorkingMemory struct {
    mu       sync.RWMutex
    items    []*MemoryItem     // fixed-size ring buffer
    capacity int               // e.g., 50 items
    head     int
    size     int
    projectID string           // current active project
}
```

Target: <100μs read/write. No database round-trip.

### TimescaleDB Policies

- **Compression**: episodic_memory after 7 days (`ALTER TABLE episodic_memory SET (timescaledb.compress, ...); SELECT add_compression_policy('episodic_memory', INTERVAL '7 days');`)
- **Retention**: Raw episodic data dropped after 90 days; consolidated summaries survive in semantic_memory
- **Continuous aggregate**: Session-level summaries auto-generated from episodic chunks
- **Decay implementation**: `importance = importance * exp(-λ * EXTRACT(EPOCH FROM (NOW() - last_accessed)) / 86400)` computed at query time, not stored (avoids full-table updates)

---

## Go Project Structure

```
hippocampus/
├── cmd/
│   └── hippocampus/
│       └── main.go                       # Bootstrap: config -> DI -> start (<=150 lines)
│
├── internal/
│   ├── domain/                           # Pure domain types — Memory Algebra primitives
│   │   ├── memory.go                     # MemoryItem, MemoryTier, ImportanceScore
│   │   ├── algebra.go                    # ENCODE, RECALL, CONSOLIDATE, FORGET, PREDICT, SURPRISE, ANALOGIZE, META
│   │   ├── project.go                    # Project, ProjectScope
│   │   ├── context.go                    # AssembledContext, TokenBudget
│   │   ├── causal.go                     # CausalLink, CausalGraph
│   │   ├── emotion.go                    # EmotionalTag, Valence, Intensity
│   │   ├── prediction.go                # Prediction, PredictionError, Delta
│   │   ├── csp.go                        # Cognitive State Protocol message types
│   │   └── errors.go                     # Domain errors
│   │
│   ├── app/                              # Use cases / services
│   │   ├── recall_service.go             # Context assembly (submodular greedy)
│   │   ├── remember_service.go           # Memory encoding + emotional tagging
│   │   ├── consolidation_service.go      # Background: episodic -> semantic (selective replay)
│   │   ├── prediction_service.go         # PREDICT + SURPRISE + delta encoding
│   │   ├── metacognition_service.go      # Confidence calibration, gap detection, strategy evaluation
│   │   ├── dreaming_service.go           # Offline processing: contradiction detection, hypothesis generation
│   │   ├── analogy_service.go            # Cross-domain structural transfer
│   │   ├── project_service.go            # Project CRUD + switching + cross-project promotion
│   │   ├── ingestion_service.go          # Cold start: ingest docs/code/chat histories
│   │   ├── health_service.go             # Memory health metrics
│   │   ├── gate_service.go              # Thalamic gate: should this be encoded? (Phase 2)
│   │   ├── orchestrator_service.go      # Task decomposition + agent dispatch (Phase 2)
│   │   ├── curiosity_service.go         # Knowledge gap detection + question gen (Phase 2)
│   │   └── immune_service.go            # Memory integrity + contradiction scan (Phase 2)
│   │
│   ├── adapter/
│   │   ├── mcp/                          # MCP server (for Cursor, Claude Code)
│   │   │   ├── server.go
│   │   │   └── tools.go                  # MCP tool definitions
│   │   ├── grpc/                         # gRPC server (for custom agents)
│   │   │   ├── server.go
│   │   │   └── handler.go
│   │   └── rest/                         # REST gateway
│   │       ├── router.go
│   │       └── handler.go
│   │
│   ├── repo/                             # Database access (SQL ONLY here)
│   │   ├── episodic_repo.go
│   │   ├── semantic_repo.go
│   │   ├── procedural_repo.go
│   │   ├── causal_repo.go
│   │   ├── prediction_repo.go
│   │   ├── metacognition_repo.go
│   │   ├── project_repo.go
│   │   └── postgres.go                   # Connection pool, migrations
│   │
│   ├── embedding/                        # Embedding provider abstraction
│   │   ├── provider.go                   # Interface
│   │   ├── openai.go                     # OpenAI implementation
│   │   ├── cache.go                      # In-process LRU cache
│   │   └── versioning.go                 # Embedding model version tracking
│   │
│   ├── memory/                           # Memory-tier specific logic
│   │   ├── working.go                    # Ring buffer with importance-weighted eviction
│   │   ├── importance.go                 # I(m,t,q) scoring with emotional modulation
│   │   ├── assembly.go                   # Submodular greedy context assembly
│   │   ├── decay.go                      # Exponential decay functions
│   │   ├── compression.go               # Multi-resolution compression (full -> summary -> fact -> minimal)
│   │   └── emotional.go                  # Emotional signal detection and tagging
│   │
│   └── pkg/
│       ├── config/
│       ├── logger/
│       └── tokenizer/                    # Token counting (tiktoken-go)
│
├── migrations/
│   ├── 001_init_extensions.up.sql        # pgvector, timescaledb
│   ├── 002_projects.up.sql
│   ├── 003_episodic_memory.up.sql
│   ├── 004_semantic_memory.up.sql
│   ├── 005_procedural_memory.up.sql
│   ├── 006_causal_links.up.sql
│   ├── 007_predictions.up.sql
│   ├── 008_metacognitive_log.up.sql
│   ├── 009_emotional_tags.up.sql
│   └── 010_indexes_and_policies.up.sql
│
├── proto/
│   └── mos/
│       └── v1/
│           ├── memory.proto              # Core memory operations
│           └── csp.proto                 # Cognitive State Protocol
│
├── docker-compose.yml                    # TimescaleDB + pgvector + MOS
├── Makefile
├── go.mod
└── README.md
```

---

## API Design

### MCP Tools (for Cursor / Claude Code)

These are the tools I (Claude) would call — mapped to Memory Algebra operations:

- `**mos_recall**` (RECALL) — assemble context for a task
  - Input: `{ task: string, budget_tokens: int, project?: string }`
  - Output: `{ context: string, sources: [{id, tier, relevance}], confidence: float, token_count: int }`
- `**mos_remember**` (ENCODE) — store an experience with emotional tagging
  - Input: `{ content: string, project?: string, importance?: float, outcome?: string, tags?: string[] }`
  - Output: `{ memory_id: string, tier: string, emotional_tags: string[] }`
- `**mos_predict**` (PREDICT) — get prediction for a task before executing
  - Input: `{ task: string, project?: string }`
  - Output: `{ predicted_outcome: string, confidence: float, similar_past_tasks: [{summary, outcome}] }`
- `**mos_resolve**` (SURPRISE) — report actual outcome, compute prediction error
  - Input: `{ prediction_id: string, actual_outcome: string }`
  - Output: `{ prediction_error: float, surprise_level: string, stored_as_delta: bool }`
- `**mos_switch_project**` — change active project context
  - Input: `{ project: string }`
  - Output: `{ active_project: string, memory_stats: {episodic: int, semantic: int, procedural: int, causal: int} }`
- `**mos_projects**` — list available projects
  - Output: `{ projects: [{slug, display_name, memory_count, last_active}] }`
- `**mos_forget**` (FORGET) — explicitly remove a memory (irreversible by design)
  - Input: `{ memory_id: string, reason: string }`
- `**mos_meta**` (META) — self-assessment of memory quality
  - Output: `{ calibration_by_domain: {domain: offset}, gaps_detected: string[], best_strategies: string[], total_predictions: int, accuracy: float }`
- `**mos_stats**` — memory health dashboard
  - Output: `{ total_memories, by_tier, by_project, storage_bytes, avg_recall_latency_ms, consolidation_status }`

### gRPC API (for custom agents)

Defined in `proto/mos/v1/memory.proto`:

```protobuf
service MemoryService {
  rpc Recall(RecallRequest) returns (RecallResponse);
  rpc Remember(RememberRequest) returns (RememberResponse);
  rpc Forget(ForgetRequest) returns (ForgetResponse);
  rpc Consolidate(ConsolidateRequest) returns (ConsolidateResponse);
  rpc SwitchProject(SwitchProjectRequest) returns (SwitchProjectResponse);
  rpc ListProjects(ListProjectsRequest) returns (ListProjectsResponse);
  rpc GetStats(GetStatsRequest) returns (GetStatsResponse);
  rpc Ingest(stream IngestRequest) returns (IngestResponse);
}
```

### REST Gateway

Thin proxy over gRPC. Same endpoints as `/api/v1/memory/*`. Includes OpenAI-compatible `/v1/chat/completions` proxy that transparently injects MOS context.

---

## Context Assembly Algorithm (the core of MOS)

```
function assemble_context(task, budget_tokens, project_id):
    // Phase 1: Task Analysis (< 2ms)
    task_embedding = embed(task)  // cached if recently seen
    
    // Phase 2: Parallel Retrieval (< 20ms total, all concurrent)
    goroutine 1: working_items  = working_memory.scan(task_embedding, limit=20)
    goroutine 2: episodic_items = episodic_repo.search_similar(
                     task_embedding, project_id, limit=50)
    goroutine 3: semantic_items = semantic_repo.search_similar(
                     task_embedding, project_id, limit=50)
    goroutine 4: procedural_items = procedural_repo.search_by_task_type(
                     task_embedding, project_id, limit=20)
    goroutine 5: global_items  = semantic_repo.search_similar(
                     task_embedding, project_id=NULL, limit=30)
    
    // Phase 3: Importance Scoring (< 3ms)
    candidates = merge(working, episodic, semantic, procedural, global)
    for each m in candidates:
        m.score = 0.5*cosine(m.embedding, task_embedding) 
                + 0.3*exp(-λ*(now - m.last_accessed)) 
                + 0.2*m.importance
    
    // Phase 4: Submodular Greedy Selection (< 5ms)
    selected = []
    remaining_budget = budget_tokens
    sort candidates by score DESC
    for each m in candidates:
        if m.token_count > remaining_budget: 
            // try summary version if available
            if m.summary and token_count(m.summary) <= remaining_budget:
                m = m.with_summary()
            else: continue
        // penalize if overlaps with already selected
        overlap_penalty = max(cosine(m.embedding, s.embedding) for s in selected) * 0.3
        adjusted_score = m.score - overlap_penalty
        if adjusted_score > threshold:
            selected.append(m)
            remaining_budget -= m.token_count
    
    // Phase 5: Ordering (< 1ms)
    order selected by: [semantic first, then episodic by recency, procedural last]
    
    // Phase 6: Format (< 1ms)
    return format_as_context_string(selected)
    
    // Total target: < 30ms p50, < 50ms p99
```

---

## Consolidation Engine ("Sleep")

Background worker implementing Complementary Learning Systems theory (McClelland et al., 1995):

**Phase 1: Selective Replay** (prioritize surprising experiences)

- Query episodic memories from last 24h
- Sort by: `consolidation_priority = emotional_intensity * prediction_error * (1 - times_consolidated)`
- Top 20% get replayed (re-processed for pattern extraction)
- This mirrors hippocampal replay during sleep: surprising/emotional events replayed more

**Phase 2: Cluster and Extract**

- Group replayed episodes by embedding similarity (DBSCAN, eps=0.15)
- For clusters with 3+ episodes, call LLM to extract:
  - Semantic fact (generalized knowledge)
  - Causal links (what caused what)
  - Procedural update (if a procedure was improved or disproved)

**Phase 3: Contradiction Detection**

- For each new semantic fact, check against existing semantic memory
- If cosine similarity > 0.8 but content contradicts: flag for resolution
- Resolution strategies: keep newer (if confidence higher), keep both (if different contexts), merge

**Phase 4: Multi-Resolution Compression**

- Episodes > 7 days: compress to summary (LLM-generated, ~100 tokens)
- Episodes > 30 days: compress to fact + causal links only (~30 tokens)
- Episodes > 180 days: compress to embedding + one-sentence (~10 tokens)
- Consolidated episodes: lower importance, eligible for pruning after 90 days

**Phase 5: Hypothesis Generation** (experimental)

- Identify gaps: domains with few memories but many queries
- Generate hypothetical causal links from existing patterns
- Mark as confidence=0.1 (hypothesis, not fact), to be validated

**Trigger conditions**:

- Time-based: every 6 hours (the "sleep cycle")
- Capacity-based: when episodic count for a project exceeds 1000
- On-demand: `mos.consolidate()` API call
- Surprise-triggered: when a single episode has prediction_error > 3*sigma

---

## Deployment

Single `docker-compose.yml`:

```yaml
services:
  hippocampus:
    build: .
    ports:
      - "8080:8080"    # REST
      - "9090:9090"    # gRPC  
      - "3000:3000"    # MCP (stdio or SSE)
    environment:
      - DATABASE_URL=postgres://mos:mos@timescaledb:5432/hippocampus
      - OPENAI_API_KEY=${OPENAI_API_KEY}
    depends_on:
      - timescaledb

  timescaledb:
    image: timescale/timescaledb-ha:pg16
    ports:
      - "5432:5432"
    environment:
      - POSTGRES_DB=hippocampus
      - POSTGRES_USER=mos
      - POSTGRES_PASSWORD=mos
    volumes:
      - pgdata:/var/lib/postgresql/data
```

One command: `docker-compose up -d`. Then configure MCP in Cursor.

---

## 8-Week Execution Plan

**Week 1-2: Foundation + First Working Demo**

- Go project skeleton with Memory Algebra domain types
- TimescaleDB schemas + migrations (all tables including causal_links, predictions, metacognitive_log, emotional_tags)
- Embedding provider (OpenAI API + in-process LRU cache + embedding versioning)
- Working memory ring buffer with importance-weighted eviction
- Basic ENCODE + RECALL (cosine similarity, no knapsack yet)
- Basic MCP server with `mos_recall`, `mos_remember`, `mos_switch_project` tools
- Docker Compose
- Cold start: ingest `.cursor/rules/` as initial knowledge
- **Milestone**: Cursor calls `mos_remember` and `mos_recall` via MCP, remembers context across sessions

**Week 3-4: Core Intelligence + Predictive Engine**

- Full importance scoring I(m, t, q) with emotional modulation
- Submodular greedy context assembly (with overlap penalty)
- Multi-project namespacing (switch, list, isolation, cross-project promotion)
- All memory tiers fully implemented (episodic + semantic + procedural + causal)
- Predictive Memory Engine: PREDICT + SURPRISE + delta encoding
- Emotional tagging: auto-detect frustration/success/surprise/novelty/danger
- **Milestone**: Agent predicts task outcomes, stores prediction errors, switches between project contexts

**Week 5-6: Consolidation + Meta-cognition**

- Consolidation engine with selective replay (surprise-weighted, not uniform)
- Multi-resolution temporal compression (full -> summary -> fact -> minimal)
- Meta-cognitive layer: confidence calibration per domain, gap detection, strategy evaluation
- Offline "dreaming" mode: contradiction detection, hypothesis generation, pre-compute likely contexts
- TimescaleDB compression + retention + continuous aggregate policies
- Analogical retrieval: cross-project structural similarity
- **Milestone**: System consolidates overnight, confidence is calibrated, contradictions detected

**Week 7-8: APIs + Paper + Launch**

- gRPC API + REST gateway + CSP protocol implementation
- Integration tests with real TimescaleDB (testcontainers)
- Benchmarks: MOS vs Mem0 vs Letta on recall latency, precision@k, storage efficiency
- Memory health metrics + stats endpoint
- arXiv paper: "Hippocampus: Memory Algebra and Predictive Memory for Cognitive Agents"
  - Section 1: Memory Algebra formalization (8 operations, formal properties)
  - Section 2: Cognitive science grounding (CLS theory, Predictive Processing, Global Workspace)
  - Section 3: Production architecture and benchmarks
  - Section 4: Safety implications (controllable AI through memory audit/forget/calibrate)
- README for 30-minute setup
- Show HN preparation + demo video
- **Milestone**: Paper on arXiv, system benchmarked, ready for community launch

**What NOT to build in Phase 1** (critical scope control):

1. Task Decomposer + Agent Scheduler (Phase 2 — needs working memory first)
2. Multi-agent communication (Phase 2 — single-agent proves theory first)
3. Scientific Knowledge Agent (Phase 2 — needs solid storage layer)
4. QLoRA fine-tuning pipeline (Phase 3)
5. Web UI dashboard (MCP tools + CLI sufficient)

---

## Phase 2 Roadmap (Weeks 9-16): Orchestration + Social Cognition

**Week 9-10: Agent Registry + Task Decomposer**

- Agent capability profiles (what each agent can do, historical success rates)
- Task decomposition via LLM (analyze task → sub-tasks → dependency graph)
- Agent-task matching by capability + past performance

**Week 11-12: Agent Scheduler + Cross-Agent Communication**

- DAG-based scheduler (respects dependencies, maximizes parallelism)
- Agents write results to MOS shared memory (not to each other)
- MOS detects sub-task completion → triggers dependent sub-tasks
- Conflict detection: two agents editing same file → block + alert

**Week 13-14: Thalamic Gate + Curiosity Engine**

- Gate function: novelty * info_gain * relevance > threshold
- Curiosity scanner: find knowledge gaps, generate questions
- Immune system: contradiction detection, hallucination quarantine

**Week 15-16: World Models + Observational Learning**

- Per-project dynamic models (if X then Y, with boundary conditions)
- Cross-agent experience sharing (Agent_1's success → Agent_2's knowledge)
- Phase 2 paper: "Cognitive Orchestration: Multi-Agent Coordination through Shared Memory"

---

## Phase 3 Roadmap (Weeks 17-32): Self-Improvement + Scientific Discovery

**Weeks 17-20: Scientific Knowledge Agent**

- arXiv API parser + semantic chunker + causal extraction
- Cross-paper contradiction detection
- Cross-domain analogy detection (the breakthrough feature)

**Weeks 21-24: Developmental Stages + Idle Processing (DMN)**

- Adaptive gate threshold based on memory maturity
- Idle-time hypothesis generation and self-narrative
- Narrative self-model ("what I know and don't know about this project")

**Weeks 25-28: Collective Intelligence Protocol**

- N agents sharing semantic memory with consensus
- Peer review mechanism for new facts
- Trust scores per agent (accuracy history)

**Weeks 29-32: QLoRA + Embodiment Preparation**

- Experience → training data pipeline
- Fine-tuning loop for domain-specialized models
- Sensor integration interface (for IoT/robotics future)

---

## Key Go Dependencies

- `github.com/jackc/pgx/v5` — PostgreSQL driver (connection pool, COPY protocol)
- `github.com/pgvector/pgvector-go` — pgvector support
- `github.com/mark3labs/mcp-go` — MCP server SDK
- `google.golang.org/grpc` — gRPC
- `github.com/pkoukk/tiktoken-go` — token counting (OpenAI compatible)
- `github.com/sashabaranov/go-openai` — OpenAI API client
- `golang.org/x/sync/errgroup` — structured concurrency
- `github.com/golang-migrate/migrate/v4` — DB migrations

---

## Stack Decision Summary


| Component      | Choice                            | Why                                                                                                                                         |
| -------------- | --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| Language       | **Go**                            | Proven in your production (2.6M events/day), goroutines perfect for parallel retrieval, solo-dev velocity, Cursor generates reliable Go     |
| Database       | **TimescaleDB + pgvector**        | time_bucket = native decay, continuous aggregates = native consolidation, vectors + time-series + relational in one DB, you already know it |
| Embeddings     | **OpenAI text-embedding-3-small** | 1536-dim, $0.02/1M tokens, excellent quality, no GPU needed                                                                                 |
| Protocol       | **MCP primary** + gRPC + REST     | MCP for Cursor/Claude Code (your main test), gRPC for high-performance agents, REST for everything else                                     |
| Working Memory | **In-process Go ring buffer**     | <100μs requirement eliminates any database                                                                                                  |
| Deployment     | **Docker Compose**                | One command, single binary + TimescaleDB, no Kubernetes needed for v1                                                                       |


