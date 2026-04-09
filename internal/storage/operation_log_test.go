package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOperationLogRecordAndList(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	ctx := context.Background()
	createdAt := time.Date(2026, 4, 9, 7, 0, 0, 0, time.UTC)
	err = s.RecordOperation(ctx, OperationLogEvent{
		Operation:  "search",
		SourceID:   "source-a",
		DurationMS: 17,
		Metadata: map[string]any{
			"candidates": 42,
		},
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListRecentOperations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d want 1", len(rows))
	}
	if rows[0].Operation != "search" || rows[0].SourceID != "source-a" {
		t.Fatalf("unexpected row: %#v", rows[0])
	}
	if rows[0].DurationMS != 17 {
		t.Fatalf("duration = %d want 17", rows[0].DurationMS)
	}
	if rows[0].CreatedAt.UTC() != createdAt {
		t.Fatalf("createdAt = %s want %s", rows[0].CreatedAt.UTC(), createdAt)
	}
	count, err := s.CountOperationLog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d want 1", count)
	}
}

func TestPurgeOperationLogByRetentionAndMaxRows(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "purge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	ctx := context.Background()
	now := time.Now().UTC()
	entries := []OperationLogEvent{
		{Operation: "old", CreatedAt: now.AddDate(0, 0, -40)},
		{Operation: "keep-1", CreatedAt: now.Add(-2 * time.Hour)},
		{Operation: "keep-2", CreatedAt: now.Add(-1 * time.Hour)},
		{Operation: "keep-3", CreatedAt: now},
	}
	for _, entry := range entries {
		if err := s.RecordOperation(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := s.PurgeOperationLog(ctx, TelemetryPurgeConfig{RetentionDays: 30, MaxRows: 2})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d want 2", deleted)
	}
	rows, err := s.ListRecentOperations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d want 2", len(rows))
	}
	if rows[0].Operation != "keep-3" || rows[1].Operation != "keep-2" {
		t.Fatalf("unexpected remaining rows: %#v", rows)
	}
}
