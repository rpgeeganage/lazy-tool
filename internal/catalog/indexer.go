package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"lazy-tool/internal/app"
	"lazy-tool/internal/connectors"
	"lazy-tool/internal/embeddings"
	"lazy-tool/internal/metrics"
	"lazy-tool/internal/storage"
	"lazy-tool/internal/summarizer"
	"lazy-tool/internal/vector"
	"lazy-tool/pkg/models"
)

type Indexer struct {
	Registry *app.SourceRegistry
	Factory  connectors.Factory
	Summary  summarizer.Summarizer
	Embed    embeddings.Embedder
	Store    *storage.SQLiteStore
	Vec      *vector.Index
	Log      *slog.Logger
}

type pendingRow struct {
	rec      models.CapabilityRecord
	reuseEmb bool
}

func (ix *Indexer) Run(ctx context.Context) error {
	if ix.Log == nil {
		ix.Log = slog.Default()
	}
	app.SetReindexStatus(true, "reindex running…")
	for _, src := range ix.Registry.AllConfigured() {
		if !src.Disabled {
			continue
		}
		n, err := ix.Store.DeleteAllCapabilitiesForSource(ctx, src.ID)
		if err != nil {
			ix.Log.Warn("delete capabilities for disabled source failed", "source", src.ID, "err", err)
			continue
		}
		if n > 0 {
			ix.Log.Info("removed capabilities for disabled source", "source", src.ID, "removed", n)
		}
	}
	sources := ix.Registry.All()
	failByID := make(map[string]error)
	for _, src := range sources {
		if err := ix.indexSource(ctx, src); err != nil {
			ix.Log.Error("index source failed", "source", src.ID, "err", err)
			failByID[src.ID] = err
			continue
		}
	}
	var srcErrs []error
	failedIDs := make([]string, 0, len(failByID))
	for id := range failByID {
		failedIDs = append(failedIDs, id)
	}
	sort.Strings(failedIDs)
	for _, id := range failedIDs {
		srcErrs = append(srcErrs, fmt.Errorf("%s: %w", id, failByID[id]))
	}
	all, err := ix.Store.ListAll(ctx)
	if err != nil {
		app.SetReindexStatus(false, err.Error())
		return err
	}
	for _, src := range sources {
		msg := ""
		ok := true
		if err, bad := failByID[src.ID]; bad {
			ok = false
			msg = err.Error()
		}
		if err := ix.Store.UpsertSourceHealth(ctx, src.ID, ok, msg); err != nil {
			ix.Log.Warn("persist source health failed", "source", src.ID, "err", err)
		}
	}
	vecNote := "no vector index"
	if ix.Vec != nil {
		embedModel := ""
		if ix.Embed != nil {
			embedModel = ix.Embed.ModelName()
		}
		skipped, err := ix.Vec.RebuildFromRecordsIfUnchanged(ctx, all, embedModel)
		if err != nil {
			app.SetReindexStatus(false, err.Error())
			return fmt.Errorf("rebuild vector index: %w", err)
		}
		if skipped {
			vecNote = "vector index unchanged (embedding fingerprint)"
		} else {
			vecNote = "vector index rebuilt"
		}
	}
	if len(srcErrs) > 0 {
		parts := make([]string, len(srcErrs))
		for i, e := range srcErrs {
			parts[i] = e.Error()
		}
		statusMsg := fmt.Sprintf("partial: %d/%d source(s) failed — %s — %d capabilities (%s)",
			len(srcErrs), len(sources), strings.Join(parts, "; "), len(all), vecNote)
		app.SetReindexStatus(false, statusMsg)
		ix.Log.Warn("reindex incomplete", "failed_sources", len(srcErrs), "capabilities", len(all))
		return fmt.Errorf("reindex incomplete: %w", errors.Join(srcErrs...))
	}
	msg := fmt.Sprintf("ok — %d capabilities (%s)", len(all), vecNote)
	app.SetReindexStatus(true, msg)
	ix.Log.Info("reindex complete", "capabilities", len(all))
	return nil
}

// DryRunResult summarizes what a reindex would do without writing anything.
type DryRunResult struct {
	PerSource []DryRunSourceResult
}

// DryRunSourceResult is the diff for one source.
type DryRunSourceResult struct {
	SourceID  string
	New       int
	Updated   int
	Unchanged int
	Stale     int
	Error     error
}

