package catalog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lazy-tool/internal/storage"
	"lazy-tool/pkg/models"
)

type fakeSummarizer struct {
	called bool
	last   models.CapabilityRecord
	out    string
	err    error
}

func (f *fakeSummarizer) Summarize(ctx context.Context, rec models.CapabilityRecord) (string, error) {
	_ = ctx
	f.called = true
	f.last = rec
	return f.out, f.err
}

func TestIndexerAutoRefinesVagueDescriptions(t *testing.T) {
	ctx := context.Background()
	st, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "refine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	fake := &fakeSummarizer{out: "Searches repositories using owner and query filters."}
	ix := &Indexer{
		Summary:            fake,
		Store:              st,
		AutoRefineVague:    true,
		VaguenessThreshold: 0.5,
		SchemaEnrichment:   true,
	}
	rec := &models.CapabilityRecord{
		ID:                  "1",
		Kind:                models.CapabilityKindTool,
		SourceID:            "src1",
		SourceType:          "gateway",
		CanonicalName:       "src1__search",
		OriginalName:        "search",
		OriginalDescription: "Utility tool",
		InputSchemaJSON:     `{"type":"object","properties":{"query":{"type":"string"},"owner":{"type":"string"}}}`,
		Tags:                []string{"query", "owner"},
		VersionHash:         "v1",
		LastSeenAt:          time.Now().UTC(),
	}
	var pending []pendingRow
	err = ix.enrichAndAppend(ctx, rec, &pending)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	if !fake.called {
		t.Fatal("expected summarizer to be called for vague description")
	}
	if !strings.Contains(fake.last.OriginalDescription, "Original description (VAGUE") {
		t.Fatalf("expected refinement prompt, got %q", fake.last.OriginalDescription)
	}
	if rec.GeneratedSummary != fake.out {
		t.Fatalf("generated summary = %q want %q", rec.GeneratedSummary, fake.out)
	}
	if len(pending) != 1 {
		t.Fatalf("pending rows = %d want 1", len(pending))
	}
}
