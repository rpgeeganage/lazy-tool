package catalog

import (
	"context"
	"time"

	"lazy-tool/internal/app"
	"lazy-tool/internal/storage"
	"lazy-tool/pkg/models"
)

// InspectView is JSON for inspect surfaces (CLI, Web, TUI): capability plus config and reindex trust fields.
type InspectView struct {
	Record      models.CapabilityRecord `json:"record"`
	Source      *models.Source          `json:"source,omitempty"`
	LastReindex *ReindexHealthView      `json:"last_reindex,omitempty"`
	// SourceInConfig is true when the capability's source_id appears in the loaded registry.
	SourceInConfig bool `json:"source_in_config"`
	// LastReindexRecorded is true when SQLite has a source_health row for this source (even if LastReindex is nil due to read errors).
	LastReindexRecorded bool `json:"last_reindex_recorded"`
	// EmbeddingText is the effective text that would be (or was) sent to the embedder.
	EmbeddingText string `json:"embedding_text,omitempty"`
	// EmbeddingTextHash is the SHA-256 of EmbeddingText, matching the stored value.
	EmbeddingTextHash string `json:"embedding_text_hash,omitempty"`
}

// ReindexHealthView is last persisted reindex outcome for the capability's source.
type ReindexHealthView struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"` // RFC3339 UTC
}

// BuildInspectView loads the catalog row and joins registry source config plus source_health.
func BuildInspectView(ctx context.Context, store *storage.SQLiteStore, reg *app.SourceRegistry, canonicalName string) (InspectView, error) {
	rec, err := store.GetByCanonicalName(ctx, canonicalName)
	if err != nil {
		return InspectView{}, err
	}
	v := InspectView{
		Record:        rec,
		EmbeddingText: BuildEmbeddingText(&rec),
	}
	v.EmbeddingTextHash = ComputeEmbeddingTextHash(v.EmbeddingText)
	if reg != nil {
		if src, ok := reg.Get(rec.SourceID); ok {
			cp := src
			v.Source = &cp
			v.SourceInConfig = true
		}
	}
	if h, ok, err := store.GetSourceHealth(ctx, rec.SourceID); err == nil && ok {
		v.LastReindexRecorded = true
		v.LastReindex = &ReindexHealthView{
			OK:        h.OK,
			Message:   h.Message,
			UpdatedAt: h.UpdatedAt.UTC().Format(time.RFC3339),
		}
	}
	return v, nil
}
