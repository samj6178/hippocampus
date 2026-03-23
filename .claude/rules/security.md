---
description: Security guardrails — enterprise-grade, always active for Go files
paths:
  - "**/*.go"
---

# Security Rules

## Injection Prevention
- SQL: parameterized queries ONLY (`$1, $2`) — never string concatenation or `fmt.Sprintf` for SQL
- Command injection: never pass user input to `exec.Command` without validation
- Path traversal: validate ALL file paths — reject `../`, absolute paths from user input
- Template injection: use `html/template` auto-escaping, never `text/template` for HTML

## SSRF Protection
- This project calls external services (Ollama, OpenAI, arxiv, GitHub API)
- NEVER allow user-controlled URLs to reach HTTP clients without validation
- Allowlist approach: validate URLs against known hosts before fetching
- Block internal network ranges (127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16)
- MCP tool inputs that contain URLs MUST be validated before use

## Resource Exhaustion
- Every user-facing input MUST have size limits:
  - Memory content: max length before processing
  - Recall query: max token budget
  - Batch operations: max items per request
- Every slice, map, channel MUST have bounded growth
- DB queries: always use LIMIT, never unbounded SELECT
- Goroutines: never spawn unbounded goroutines from user input
- HTTP request body: enforce `http.MaxBytesReader` on all endpoints
- Timeouts: every external call (DB, LLM, embedding) MUST have context timeout

## Secrets Management
- Secrets ONLY via environment variables — never in source, config files, or logs
- `.gitignore` MUST cover: `.env`, `config.local.json`, `*.pem`, `*.key`
- Never log secrets, API keys, tokens, passwords, or connection strings
- Never include secrets in error messages returned to clients

## Authentication & Authorization
- Map all REST endpoints → required auth level
- MCP server: validate session/agent identity
- Health check endpoints may be public — no sensitive data in health responses
- Admin operations (consolidate, LLM switch): require authorization

## Data Exposure
- API responses: never leak internal IDs, SQL errors, stack traces, file paths
- Error messages to clients: generic ("internal error"), specific details in server logs only
- CORS: explicit allowlist, never wildcard in production
- Embedding vectors: treat as sensitive — don't expose raw embeddings via API

## Logging Security
- NEVER log: passwords, tokens, API keys, PII, raw embeddings, connection strings
- Sanitize user input before logging — prevent log injection (newlines, control chars)
- Structured logging (slog) with explicit fields — no `fmt.Sprintf` with user data into log messages

## Dependency Security
- Review `go.mod` for known vulnerabilities: `govulncheck ./...`
- Flag dependencies >6 months behind latest
- Flag abandoned dependencies (no commits in 1+ year)
- Minimize direct dependencies — prefer stdlib

## Database Security
- Connection pool: enforce max connections, idle timeouts, health checks
- Migrations: advisory lock to prevent concurrent runs
- Never expose DB connection details in API responses or logs
- Use read-only connections for query-only operations where possible

*For comprehensive security audit → use security-auditor agent.*
