---
paths:
  - "internal/adapter/mcp/**"
  - "**/mcp/**"
---

# MCP Protocol Rules

CRITICAL: MCP stdio transport uses newline-delimited JSON (JSONL), NOT Content-Length framing.
- Each message is one JSON object followed by a newline
- Use `bufio.Scanner` + `json.Marshal` + `\n`, NOT LSP-style Content-Length headers
- Always build to `bin/` directory to avoid Cursor caching stale binaries
- Test with `echo '{"jsonrpc":"2.0","method":"initialize","id":1}' | ./bin/hippocampus.exe`
