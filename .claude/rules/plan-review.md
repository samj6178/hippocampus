---
description: Plan mode review — enterprise checklist before implementation
---

# Plan Mode Review

Before writing code, review the plan. Do NOT implement until approved.

## Principles
- Prefer DRY — flag duplication aggressively
- Well-tested code is mandatory
- "Engineered enough" — not hacky, not over-engineered
- Correctness and edge cases over speed
- Explicit over clever

## Review Checklist

### Architecture
- Component boundaries and dependency direction (adapter → app → domain)
- Coupling risks — will this change force changes elsewhere?
- Data flow: where does data enter, transform, persist?
- Single points of failure, scaling bottlenecks

### Migration Safety (TimescaleDB)
- **Safe operations** (no downtime): ADD COLUMN with DEFAULT, ADD INDEX CONCURRENTLY, CREATE TABLE
- **Dangerous operations** (potential downtime): ALTER COLUMN TYPE, DROP COLUMN with dependents, RENAME COLUMN/TABLE, ADD NOT NULL without DEFAULT
- **Required**: every schema change MUST have `.down.sql` rollback
- **Rule**: schema changes must be backwards compatible — old code must work with new schema during rollout
- **Advisory lock**: migrations use PG advisory lock — never run two migration processes simultaneously

### MCP API Compatibility
- MCP tools are a public API — Cursor/Claude Code depend on tool schemas
- Adding new tool parameters: OK (with defaults)
- Removing/renaming parameters: BREAKING — requires version bump or deprecation
- Changing return format: BREAKING — clients parse responses
- Rule: additive changes only, or explicit deprecation path

### Tests
- Coverage plan: what's tested, what's not, why
- Edge cases: empty input, nil, zero values, unicode, max bounds
- Concurrent access: any shared state needs race testing

### Performance
- N+1 queries, missing indexes, unbounded result sets
- Memory growth: unbounded slices/maps
- Caching: repeated identical queries
- External call latency: LLM/embedding timeouts

## For Each Issue Found
1. Problem + why it matters
2. 2-3 options (including "do nothing") with effort/risk/impact
3. Recommended option

## Workflow
- Ask: BIG or SMALL change? BIG = full review, SMALL = concise
- Pause after each section for feedback
- For deep analysis → delegate to dedicated agents (architecture-reviewer, security-auditor, performance-profiler)
