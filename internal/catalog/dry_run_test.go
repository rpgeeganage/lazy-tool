package catalog

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"lazy-tool/internal/app"
	"lazy-tool/internal/connectors"
	"lazy-tool/internal/storage"
	"lazy-tool/internal/summarizer"
	"lazy-tool/pkg/models"
)

func TestDryRunReportsPerRecordChanges(t *testing.T) {
	ctx := context.Background()
	st, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "dryrun.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	now := time.Now().UTC()
	if err := st.UpsertCapability(ctx, models.CapabilityRecord{
		ID:                  CapabilityID("src1", "existing"),
		Kind:                models.CapabilityKindTool,
		SourceID:            "src1",
		SourceType:          string(models.SourceTypeGateway),
		CanonicalName:       "src1__existing",
		OriginalName:        "existing",
		OriginalDescription: "Old tool",
		InputSchemaJSON:     `{"type":"object"}`,
		MetadataJSON:        "{}",
		VersionHash:         "old-hash",
		LastSeenAt:          now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertCapability(ctx, models.CapabilityRecord{
		ID:                  CapabilityID("src1", "stale"),
		Kind:                models.CapabilityKindTool,
		SourceID:            "src1",
		SourceType:          string(models.SourceTypeGateway),
		CanonicalName:       "src1__stale",
		OriginalName:        "stale",
		OriginalDescription: "Stale tool",
		InputSchemaJSON:     `{"type":"object"}`,
		MetadataJSON:        "{}",
		VersionHash:         "stale-hash",
		LastSeenAt:          now,
	}); err != nil {
		t.Fatal(err)
	}
	reg, err := app.NewSourceRegistry([]models.Source{{ID: "src1", Type: models.SourceTypeGateway, Transport: models.TransportHTTP}})
	if err != nil {
		t.Fatal(err)
	}
	ix := &Indexer{
		Registry: reg,
		Factory: stubIndexerFactory{conn: stubIndexerConnector{snap: &connectors.IndexSnapshot{
			Tools: []connectors.ToolMeta{
				{Name: "existing", Description: "Updated tool", InputSchema: []byte(`{"type":"object"}`)},
				{Name: "newtool", Description: "Brand new tool", InputSchema: []byte(`{"type":"object"}`)},
			},
		}}},
		Summary: summarizer.Noop{},
		Store:   st,
	}
	result, err := ix.DryRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PerSource) != 1 {
		t.Fatalf("sources = %d want 1", len(result.PerSource))
	}
	changes := result.PerSource[0].Changes
	if len(changes) != 3 {
		t.Fatalf("changes = %d want 3 (%#v)", len(changes), changes)
	}
	want := []struct {
		status string
		name   string
	}{
		{status: "NEW", name: "src1__newtool"},
		{status: "STALE", name: "src1__stale"},
		{status: "UPDATED", name: "src1__existing"},
	}
	for _, exp := range want {
		found := false
		for _, ch := range changes {
			if ch.Status == exp.status && ch.CanonicalName == exp.name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing change %s %s in %#v", exp.status, exp.name, changes)
		}
	}
}
