package embeddings

import (
	"os"
	"strings"
	"time"

	"lazy-tool/internal/config"
)

func FromConfig(c *config.Config) Embedder {
	p := strings.ToLower(c.Embeddings.Provider)
	var out Embedder
	switch {
	case p == "" || p == "noop":
		out = Noop{}
	case strings.Contains(p, "ollama"):
		out = &Ollama{
			BaseURL: c.Embeddings.BaseURL,
			Model:   c.Embeddings.Model,
		}
	case strings.Contains(p, "openai") || p == "openai-compatible":
		key := ""
		if c.Embeddings.APIKeyEnv != "" {
			key = os.Getenv(c.Embeddings.APIKeyEnv)
		}
		out = &OpenAICompatible{
			BaseURL:   c.Embeddings.BaseURL,
			APIKey:    key,
			Model:     c.Embeddings.Model,
			UserAgent: "lazy-tool/0.1",
		}
	default:
		out = Noop{}
	}
	if c.Embeddings.RetryAttempts > 0 && kind(out) != "noop" {
		out = retryingEmbedder{
			next:     out,
			attempts: c.Embeddings.RetryAttempts,
			backoff:  time.Duration(c.Embeddings.RetryBackoffMS) * time.Millisecond,
			sourceID: "embeddings",
		}
	}
	return out
}
