package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"lazy-tool/pkg/models"
)

func seedRecords(t *testing.T, s *SQLiteStore, source string, names ...string) {
	t.Helper()
	ctx := context.Background()
	for _, name := range names {
		rec := models.CapabilityRecord{
			ID:            source + "_" + name,
			Kind:          models.CapabilityKindTool,
			SourceID:      source,
			SourceType:    "gateway",
			CanonicalName: source + "__" + name,
			OriginalName:  name,
			SearchText:    name,
			InputSchemaJSON: "{}",
			MetadataJSON:    "{}",
			Tags:            []string{},
			VersionHash:     "h",
			LastSeenAt:      time.Now().UTC().Truncate(time.Second),
		}
		if err := s.UpsertCapability(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListBySourceWithLimit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	s, err := OpenSQLite(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	seedRecords(t, s, "s1", "alpha", "beta", "gamma", "delta")
	ctx := context.Background()

	// Limit 2: should return exactly 2
	recs, err := s.ListBySourceWithLimit(ctx, "s1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2, got %d", len(recs))
	}

	// Limit larger than count: returns all 4
	recs, err = s.ListBySourceWithLimit(ctx, "s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 4 {
		t.Fatalf("expected 4, got %d", len(recs))
	}

	// Wrong source: returns 0
	recs, err = s.ListBySourceWithLimit(ctx, "nope", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected 0, got %d", len(recs))
	}
}

func TestSQLite_UpsertRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	s, err := OpenSQLite(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	rec := models.CapabilityRecord{
		ID:                  "id1",
		Kind:                models.CapabilityKindTool,
		SourceID:            "s1",
		SourceType:          "gateway",
		CanonicalName:       "s1__toola",
		OriginalName:        "toolA",
		OriginalDescription: "does a",
		GeneratedSummary:    "Does a thing.",
		SearchText:          "toola does",
		InputSchemaJSON:     "{}",
		MetadataJSON:        "{}",
		Tags:                []string{"x"},
		VersionHash:         "abc",
		LastSeenAt:          time.Now().UTC().Truncate(time.Second),
	}
	if err := s.UpsertCapability(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCapability(ctx, "id1")
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalName != rec.CanonicalName {
		t.Fatal(got.CanonicalName)
	}
	got2, err := s.GetByCanonicalName(ctx, "s1__toola")
	if err != nil {
		t.Fatal(err)
	}
	if got2.ID != rec.ID {
		t.Fatal(got2.ID)
	}
}