// DryRun connects to all sources and computes what a reindex would change, without writing to the database.
func (ix *Indexer) DryRun(ctx context.Context) (DryRunResult, error) {
	if ix.Log == nil {
		ix.Log = slog.Default()
	}
	sources := ix.Registry.All()
	var result DryRunResult
	for _, src := range sources {
		sr := ix.dryRunSource(ctx, src)
		result.PerSource = append(result.PerSource, sr)
	}
	return result, nil
}

func (ix *Indexer) dryRunSource(ctx context.Context, src models.Source) DryRunSourceResult {
	sr := DryRunSourceResult{SourceID: src.ID}
	conn, err := ix.Factory.New(ctx, src)
	if err != nil {
		sr.Error = err
		return sr
	}
	defer func() { _ = conn.Close() }()

	snap, err := conn.ListForIndex(ctx)
	if err != nil {
		sr.Error = err
		return sr
	}

	now := time.Now()
	seen := make(map[string]struct{})

	checkRecord := func(rec models.CapabilityRecord) {
		seen[rec.ID] = struct{}{}
		old, gerr := ix.Store.GetCapability(ctx, rec.ID)
		if errors.Is(gerr, sql.ErrNoRows) {
			sr.New++
		} else if gerr != nil {
			sr.New++ // assume new on error
		} else if old.VersionHash != rec.VersionHash {
			sr.Updated++
		} else {
			sr.Unchanged++
		}
	}

	for _, meta := range snap.Tools {
		checkRecord(NormalizeTool(src, meta, now))
	}
	if snap.PromptsErr == nil {
		for _, meta := range snap.Prompts {
			checkRecord(NormalizePrompt(src, meta, now))
		}
	}
	if snap.ResourcesErr == nil {
		for _, meta := range snap.Resources {
			checkRecord(NormalizeResource(src, meta, now))
		}
	}
	if snap.ResourceTemplatesErr == nil {
		for _, meta := range snap.ResourceTemplates {
			checkRecord(NormalizeResourceTemplate(src, meta, now))
		}
	}

	// Count stale: existing records for this source that weren't seen upstream.
	existing, err := ix.Store.ListBySource(ctx, src.ID)
	if err == nil {
		for _, rec := range existing {
			if _, ok := seen[rec.ID]; !ok {
				sr.Stale++
			}
		}
	}

	return sr
}

func (ix *Indexer) enrichAndAppend(ctx context.Context, rec *models.CapabilityRecord, pending *[]pendingRow) error {
	old, gerr := ix.Store.GetCapability(ctx, rec.ID)
	if gerr == nil {
		rec.UserSummary = old.UserSummary
	}
	if gerr == nil && old.VersionHash == rec.VersionHash && old.GeneratedSummary != "" {
		rec.GeneratedSummary = old.GeneratedSummary
	} else if gerr != nil && !errors.Is(gerr, sql.ErrNoRows) {
		return gerr
	} else {
		sum, serr := ix.Summary.Summarize(ctx, *rec)
		if serr != nil {
			ix.Log.Warn("summary failed", "kind", rec.Kind, "name", rec.OriginalName, "err", serr)
			sum = rec.OriginalDescription
		}
		rec.GeneratedSummary = sum
	}
	RefreshSearchText(rec)

	// Compute embedding text hash for cache invalidation.
	// Reuse the old embedding only when the model matches AND the embedding text
	// hasn't changed (catches user_summary edits that VersionHash misses).
	embText := BuildEmbeddingText(rec)
	embTextHash := ComputeEmbeddingTextHash(embText)
	rec.EmbeddingTextHash = embTextHash

	reuseEmb := ix.Embed != nil && gerr == nil &&
		old.EmbeddingModel == ix.Embed.ModelName() &&
		len(old.EmbeddingVector) > 0 &&
		old.EmbeddingTextHash != "" &&
		old.EmbeddingTextHash == embTextHash
	if reuseEmb {
		rec.EmbeddingVector = old.EmbeddingVector
		rec.EmbeddingModel = old.EmbeddingModel
	}
	*pending = append(*pending, pendingRow{rec: *rec, reuseEmb: reuseEmb})
	return nil
}

