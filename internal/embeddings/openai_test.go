package embeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbedderEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var request struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != DefaultModel || len(request.Input) != 2 {
			t.Fatalf("request = %+v", request)
		}
		vector := make([]float32, DefaultDimensions)
		vector[0] = 1
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"index": 1, "embedding": vector},
			{"index": 0, "embedding": vector},
		}})
	}))
	defer server.Close()

	embedder, err := NewOpenAIEmbedder("test-key", "", server.URL)
	if err != nil {
		t.Fatalf("NewOpenAIEmbedder: %v", err)
	}
	embedder.client = server.Client()
	vectors, err := embedder.Embed(context.Background(), []string{"one", "two"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vectors) != 2 || len(vectors[0]) != DefaultDimensions || vectors[0][0] != 1 {
		t.Errorf("vectors have unexpected shape")
	}
}

func TestOpenAIEmbedderRejectsIncompatibleModel(t *testing.T) {
	if _, err := NewOpenAIEmbedder("test-key", "text-embedding-3-large", ""); err == nil {
		t.Fatal("NewOpenAIEmbedder accepted incompatible model")
	}
}
