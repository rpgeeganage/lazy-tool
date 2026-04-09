package embeddings

import (
	"context"
	"time"

	"lazy-tool/internal/metrics"
)

type retryingEmbedder struct {
	next       Embedder
	attempts   int
	backoff    time.Duration
	sourceID   string
}

func (r retryingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	attempts := r.attempts
	if attempts <= 0 {
		attempts = 1
	}
	backoff := r.backoff
	if backoff <= 0 {
		backoff = time.Second
	}
	var last error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			metrics.ConnectorRetry(r.sourceID, attempt, last)
			delay := backoff
			for i := 1; i < attempt; i++ {
				delay *= 2
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		vecs, err := r.next.Embed(ctx, texts)
		if err == nil {
			return vecs, nil
		}
		last = err
	}
	return nil, last
}

func (r retryingEmbedder) ModelName() string {
	return r.next.ModelName()
}
