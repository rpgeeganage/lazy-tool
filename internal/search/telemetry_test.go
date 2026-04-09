package search

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"lazy-tool/internal/embeddings"
	"lazy-tool/internal/storage"
	"lazy-tool/internal/vector"
	"lazy-tool/pkg/models"
)

func TestSearchRecordsSearchAndVectorTelemetry(t *testing.T) {
	ctx := context.Background()
	st, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "search-telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	vi, err := vector.NewInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = vi.Close() }()

	rec := models.CapabilityRecord{
		ID:                  "1",
		Kind:                models.CapabilityKindTool,
		SourceID:            "src1",
		SourceType:          "gateway",
		CanonicalName:       "src1__echo",
		OriginalName:        "echo",
		OriginalDescription: "Echo input",
		GeneratedSummary:    "Echoes text back.",
		SearchText:          "echo input output",
		InputSchemaJSON:     "{}",
		MetadataJSON:        "{}",
		VersionHash:         "v1",
		LastSeenAt:          time.Now().UTC(),
		EmbeddingModel:      "test-model",
		EmbeddingVector:     []float32{1, 0, 0},
	}
	if err := st.UpsertCapability(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := vi.RebuildFromRecords(ctx, []models.CapabilityRecord{rec}); err != nil {
		t.Fatal(err)
	}

	svc := NewService(st, vi, embeddings.Noop{}, ScoreWeights{}, false)
	_, err = svc.Search(ctx, models.SearchQuery{Text: "echo", Limit: 5, Embedding: []float32{1, 0, 0}, HasEmbedding: true})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListRecentOperations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.Operation] = true
	}
	if !seen["search"] {
		t.Fatal("expected search telemetry row")
	}
	if !seen["vector_query"] {
		t.Fatal("expected vector_query telemetry row")
	}
}
