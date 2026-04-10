package contenthash

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Of computes a persistent dedup key for memory content.
// Uses SHA256 truncated to 128 bits (32 hex chars) — collision probability
// is negligible for any practical memory count.
// Content is trimmed before hashing to ensure consistent dedup
// regardless of leading/trailing whitespace.
func Of(content string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(h[:16])
}
