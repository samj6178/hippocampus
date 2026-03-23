package vecutil

import (
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0.0},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1.0},
		{"similar", []float32{1, 1}, []float32{1, 0.9}, 0.99},
		{"empty", nil, nil, 0.0},
		{"length_mismatch", []float32{1}, []float32{1, 2}, 0.0},
		{"zero_vector", []float32{0, 0}, []float32{1, 1}, 0.0},
		{"both_zero", []float32{0, 0}, []float32{0, 0}, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CosineSimilarity(tt.a, tt.b)
			if tt.name == "similar" {
				if got < 0.99 || got > 1.0 {
					t.Errorf("CosineSimilarity() = %f, want ~%f", got, tt.want)
				}
				return
			}
			if math.Abs(got-tt.want) > 1e-6 {
				t.Errorf("CosineSimilarity() = %f, want %f", got, tt.want)
			}
		})
	}
}
