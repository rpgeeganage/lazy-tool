package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOperationLogSummariesAndTimeline(t *testing.T) {
	ctx := context.Background()
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	now := time.Date(2026, 4, 9, 8, 0, 0, 0, time.UTC)
	events := []OperationLogEvent{
		{Operation: "search", DurationMS: 10, CreatedAt: now.Add(-90 * time.Minute)},
		{Operation: "search", DurationMS: 20, Error: "boom", CreatedAt: now.Add(-30 * time.Minute)},
		{Operation: "proxy_invoke", SourceID: "src1", DurationMS: 5, Metadata: map[string]any{"cached": true}, CreatedAt: now.Add(-10 * time.Minute)},
		{Operation: "proxy_invoke", SourceID: "src1", DurationMS: 7, Metadata: map[string]any{"cached": false}, CreatedAt: now.Add(-5 * time.Minute)},
		{Operation: "reindex", SourceID: "src1", DurationMS: 30, CreatedAt: now},
	}
	for _, ev := range events {
		if err := s.RecordOperation(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UpsertSourceHealth(ctx, "src1", true, "ok"); err != nil {
		t.Fatal(err)
	}

	summary, err := s.SummarizeOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Count != 5 || summary.ErrorCount != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if summary.CacheHits != 1 || summary.CacheMisses != 1 {
		t.Fatalf("unexpected cache summary: %#v", summary)
	}
	points, err := s.SearchTimeline(ctx, 60, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("timeline points = %d want 2", len(points))
	}
	sources, err := s.SourceOperationSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].SourceID != "src1" {
		t.Fatalf("unexpected sources: %#v", sources)
	}
	if sources[0].LastReindexOK == nil || !*sources[0].LastReindexOK {
		t.Fatalf("expected reindex health: %#v", sources[0])
	}
}
