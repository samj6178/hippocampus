package roomclass

import "strings"

// Room represents a classified topic area for a memory.
const (
	RoomArchitecture = "architecture"
	RoomDatabase     = "database"
	RoomAPI          = "api"
	RoomTesting      = "testing"
	RoomDeployment   = "deployment"
	RoomSecurity     = "security"
	RoomPerformance  = "performance"
	RoomBugs         = "bugs"
	RoomDecisions    = "decisions"
	RoomDependencies = "dependencies"
	RoomGeneral      = "general"
)

// TagPrefix is prepended to room names when stored as memory tags.
const TagPrefix = "room:"

type pattern struct {
	room     string
	keywords []string
	weight   int // higher weight wins on tie
}

var patterns = []pattern{
	{RoomBugs, []string{
		"error", "panic", "crash", "fatal", "bug", "fix:", "fixed",
		"root cause", "stack trace", "nil pointer", "race condition",
		"segfault", "oom", "memory leak", "deadlock", "timeout error",
	}, 3},
	{RoomDecisions, []string{
		"decided", "decision", "chose", "chosen", "trade-off", "tradeoff",
		"approach", "instead of", "alternative", "because we", "opted for",
		"went with", "rationale", "reasoning behind",
	}, 3},
	{RoomArchitecture, []string{
		"architecture", "clean architecture", "hexagonal", "dependency injection",
		"layer", "adapter", "domain model", "interface", "refactor", "restructure",
		"design pattern", "composition", "abstraction", "coupling", "cohesion",
		"module", "package structure",
	}, 2},
	{RoomDatabase, []string{
		"sql", "migration", "schema", "index", "query", "postgres", "sqlite",
		"table", "column", "foreign key", "transaction", "constraint",
		"database", "pgx", "timescale", "fts5", "vector search",
	}, 2},
	{RoomAPI, []string{
		"endpoint", "handler", "route", "request", "response", "rest",
		"grpc", "mcp", "tool", "json-rpc", "http", "middleware",
		"api", "status code", "payload",
	}, 2},
	{RoomTesting, []string{
		"test", "mock", "assert", "fixture", "coverage", "tdd",
		"table-driven", "benchmark", "testify", "vitest",
		"test case", "regression test",
	}, 2},
	{RoomDeployment, []string{
		"docker", "compose", "deploy", "ci", "pipeline", "build",
		"container", "kubernetes", "k8s", "nginx", "systemd",
		"dockerfile", "image", "registry",
	}, 2},
	{RoomSecurity, []string{
		"vulnerability", "security", "auth", "credential", "permission",
		"jwt", "token", "encryption", "injection", "xss", "csrf",
		"secret", "certificate", "tls",
	}, 2},
	{RoomPerformance, []string{
		"latency", "throughput", "bottleneck", "optimization", "benchmark",
		"profiling", "cache", "memory usage", "cpu", "goroutine pool",
		"connection pool", "p99", "slow query",
	}, 2},
	{RoomDependencies, []string{
		"dependency", "go.mod", "package.json", "cargo.toml", "import",
		"upgrade", "downgrade", "breaking change", "compatibility",
		"version", "semver", "library",
	}, 1},
}

// Classify returns the best-matching room for the given content.
// Returns RoomGeneral if no patterns match above threshold.
func Classify(content string) string {
	lower := strings.ToLower(content)

	type scored struct {
		room  string
		score int
	}
	var results []scored

	for _, p := range patterns {
		hits := 0
		for _, kw := range p.keywords {
			if strings.Contains(lower, kw) {
				hits++
			}
		}
		if hits > 0 {
			results = append(results, scored{room: p.room, score: hits * p.weight})
		}
	}

	if len(results) == 0 {
		return RoomGeneral
	}

	best := results[0]
	for _, r := range results[1:] {
		if r.score > best.score {
			best = r
		}
	}
	return best.room
}

// Tag returns the tag string for a room, e.g. "room:architecture".
func Tag(room string) string {
	return TagPrefix + room
}

// FromTag extracts the room name from a tag, e.g. "room:architecture" -> "architecture".
// Returns empty string if the tag is not a room tag.
func FromTag(tag string) string {
	if strings.HasPrefix(tag, TagPrefix) {
		return tag[len(TagPrefix):]
	}
	return ""
}
