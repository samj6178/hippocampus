package sqlite

import (
	"encoding/binary"
	"math"
)

const float32Size = 4 // bytes per float32

// EncodeEmbedding serialises a float32 slice to a compact binary blob using
// little-endian IEEE 754 encoding (4 bytes per dimension).
// Returns nil for a nil or empty input.
func EncodeEmbedding(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	buf := make([]byte, len(v)*float32Size)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*float32Size:], math.Float32bits(f))
	}
	return buf
}

// DecodeEmbedding deserialises a binary blob produced by EncodeEmbedding back
// into a []float32. Returns nil for a nil or empty input.
// If the blob length is not a multiple of 4 the trailing bytes are ignored.
func DecodeEmbedding(b []byte) []float32 {
	if len(b) == 0 {
		return nil
	}
	n := len(b) / float32Size
	v := make([]float32, n)
	for i := range v {
		bits := binary.LittleEndian.Uint32(b[i*float32Size:])
		v[i] = math.Float32frombits(bits)
	}
	return v
}
