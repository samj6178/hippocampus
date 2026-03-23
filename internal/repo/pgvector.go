package repo

import (
	"fmt"
	"strconv"
	"strings"
)

// encodeVector converts a float32 slice to pgvector literal format: '[1.0,2.0,3.0]'
func encodeVector(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// decodeVector parses pgvector text format '[0.1,0.2,0.3]' back to []float32.
func decodeVector(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return nil, nil
	}
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	parts := strings.Split(s, ",")
	result := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("decodeVector element %d: %w", i, err)
		}
		result[i] = float32(f)
	}
	return result, nil
}
