package mcpserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"lazy-tool/internal/app"
	"lazy-tool/internal/runtime"
	"lazy-tool/internal/storage"
	"lazy-tool/pkg/models"
)

func seedStore(t *testing.T, s *storage.SQLiteStore, source string, names ...string) {
	t.Helper()
	ctx := context.Background()
	for _, name := range names {
		rec := models.CapabilityRecord{
			ID:              source + "_" + name,
			Kind:            models.CapabilityKindTool,
			SourceID:        source,
			SourceType:      "gateway",
			CanonicalName:   source + "__" + name,
			OriginalName:    name,
			SearchText:      name,
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

func testStack(t *testing.T, sources []models.Source) (*runtime.Stack, *storage.SQLiteStore) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.db")
	st, err := storage.OpenSQLite(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	reg, err := app.NewSourceRegistry(sources)
	if err != nil {
		t.Fatal(err)
	}
	return &runtime.Stack{Store: st, Registry: reg}, st
}

func TestPassthroughFallback_ZeroResultsTriggers(t *testing.T) {
	stack, st := testStack(t, []models.Source{
		{ID: "s1", Type: models.SourceTypeGateway, Fallback: "passthrough"},
	})
	seedStore(t, st, "s1", "alpha", "beta")
	ctx := context.Background()
	ranked := applyPassthroughFallback(ctx, stack, models.RankedResults{}, 10, nil)
	if len(ranked.Results) != 2 {
		t.Fatalf("expected 2, got %d", len(ranked.Results))
	}
	if ranked.CandidatePath != "passthrough_fallback" {
		t.Fatalf("expected passthrough_fallback, got %q", ranked.CandidatePath)
	}
}

func TestPassthroughFallback_RespectsLimit(t *testing.T) {
	stack, st := testStack(t, []models.Source{
		{ID: "s1", Type: models.SourceTypeGateway, Fallback: "passthrough"},
	})
	seedStore(t, st, "s1", "a", "b", "c", "d", "e")
	ctx := context.Background()
	ranked := applyPassthroughFallback(ctx, stack, models.RankedResults{}, 3, nil)
	if len(ranked.Results) != 3 {
		t.Fatalf("expected 3, got %d", len(ranked.Results))
	}
}

func TestPassthroughFallback_RespectsSourceFilter(t *testing.T) {
	stack, st := testStack(t, []models.Source{
		{ID: "s1", Type: models.SourceTypeGateway, Fallback: "passthrough"},
		{ID: "s2", Type: models.SourceTypeGateway, Fallback: "passthrough"},
	})
	seedStore(t, st, "s1", "alpha")
	seedStore(t, st, "s2", "beta")
	ctx := context.Background()
	ranked := applyPassthroughFallback(ctx, stack, models.RankedResults{}, 10, []string{"s2"})
	if len(ranked.Results) != 1 {
		t.Fatalf("expected 1, got %d", len(ranked.Results))
	}
	if ranked.Results[0].SourceID != "s2" {
		t.Fatalf("expected s2, got %q", ranked.Results[0].SourceID)
	}
}

func TestPassthroughFallback_EmptyFilterAllowsAll(t *testing.T) {
	stack, st := testStack(t, []models.Source{
		{ID: "s1", Type: models.SourceTypeGateway, Fallback: "passthrough"},
		{ID: "s2", Type: models.SourceTypeGateway, Fallback: "passthrough"},
	})
	seedStore(t, st, "s1", "alpha")
	seedStore(t, st, "s2", "beta")
	ctx := context.Background()
	ranked := applyPassthroughFallback(ctx, stack, models.RankedResults{}, 10, nil)
	if len(ranked.Results) != 2 {
		t.Fatalf("expected 2, got %d", len(ranked.Results))
	}
}

func TestPassthroughFallback_NoPassthroughSources(t *testing.T) {
	stack, st := testStack(t, []models.Source{
		{ID: "s1", Type: models.SourceTypeGateway},
	})
	seedStore(t, st, "s1", "alpha")
	ctx := context.Background()
	ranked := applyPassthroughFallback(ctx, stack, models.RankedResults{}, 10, nil)
	if len(ranked.Results) != 0 {
		t.Fatalf("expected 0, got %d", len(ranked.Results))
	}
}
