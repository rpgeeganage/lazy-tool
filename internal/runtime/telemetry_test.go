package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"lazy-tool/internal/config"
	"lazy-tool/internal/storage"
)

func TestStartTelemetryPurger_PurgesImmediatelyAndStops(t *testing.T) {
	st, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	if err := st.RecordOperation(ctx, storage.OperationLogEvent{
		Operation: "stale",
		CreatedAt: time.Now().UTC().AddDate(0, 0, -40),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordOperation(ctx, storage.OperationLogEvent{
		Operation: "fresh",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	cfg.Telemetry.RetentionDays = 30
	cfg.Telemetry.PurgeIntervalHours = 1
	cfg.Telemetry.MaxRows = 100

	stop := startTelemetryPurger(st, cfg)
	if stop == nil {
		t.Fatal("expected stop func")
	}
	defer stop()

	rows, err := st.ListRecentOperations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d want 1", len(rows))
	}
	if rows[0].Operation != "fresh" {
		t.Fatalf("remaining operation = %q want fresh", rows[0].Operation)
	}
	stop()
}
