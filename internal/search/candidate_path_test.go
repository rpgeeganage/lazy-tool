package search

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"lazy-tool/internal/embeddings"
	"lazy-tool/internal/metrics"
	"lazy-tool/internal/storage"
	"lazy-tool/pkg/models"
)

func TestSearch_candidatePath_substringMatrix(t *testing.T) {
	type row struct {
		name    string
		query   string
		want    string
		fixture models.CapabilityRecord
	}
	rows := []row{
		{
			name:  "fts_sparse_augments_with_substring",
			query: "create github issue",
			want:  models.SearchCandidatePathSubstringAugmentedFTSSparse,
			fixture: models.CapabilityRecord{
				ID: "1", Kind: models.CapabilityKindTool, SourceID: "github-gateway", SourceType: "gateway",
				CanonicalName: "github_gateway__create_issue", OriginalName: "create_issue",
				OriginalDescription: "Create an issue in a repo",
				GeneratedSummary:    "Creates GitHub issues with title and body.",
				SearchText:            "github-gateway create_issue repo title body issue",
				VersionHash:           "h1", LastSeenAt: time.Now(),
			},
		},
		{
			name:  "no_fts_tokens_full_catalog",
			query: "z",
			want:  models.SearchCandidatePathSubstringFullCatalogNoFTSMatch,
			fixture: models.CapabilityRecord{
				ID: "2", Kind: models.CapabilityKindTool, SourceID: "s", SourceType: "gateway",
				CanonicalName: "s__x", OriginalName: "x",
				GeneratedSummary: "hi", SearchText: "s x tool z marker",
				VersionHash: "1", LastSeenAt: time.Now(), InputSchemaJSON: "{}", MetadataJSON: "{}",
			},
		},
		{
			name:  "fts_non_empty_match_zero_rows",
			query: "alpha phantomzz",
			want:  models.SearchCandidatePathSubstringFullCatalogFTSZeroRows,
			fixture: models.CapabilityRecord{
				ID: "3", Kind: models.CapabilityKindTool, SourceID: "s2", SourceType: "gateway",
				CanonicalName: "s2__y", OriginalName: "y",
				GeneratedSummary: "beta", SearchText: "s2 y alpha beta gamma",
				VersionHash: "1", LastSeenAt: time.Now(), InputSchemaJSON: "{}", MetadataJSON: "{}",
			},
		},
		{
			name:  "conversational_query_uses_zero_row_fallback",
			query: "create a new Word document populated with a summary of this conversation",
			want:  models.SearchCandidatePathSubstringFullCatalogFTSZeroRows,
			fixture: models.CapabilityRecord{
				ID: "4", Kind: models.CapabilityKindTool, SourceID: "office", SourceType: "server",
				CanonicalName: "office__word_from_markdown", OriginalName: "word_from_markdown",
				GeneratedSummary: "Creates a new Word document populated from Markdown summary, conversation summary, notes, or report content.",
				SearchText:       "office word_from_markdown creates a new word document populated from markdown summary conversation summary notes report content",
				VersionHash:      "1", LastSeenAt: time.Now(), InputSchemaJSON: "{}", MetadataJSON: "{}",
			},
		},
	}

	// When limit=1 and FTS returns 1 hit, substring scan is skipped (FTS has enough).
	t.Run("fts_sufficient_skips_substring", func(t *testing.T) {
		var mode string
		prev := metrics.SearchCandidateGeneration
		metrics.SearchCandidateGeneration = func(m string) { mode = m }
		defer func() { metrics.SearchCandidateGeneration = prev }()

		p := filepath.Join(t.TempDir(), "substr.db")
		st, err := storage.OpenSQLite(p)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = st.Close() }()
		ctx := context.Background()
		rec := models.CapabilityRecord{
			ID: "s1", Kind: models.CapabilityKindTool, SourceID: "github-gateway", SourceType: "gateway",
			CanonicalName: "github_gateway__create_issue", OriginalName: "create_issue",
			OriginalDescription: "Create an issue in a repo",
			GeneratedSummary:    "Creates GitHub issues with title and body.",
			SearchText:          "github-gateway create_issue repo title body issue",
			VersionHash:         "h1", LastSeenAt: time.Now(),
			InputSchemaJSON:     "{}", MetadataJSON: "{}",
		}
		if err := st.UpsertCapability(ctx, rec); err != nil {
			t.Fatal(err)
		}
		svc := NewService(st, nil, embeddings.Noop{}, ScoreWeights{}, false)
		// limit=1: FTS returns 1 hit which is >= limit, so substring is skipped
		ranked, err := svc.Search(ctx, models.SearchQuery{Text: "create github issue", Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if mode != models.SearchCandidatePathSubstringSkippedFTSHit {
			t.Fatalf("metrics path: got %q want %q", mode, models.SearchCandidatePathSubstringSkippedFTSHit)
		}
		if ranked.CandidatePath != models.SearchCandidatePathSubstringSkippedFTSHit {
			t.Fatalf("CandidatePath: got %q want %q", ranked.CandidatePath, models.SearchCandidatePathSubstringSkippedFTSHit)
		}
	})

	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			var mode string
			prev := metrics.SearchCandidateGeneration
			metrics.SearchCandidateGeneration = func(m string) { mode = m }
			defer func() { metrics.SearchCandidateGeneration = prev }()

			p := filepath.Join(t.TempDir(), "substr.db")
			st, err := storage.OpenSQLite(p)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			ctx := context.Background()
			rec := tc.fixture
			if rec.InputSchemaJSON == "" {
				rec.InputSchemaJSON = "{}"
			}
			if rec.MetadataJSON == "" {
				rec.MetadataJSON = "{}"
			}
			if err := st.UpsertCapability(ctx, rec); err != nil {
				t.Fatal(err)
			}
			svc := NewService(st, nil, embeddings.Noop{}, ScoreWeights{}, false)
			ranked, err := svc.Search(ctx, models.SearchQuery{Text: tc.query, Limit: 5})
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "conversational_query_uses_zero_row_fallback" {
				if len(ranked.Results) == 0 || ranked.Results[0].ProxyToolName != "office__word_from_markdown" {
					t.Fatalf("expected office__word_from_markdown top hit, got %#v", ranked.Results)
				}
			}
			if mode != tc.want {
				t.Fatalf("metrics path: got %q want %q", mode, tc.want)
			}
			if ranked.CandidatePath != tc.want {
				t.Fatalf("RankedResults.CandidatePath: got %q want %q", ranked.CandidatePath, tc.want)
			}
		})
	}
}
