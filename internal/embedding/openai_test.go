package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeEmbeddingServer(t *testing.T, dim int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp := openAIResponse{
			Data:  make([]openAIEmbedding, len(req.Input)),
			Usage: openAIUsage{PromptTokens: len(req.Input) * 10, TotalTokens: len(req.Input) * 10},
		}
		for i := range req.Input {
			emb := make([]float32, dim)
			for j := range emb {
				emb[j] = float32(i+1) * 0.01
			}
			resp.Data[i] = openAIEmbedding{Index: i, Embedding: emb}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func testProvider(t *testing.T, serverURL string) *OpenAIProvider {
	t.Helper()
	p := NewOpenAIProvider("test-key", "text-embedding-3-small", 10, 100,
		WithBaseURL(serverURL),
	)
	p.client = &http.Client{}
	return p
}

func TestLRUCache_BasicOperations(t *testing.T) {
	c := NewLRUCache(3)

	emb1 := []float32{1.0, 2.0, 3.0}
	emb2 := []float32{4.0, 5.0, 6.0}
	emb3 := []float32{7.0, 8.0, 9.0}
	emb4 := []float32{10.0, 11.0, 12.0}

	c.Put("model", "text1", emb1)
	c.Put("model", "text2", emb2)
	c.Put("model", "text3", emb3)

	if got, ok := c.Get("model", "text1"); !ok {
		t.Error("expected text1 in cache")
	} else if got[0] != 1.0 {
		t.Errorf("expected 1.0, got %f", got[0])
	}

	// text1 accessed -> LRU order: text1, text3, text2
	// Adding text4 should evict text2
	c.Put("model", "text4", emb4)

	if _, ok := c.Get("model", "text2"); ok {
		t.Error("text2 should have been evicted")
	}

	if _, ok := c.Get("model", "text1"); !ok {
		t.Error("text1 should still be in cache")
	}

	hits, misses, size := c.Stats()
	if size != 3 {
		t.Errorf("expected size 3, got %d", size)
	}
	if hits < 2 {
		t.Errorf("expected at least 2 hits, got %d", hits)
	}
	if misses < 1 {
		t.Errorf("expected at least 1 miss, got %d", misses)
	}
}

func TestLRUCache_IsolatesModels(t *testing.T) {
	c := NewLRUCache(100)

	emb1 := []float32{1.0}
	emb2 := []float32{2.0}

	c.Put("model-a", "same text", emb1)
	c.Put("model-b", "same text", emb2)

	got1, ok := c.Get("model-a", "same text")
	if !ok || got1[0] != 1.0 {
		t.Errorf("model-a should return 1.0, got %v", got1)
	}

	got2, ok := c.Get("model-b", "same text")
	if !ok || got2[0] != 2.0 {
		t.Errorf("model-b should return 2.0, got %v", got2)
	}
}

func TestLRUCache_MutationSafety(t *testing.T) {
	c := NewLRUCache(10)
	original := []float32{1.0, 2.0, 3.0}
	c.Put("m", "t", original)

	original[0] = 999.0

	got, ok := c.Get("m", "t")
	if !ok {
		t.Fatal("expected hit")
	}
	if got[0] != 1.0 {
		t.Errorf("cache should be isolated from mutations, got %f", got[0])
	}

	got[1] = 888.0
	got2, _ := c.Get("m", "t")
	if got2[1] != 2.0 {
		t.Errorf("returned slice mutation should not affect cache, got %f", got2[1])
	}
}

func TestNormalizeText(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"  hello  world  ", "hello world"},
		{"\t\nnewlines\t\n", "newlines"},
		{"already clean", "already clean"},
		{"", ""},
		{"  ", ""},
	}
	for _, tt := range tests {
		got := normalizeText(tt.input)
		if got != tt.want {
			t.Errorf("normalizeText(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEmbed_EmptyText(t *testing.T) {
	p := NewOpenAIProvider("key", "model", 10, 100)

	result, err := p.Embed(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != p.dims {
		t.Errorf("expected zero vector of dim %d, got %d", p.dims, len(result))
	}
	for i, v := range result {
		if v != 0 {
			t.Errorf("expected 0 at index %d, got %f", i, v)
		}
	}
}
