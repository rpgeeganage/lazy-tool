package embeddings

import (
	"context"
	"errors"
	"testing"
	"time"

	"lazy-tool/internal/metrics"
)

type flakyEmbedder struct {
	fails int
	calls int
}

func (f *flakyEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	_, _ = ctx, texts
	f.calls++
	if f.calls <= f.fails {
		return nil, errors.New("temporary failure")
	}
	return [][]float32{{1, 2, 3}}, nil
}

func (f *flakyEmbedder) ModelName() string { return "flaky" }

func TestRetryingEmbedder_RetriesAndSucceeds(t *testing.T) {
	flaky := &flakyEmbedder{fails: 2}
	var retries []int
	prev := metrics.ConnectorRetry
	metrics.ConnectorRetry = func(sourceID string, attempt int, err error) {
		if sourceID != "embeddings" || err == nil {
			t.Fatalf("unexpected retry hook args: %q %d %v", sourceID, attempt, err)
		}
		retries = append(retries, attempt)
	}
	defer func() { metrics.ConnectorRetry = prev }()

	r := retryingEmbedder{next: flaky, attempts: 3, backoff: time.Millisecond, sourceID: "embeddings"}
	vecs, err := r.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 3 {
		t.Fatalf("unexpected vectors: %#v", vecs)
	}
	if flaky.calls != 3 {
		t.Fatalf("calls = %d want 3", flaky.calls)
	}
	if len(retries) != 2 || retries[0] != 1 || retries[1] != 2 {
		t.Fatalf("unexpected retries: %#v", retries)
	}
}

func TestRetryingEmbedder_ReturnsLastError(t *testing.T) {
	flaky := &flakyEmbedder{fails: 5}
	r := retryingEmbedder{next: flaky, attempts: 2, backoff: time.Millisecond, sourceID: "embeddings"}
	_, err := r.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if flaky.calls != 2 {
		t.Fatalf("calls = %d want 2", flaky.calls)
	}
}
