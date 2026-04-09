package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"lazy-tool/pkg/models"
)

func TestListIDsBySearchTextTokenConjunction(t *testing.T) {
	p := filepath.Join(t.TempDir(), "substr-conjunction.db")
	st, err := OpenSQLite(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	rec := models.CapabilityRecord{
		ID:                  "1",
		Kind:                models.CapabilityKindTool,
		SourceID:            "office",
		SourceType:          "server",
		CanonicalName:       "office__word_from_markdown",
		OriginalName:        "word_from_markdown",
		OriginalDescription: "Create a Word document from Markdown.",
		GeneratedSummary:    "Creates Word documents from Markdown.",
		SearchText:          "creates a new word document populated from markdown summary conversation summary notes report content",
		InputSchemaJSON:     "{}",
		MetadataJSON:        "{}",
		VersionHash:         "v1",
		LastSeenAt:          time.Now(),
	}
	if err := st.UpsertCapability(ctx, rec); err != nil {
		t.Fatal(err)
	}

	ids, err := st.ListIDsBySearchTextTokenConjunction(ctx, []string{"conversation", "summary", "word", "document"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != rec.ID {
		t.Fatalf("unexpected ids: %#v", ids)
	}
}
