package catalog

import (
	"slices"
	"strings"
	"testing"

	"lazy-tool/pkg/models"
)

func TestScoreVagueness_FlagsVagueDescriptions(t *testing.T) {
	rec := &models.CapabilityRecord{
		Kind:                models.CapabilityKindTool,
		OriginalName:        "helper",
		OriginalDescription: "Helper tool",
		Tags:                []string{"query"},
	}
	score := ScoreVagueness(rec)
	if score.Score < 0.5 {
		t.Fatalf("expected vague score >= 0.5, got %#v", score)
	}
	if !slices.Contains(score.Reasons, "too_short") {
		t.Fatalf("expected too_short reason, got %#v", score.Reasons)
	}
}

func TestSchemaSignals_InferActionHints(t *testing.T) {
	schema := `{"type":"object","properties":{"query":{"type":"string"},"url":{"type":"string"},"path":{"type":"string"},"content":{"type":"string"}}}`
	got := SchemaSignals(schema)
	for _, want := range []string{"searches", "fetches", "writes"} {
		if !slices.Contains(got, want) {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
}

func TestBuildEmbeddingTextWithStrategy_AutoPrefersSummaryWhenVague(t *testing.T) {
	rec := &models.CapabilityRecord{
		Kind:                models.CapabilityKindTool,
		OriginalName:        "manage_repo",
		OriginalDescription: "Manages resources",
		GeneratedSummary:    "Searches repositories using owner and repo filters.",
		Tags:                []string{"owner", "repo", "query"},
	}
	text := BuildEmbeddingTextWithStrategy(rec, "auto")
	if !strings.Contains(text, "Searches repositories using owner and repo filters.") {
		t.Fatalf("expected summary-first auto text, got %q", text)
	}
	if !strings.Contains(text, "Parameters: owner, repo, query") {
		t.Fatalf("expected parameters in text, got %q", text)
	}
}

func TestEnrichRecord_AppendsSchemaEnrichmentForVagueSummary(t *testing.T) {
	rec := &models.CapabilityRecord{
		Kind:                models.CapabilityKindTool,
		OriginalName:        "run_query",
		OriginalDescription: "Utility tool",
		GeneratedSummary:    "Utility tool",
		Tags:                []string{"query", "repo"},
	}
	enrichRecord(rec)
	if !strings.Contains(strings.ToLower(rec.GeneratedSummary), "searches") {
		t.Fatalf("expected schema enrichment in summary, got %q", rec.GeneratedSummary)
	}
	if !slices.Contains(rec.Tags, "searches") {
		t.Fatalf("expected inferred action tag, got %v", rec.Tags)
	}
}