func (ix *Indexer) flushPending(ctx context.Context, src models.Source, pending []pendingRow, keep map[string]struct{}) error {
	if ix.Embed != nil {
		var texts []string
		var needIdx []int
		for i := range pending {
			if !pending[i].reuseEmb {
				texts = append(texts, BuildEmbeddingText(&pending[i].rec))
				needIdx = append(needIdx, i)
			}
		}
		if len(texts) > 0 {
			embedStarted := time.Now()
			vecs, eerr := ix.Embed.Embed(ctx, texts)
			durationMS := time.Since(embedStarted).Milliseconds()
			metrics.EmbeddingBatchDuration(src.ID, len(texts), durationMS, eerr != nil, eerr)
			if ix.Store != nil {
				_ = ix.Store.RecordOperation(ctx, storage.OperationLogEvent{
					Operation:  "embed",
					SourceID:   src.ID,
					DurationMS: durationMS,
					Metadata: map[string]any{
						"batch_size": len(texts),
						"fallback":   eerr != nil,
					},
					Error: errorString(eerr),
				})
			}
			if eerr != nil {
				ix.Log.Warn("batch embed failed, falling back per row", "source", src.ID, "err", eerr)
				for _, i := range needIdx {
					v, err2 := ix.Embed.Embed(ctx, []string{BuildEmbeddingText(&pending[i].rec)})
					if err2 != nil {
						ix.Log.Warn("embed failed", "kind", pending[i].rec.Kind, "name", pending[i].rec.OriginalName, "err", err2)
						continue
					}
					if len(v) > 0 && len(v[0]) > 0 {
						pending[i].rec.EmbeddingVector = v[0]
						pending[i].rec.EmbeddingModel = ix.Embed.ModelName()
					}
				}
			} else {
				for j, i := range needIdx {
					if j < len(vecs) && len(vecs[j]) > 0 {
						pending[i].rec.EmbeddingVector = vecs[j]
						pending[i].rec.EmbeddingModel = ix.Embed.ModelName()
					}
				}
			}
		}
	}

	for _, row := range pending {
		if err := ix.Store.UpsertCapability(ctx, row.rec); err != nil {
			return err
		}
		keep[row.rec.ID] = struct{}{}
	}
	return nil
}

func (ix *Indexer) indexSource(ctx context.Context, src models.Source) error {
	started := time.Now()
	recordDone := func(pendingCount, staleRemoved int, err error) {
		durationMS := time.Since(started).Milliseconds()
		metrics.ReindexSourceDone(src.ID, pendingCount, staleRemoved, err)
		metrics.ReindexSourceDuration(src.ID, durationMS, err)
		if ix.Store != nil {
			_ = ix.Store.RecordOperation(ctx, storage.OperationLogEvent{
				Operation:  "reindex",
				SourceID:   src.ID,
				DurationMS: durationMS,
				Metadata: map[string]any{
					"pending":       pendingCount,
					"stale_removed": staleRemoved,
				},
				Error: errorString(err),
			})
		}
	}
	conn, err := ix.Factory.New(ctx, src)
	if err != nil {
		recordDone(0, 0, err)
		return err
	}
	defer func() { _ = conn.Close() }()

	var pending []pendingRow
	keep := make(map[string]struct{})
	now := time.Now()

	snap, err := conn.ListForIndex(ctx)
	if err != nil {
		recordDone(0, 0, err)
		return err
	}
	for _, meta := range snap.Tools {
		rec := NormalizeTool(src, meta, now)
		if err := ix.enrichAndAppend(ctx, &rec, &pending); err != nil {
			return err
		}
	}

	if snap.PromptsErr != nil {
		ix.Log.Warn("list prompts skipped", "source", src.ID, "err", snap.PromptsErr)
	} else {
		for _, meta := range snap.Prompts {
			rec := NormalizePrompt(src, meta, now)
			if err := ix.enrichAndAppend(ctx, &rec, &pending); err != nil {
				return err
			}
		}
	}

	if snap.ResourcesErr != nil {
		ix.Log.Warn("list resources skipped", "source", src.ID, "err", snap.ResourcesErr)
	} else {
		for _, meta := range snap.Resources {
			rec := NormalizeResource(src, meta, now)
			if err := ix.enrichAndAppend(ctx, &rec, &pending); err != nil {
				return err
			}
		}
	}

	if snap.ResourceTemplatesErr != nil {
		ix.Log.Warn("list resource templates skipped", "source", src.ID, "err", snap.ResourceTemplatesErr)
	} else {
		for _, meta := range snap.ResourceTemplates {
			rec := NormalizeResourceTemplate(src, meta, now)
			if err := ix.enrichAndAppend(ctx, &rec, &pending); err != nil {
				return err
			}
		}
	}

	if err := ix.flushPending(ctx, src, pending, keep); err != nil {
		recordDone(len(pending), 0, err)
		return err
	}

	n, err := ix.Store.DeleteStale(ctx, src.ID, keep)
	if err != nil {
		recordDone(len(pending), 0, err)
		return err
	}
	recordDone(len(pending), n, nil)
	ix.Log.Info("indexed source", "source", src.ID, "rows", len(pending), "removed_stale", n)
	return nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
