package embeddings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestOllamaEmbed_UsesBatchEndpoint(t *testing.T) {
	t.Parallel()

	var batchCalls atomic.Int32
	var mu sync.Mutex
	var handlerErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embed":
			batchCalls.Add(1)
			var req struct {
				Model string   `json:"model"`
				Input []string `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				mu.Lock()
				handlerErr = fmt.Errorf("decode batch request: %w", err)
				mu.Unlock()
				http.Error(w, handlerErr.Error(), http.StatusBadRequest)
				return
			}
			if len(req.Input) != 2 || req.Input[0] != "text1" || req.Input[1] != "text2" {
				mu.Lock()
				handlerErr = fmt.Errorf("unexpected batch input: %#v", req.Input)
				mu.Unlock()
				http.Error(w, handlerErr.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"embeddings": [][]float64{{1, 2}, {3, 4}},
			})
		case "/api/embeddings":
			t.Fatal("legacy endpoint should not be called when batch succeeds")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	o := &Ollama{BaseURL: server.URL, Model: "nomic-embed-text", Client: server.Client()}
	vecs, err := o.Embed(context.Background(), []string{"text1", "text2"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if batchCalls.Load() != 1 {
		t.Fatalf("batch calls = %d want 1", batchCalls.Load())
	}
	if len(vecs) != 2 || len(vecs[0]) != 2 || len(vecs[1]) != 2 {
		t.Fatalf("unexpected vectors: %#v", vecs)
	}
	if vecs[0][0] != 1 || vecs[1][1] != 4 {
		t.Fatalf("unexpected vector values: %#v", vecs)
	}
}

func TestOllamaEmbed_FallsBackToLegacyOnBatch404(t *testing.T) {
	t.Parallel()

	var batchCalls atomic.Int32
	var legacyCalls atomic.Int32
	var mu sync.Mutex
	var handlerErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embed":
			batchCalls.Add(1)
			http.NotFound(w, r)
		case "/api/embeddings":
			legacyCalls.Add(1)
			var req struct {
				Prompt string `json:"prompt"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				mu.Lock()
				handlerErr = fmt.Errorf("decode legacy request: %w", err)
				mu.Unlock()
				http.Error(w, handlerErr.Error(), http.StatusBadRequest)
				return
			}
			resp := map[string]any{"embedding": []float64{float64(len(req.Prompt)), 9}}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	o := &Ollama{BaseURL: server.URL, Model: "nomic-embed-text", Client: server.Client()}
	vecs, err := o.Embed(context.Background(), []string{"a", "bbbb"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if batchCalls.Load() != 1 {
		t.Fatalf("batch calls = %d want 1", batchCalls.Load())
	}
	if legacyCalls.Load() != 2 {
		t.Fatalf("legacy calls = %d want 2", legacyCalls.Load())
	}
	if len(vecs) != 2 || vecs[0][0] != 1 || vecs[1][0] != 4 {
		t.Fatalf("unexpected fallback vectors: %#v", vecs)
	}
}
