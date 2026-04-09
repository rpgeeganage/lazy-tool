package embeddings

import (
	"context"
	"testing"

	"lazy-tool/internal/config"
)

func kind(e Embedder) string {
	switch e.(type) {
	case Noop:
		return "noop"
	case *Ollama:
		return "ollama"
	case *OpenAICompatible:
		return "openai"
	default:
		return "unknown"
	}
}

func TestFromConfig_providerRouting(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		provider string
		wantKind string
	}{
		{"", "noop"},
		{"noop", "noop"},
		{"NoOp", "noop"},
		{"ollama", "ollama"},
		{"OllamaEmbed", "ollama"},
		{"openai", "openai"},
		{"OpenAI", "openai"},
		{"openai-compatible", "openai"},
		{"unknown-vendor", "noop"},
	}
	for _, tc := range cases {
		cfg := &config.Config{}
		cfg.Embeddings.Provider = tc.provider
		e := FromConfig(cfg)
		if got := kind(e); got != tc.wantKind {
			t.Fatalf("provider %q: kind = %q want %q", tc.provider, got, tc.wantKind)
		}
		if tc.wantKind == "noop" {
			if _, err := e.Embed(ctx, []string{"a"}); err != nil {
				t.Fatalf("provider %q: Embed: %v", tc.provider, err)
			}
		}
	}
	if n := FromConfig(&config.Config{}); n.ModelName() != "noop" {
		t.Fatalf("noop ModelName = %q", n.ModelName())
	}
}

func TestNew_delegatesToFromConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Embeddings.Provider = "noop"
	if _, ok := New(cfg).(Noop); !ok {
		t.Fatal("expected Noop embedder")
	}
}

func TestFromConfig_wrapsRetryingEmbedder(t *testing.T) {
	cfg := &config.Config{}
	cfg.Embeddings.Provider = "ollama"
	cfg.Embeddings.RetryAttempts = 2
	cfg.Embeddings.RetryBackoffMS = 10
	e := FromConfig(cfg)
	if _, ok := e.(retryingEmbedder); !ok {
		t.Fatalf("expected retryingEmbedder, got %T", e)
	}
}
