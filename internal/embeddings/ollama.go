package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ollamaBatchResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

type ollamaLegacyResponse struct {
	Embedding []float64 `json:"embedding"`
}

type Ollama struct {
	BaseURL string
	Model   string
	Client  *http.Client
}

func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 120 * time.Second}
	}
	base := strings.TrimSuffix(o.BaseURL, "/")
	if base == "" {
		base = "http://127.0.0.1:11434"
	}
	vecs, fallback, err := o.embedBatch(ctx, base, texts)
	if err != nil {
		return nil, err
	}
	if !fallback {
		return vecs, nil
	}
	return o.embedLegacyLoop(ctx, base, texts)
}

func (o *Ollama) ModelName() string { return o.Model }

func (o *Ollama) embedBatch(ctx context.Context, base string, texts []string) ([][]float32, bool, error) {
	body, _ := json.Marshal(map[string]any{"model": o.Model, "input": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("ollama embed %s: %s", resp.Status, shortMsg(string(raw), 120))
	}
	var parsed ollamaBatchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, false, err
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, false, fmt.Errorf("ollama embed count mismatch")
	}
	out := make([][]float32, len(texts))
	for i, emb := range parsed.Embeddings {
		out[i] = toFloat32(emb)
	}
	return out, false, nil
}

func (o *Ollama) embedLegacyLoop(ctx context.Context, base string, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		body, _ := json.Marshal(map[string]any{"model": o.Model, "prompt": text})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := o.Client.Do(req)
		if err != nil {
			return nil, err
		}
		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("ollama embeddings %s: %s", resp.Status, shortMsg(string(raw), 120))
		}
		var parsed ollamaLegacyResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, err
		}
		out[i] = toFloat32(parsed.Embedding)
	}
	return out, nil
}

func toFloat32(in []float64) []float32 {
	out := make([]float32, len(in))
	for i, v := range in {
		out[i] = float32(v)
	}
	return out
}
