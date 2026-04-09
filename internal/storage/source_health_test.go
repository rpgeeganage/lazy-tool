package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateCircuitStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "source-health.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	failedAt := time.Date(2026, 4, 9, 9, 0, 0, 0, time.UTC)
	if err := s.UpdateCircuitState(ctx, "src1", "open", 3, &failedAt); err != nil {
		t.Fatal(err)
	}
	row, ok, err := s.GetSourceHealth(ctx, "src1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected source health row")
	}
	if row.CircuitState != "open" || row.CircuitFailures != 3 {
		t.Fatalf("unexpected row: %#v", row)
	}
	if row.CircuitLastFailedAt == nil || !row.CircuitLastFailedAt.Equal(failedAt) {
		t.Fatalf("unexpected failed_at: %#v", row.CircuitLastFailedAt)
	}
}
