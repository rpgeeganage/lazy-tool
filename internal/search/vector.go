package search

import (
	"context"
	"time"

	"github.com/philippgille/chromem-go"
	"lazy-tool/internal/metrics"
	"lazy-tool/internal/storage"
	"lazy-tool/internal/vector"
)

// VectorQuery runs embedding similarity against the chromem index (spec §14 vector leg).
func VectorQuery(ctx context.Context, store *storage.SQLiteStore, idx *vector.Index, embedding []float32, limit int, sourceID string) ([]chromem.Result, error) {
	started := time.Now()
	if idx == nil {
		durationMS := time.Since(started).Milliseconds()
		metrics.VectorQueryDuration(durationMS, sourceID, limit, nil)
		recordVectorQuery(ctx, store, sourceID, limit, durationMS, nil)
		return nil, nil
	}
	res, err := idx.Query(ctx, embedding, limit, sourceID)
	durationMS := time.Since(started).Milliseconds()
	metrics.VectorQueryDuration(durationMS, sourceID, limit, err)
	recordVectorQuery(ctx, store, sourceID, limit, durationMS, err)
	return res, err
}

func recordVectorQuery(ctx context.Context, store *storage.SQLiteStore, sourceID string, limit int, durationMS int64, err error) {
	if store == nil {
		return
	}
	_ = store.RecordOperation(ctx, storage.OperationLogEvent{
		Operation:  "vector_query",
		SourceID:   sourceID,
		DurationMS: durationMS,
		Metadata: map[string]any{
			"limit": limit,
		},
		Error: errorString(err),
	})
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
