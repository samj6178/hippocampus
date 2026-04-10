package embedding

import "context"

// NullProvider returns nil embeddings for all calls.
// Used when embedding mode is "none" (BM25-only mode).
type NullProvider struct{}

func NewNullProvider() *NullProvider {
	return &NullProvider{}
}

func (p *NullProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}

func (p *NullProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	return result, nil
}

func (p *NullProvider) Dimensions() int {
	return 0
}

func (p *NullProvider) ModelID() string {
	return "none"
}
