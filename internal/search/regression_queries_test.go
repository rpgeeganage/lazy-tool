package search

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"lazy-tool/internal/embeddings"
	"lazy-tool/internal/storage"
	"lazy-tool/pkg/models"
)

func TestRegressionQuerySet(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantTop string
		records []models.CapabilityRecord
	}{
		{
			name:    "exact_canonical_office_word_from_markdown",
			query:   "office__word_from_markdown",
			wantTop: "office__word_from_markdown",
			records: []models.CapabilityRecord{
				capRec("word1", "office__word_from_markdown", "word_from_markdown", "Creates a new Word document populated from Markdown summary, conversation summary, notes, or report content.", "office word_from_markdown creates new word document populated markdown summary conversation notes report"),
				capRec("word2", "office__append_staffing_row_to_office_table", "append_staffing_row_to_office_table", "Appends a staffing row to an office table.", "office append_staffing_row_to_office_table staffing row table"),
			},
		},
		{
			name:    "exact_original_word_from_markdown",
			query:   "word_from_markdown",
			wantTop: "office__word_from_markdown",
			records: []models.CapabilityRecord{
				capRec("word1", "office__word_from_markdown", "word_from_markdown", "Creates a new Word document populated from Markdown summary, conversation summary, notes, or report content.", "office word_from_markdown creates new word document populated markdown summary conversation notes report"),
				capRec("word2", "office__word_template_placeholders", "word_template_placeholders", "Inspects a Word template and lists placeholders.", "office word_template_placeholders inspect template placeholders"),
			},
		},
		{
			name:    "exact_canonical_azure_query_prices",
			query:   "azure_query_prices",
			wantTop: "azure__azure_query_prices",
			records: []models.CapabilityRecord{
				capRec("az1", "azure__azure_query_prices", "azure_query_prices", "Queries cached Azure service pricing by sku, region, and quantity.", "azure azure_query_prices service sku region quantity price cached"),
				capRec("az2", "azure__firewall_premium_price", "firewall_premium_price", "Returns Azure firewall premium pricing.", "azure firewall premium price security tier"),
			},
		},
		{
			name:    "parameter_output_path_markdown_word",
			query:   "output_path markdown word",
			wantTop: "office__word_from_markdown",
			records: []models.CapabilityRecord{
				capRec("word1", "office__word_from_markdown", "word_from_markdown", "Creates a new Word document from Markdown input.", "office word_from_markdown output_path markdown word document"),
				capRec("word2", "office__read_word_template", "read_word_template", "Reads a Word template file.", "office read_word_template template file"),
			},
		},
		{
			name:    "parameter_service_sku_region_quantity",
			query:   "service sku region quantity",
			wantTop: "azure__azure_query_prices",
			records: []models.CapabilityRecord{
				capRec("az1", "azure__azure_query_prices", "azure_query_prices", "Queries cached Azure pricing for a service sku in a region.", "azure query prices service sku region quantity cached pricing"),
				capRec("az2", "azure__firewall_premium_price", "firewall_premium_price", "Returns Azure firewall premium pricing.", "azure firewall premium price security tier"),
			},
		},
		{
			name:    "parameter_speaker_notes_powerpoint",
			query:   "speaker notes powerpoint",
			wantTop: "office__read_powerpoint_speaker_notes",
			records: []models.CapabilityRecord{
				capRec("ppt1", "office__read_powerpoint_speaker_notes", "read_powerpoint_speaker_notes", "Reads speaker notes from a PowerPoint deck.", "office powerpoint speaker notes read deck slides"),
				capRec("ppt2", "office__word_from_markdown", "word_from_markdown", "Creates a new Word document from Markdown input.", "office word_from_markdown markdown summary report"),
			},
		},
		{
			name:    "paraphrase_summary_conversation_to_word",
			query:   "create a new Word document populated with a summary of this conversation",
			wantTop: "office__word_from_markdown",
			records: []models.CapabilityRecord{
				capRec("word1", "office__word_from_markdown", "word_from_markdown", "Creates a new Word document populated from Markdown summary, conversation summary, notes, or report content.", "office word_from_markdown summary conversation markdown report"),
				capRec("word2", "office__read_powerpoint_speaker_notes", "read_powerpoint_speaker_notes", "Reads speaker notes from a PowerPoint deck.", "office powerpoint speaker notes read deck slides"),
			},
		},
		{
			name:    "paraphrase_read_powerpoint_speaker_notes",
			query:   "read powerpoint speaker notes",
			wantTop: "office__read_powerpoint_speaker_notes",
			records: []models.CapabilityRecord{
				capRec("ppt1", "office__read_powerpoint_speaker_notes", "read_powerpoint_speaker_notes", "Reads speaker notes from a PowerPoint deck.", "office powerpoint speaker notes read deck slides"),
				capRec("word1", "office__word_from_markdown", "word_from_markdown", "Creates a new Word document populated from Markdown summary content.", "office word_from_markdown markdown summary report"),
			},
		},
		{
			name:    "paraphrase_calculate_azure_vm_monthly_cost_from_cached_prices",
			query:   "calculate azure vm monthly cost from cached prices",
			wantTop: "azure__azure_query_prices",
			records: []models.CapabilityRecord{
				capRec("az1", "azure__azure_query_prices", "azure_query_prices", "Calculates Azure VM monthly cost from cached pricing data.", "azure vm monthly cost cached prices service sku region quantity"),
				capRec("az2", "azure__firewall_premium_price", "firewall_premium_price", "Returns Azure firewall premium pricing.", "azure firewall premium price security tier"),
			},
		},
		{
			name:    "conversational_firewall_premium_price",
			query:   "firewall premium price",
			wantTop: "azure__firewall_premium_price",
			records: []models.CapabilityRecord{
				capRec("az1", "azure__firewall_premium_price", "firewall_premium_price", "Returns Azure firewall premium pricing.", "azure firewall premium price security tier"),
				capRec("az2", "azure__azure_query_prices", "azure_query_prices", "Queries cached Azure pricing by service sku and region.", "azure query prices service sku region quantity cached pricing"),
			},
		},
		{
			name:    "conversational_copy_template_and_inspect_placeholders_in_word_sow",
			query:   "copy template and inspect placeholders in word sow",
			wantTop: "office__word_template_placeholders",
			records: []models.CapabilityRecord{
				capRec("word1", "office__word_template_placeholders", "word_template_placeholders", "Copies a Word template and inspects placeholders in a statement of work.", "office word template placeholders copy inspect sow"),
				capRec("word2", "office__word_from_markdown", "word_from_markdown", "Creates a new Word document from Markdown input.", "office word_from_markdown markdown summary report"),
			},
		},
		{
			name:    "conversational_append_staffing_row_to_office_table",
			query:   "append staffing row to office table",
			wantTop: "office__append_staffing_row_to_office_table",
			records: []models.CapabilityRecord{
				capRec("office1", "office__append_staffing_row_to_office_table", "append_staffing_row_to_office_table", "Appends a staffing row to an office table.", "office staffing row table append"),
				capRec("word1", "office__word_from_markdown", "word_from_markdown", "Creates a new Word document from Markdown input.", "office word_from_markdown markdown summary report"),
			},
		},
		{
			name:    "ambiguous_markdown_report",
			query:   "markdown report",
			wantTop: "office__word_from_markdown",
			records: []models.CapabilityRecord{
				capRec("word1", "office__word_from_markdown", "word_from_markdown", "Creates a new Word document from Markdown report content.", "office word_from_markdown markdown report summary"),
				capRec("docs1", "docs__read_document_fully", "read_document_fully", "Reads a document end to end.", "docs read document fully content"),
			},
		},
		{
			name:    "ambiguous_azure_pricing",
			query:   "azure pricing",
			wantTop: "azure__azure_query_prices",
			records: []models.CapabilityRecord{
				capRec("az1", "azure__azure_query_prices", "azure_query_prices", "Queries cached Azure pricing by service and region.", "azure pricing query prices cached service sku region"),
				capRec("az2", "azure__firewall_premium_price", "firewall_premium_price", "Returns Azure firewall premium pricing.", "azure firewall premium price security tier"),
			},
		},
		{
			name:    "ambiguous_read_docs_fully",
			query:   "read docs fully",
			wantTop: "docs__read_document_fully",
			records: []models.CapabilityRecord{
				capRec("docs1", "docs__read_document_fully", "read_document_fully", "Reads documents fully and returns the full text.", "docs read document fully entire content"),
				capRec("word1", "office__word_from_markdown", "word_from_markdown", "Creates a new Word document from Markdown input.", "office word_from_markdown markdown summary report"),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "s.db")
			st, err := storage.OpenSQLite(p)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()

			ctx := context.Background()
			for _, rec := range tc.records {
				if rec.InputSchemaJSON == "" {
					rec.InputSchemaJSON = "{}"
				}
				if rec.MetadataJSON == "" {
					rec.MetadataJSON = "{}"
				}
				if rec.VersionHash == "" {
					rec.VersionHash = "1"
				}
				if rec.LastSeenAt.IsZero() {
					rec.LastSeenAt = time.Now()
				}
				if err := st.UpsertCapability(ctx, rec); err != nil {
					t.Fatal(err)
				}
			}

			svc := NewService(st, nil, embeddings.Noop{}, DefaultScoreWeights(), false)
			out, err := svc.Search(ctx, models.SearchQuery{Text: tc.query, Limit: 5})
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Results) == 0 {
				t.Fatal("no results")
			}
			if out.Results[0].ProxyToolName != tc.wantTop {
				t.Fatalf("expected %s first, got %+v", tc.wantTop, out.Results)
			}
		})
	}
}

func capRec(id, canonical, original, summary, searchText string) models.CapabilityRecord {
	return models.CapabilityRecord{
		ID:                  id,
		Kind:                models.CapabilityKindTool,
		SourceID:            canonicalSourceID(canonical),
		SourceType:          "server",
		CanonicalName:       canonical,
		OriginalName:        original,
		GeneratedSummary:    summary,
		SearchText:          searchText,
		VersionHash:         "1",
		LastSeenAt:          time.Now(),
		InputSchemaJSON:     "{}",
		MetadataJSON:        "{}",
	}
}

func canonicalSourceID(canonical string) string {
	for i := 0; i < len(canonical)-1; i++ {
		if canonical[i] == '_' && canonical[i+1] == '_' {
			return canonical[:i]
		}
	}
	return "search"
}
