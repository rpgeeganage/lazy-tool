package catalog

import (
	"strings"
	"testing"
	"time"

	"lazy-tool/internal/connectors"
	"lazy-tool/pkg/models"
)

func TestNormalizeTool_tagsAndSearchText(t *testing.T) {
	src := models.Source{ID: "gh", Type: models.SourceTypeGateway, Transport: models.TransportHTTP}
	meta := connectors.ToolMeta{
		Name:        "create_issue",
		Description: "open issue",
		InputSchema: []byte(`{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"}}}`),
	}
	rec := NormalizeTool(src, meta, time.Now())
	if len(rec.Tags) < 2 {
		t.Fatalf("tags: %v", rec.Tags)
	}
	if !strings.Contains(rec.SearchText, "title") || !strings.Contains(rec.SearchText, "gh") {
		t.Fatalf("search text missing expected tokens: %q", rec.SearchText)
	}
}

func TestSanitizeSegment_preservesHyphenUnderscoreDistinction(t *testing.T) {
	// These must produce different segments so canonical names don't collide.
	cases := []struct {
		a, b string
	}{
		{"my-gw", "my_gw"},
		{"foo-bar", "foo_bar"},
		{"a-b-c", "a_b_c"},
	}
	for _, tc := range cases {
		sa := SanitizeSegment(tc.a)
		sb := SanitizeSegment(tc.b)
		if sa == sb {
			t.Errorf("SanitizeSegment(%q) == SanitizeSegment(%q) == %q; want different segments", tc.a, tc.b, sa)
		}
	}
}

func TestSanitizeSegment_noDoubleUnderscore(t *testing.T) {
	// No segment should ever contain "__" because that's the canonical name delimiter.
	inputs := []string{"foo__bar", "a___b", "x____y", "hello  world", "a..b"}
	for _, in := range inputs {
		out := SanitizeSegment(in)
		if strings.Contains(out, "__") {
			t.Errorf("SanitizeSegment(%q) = %q; must not contain double underscore", in, out)
		}
	}
}

func TestNormalizeTool_noCanonicalCollision(t *testing.T) {
	now := time.Now()
	srcA := models.Source{ID: "my-gw", Type: models.SourceTypeGateway, Transport: models.TransportHTTP}
	srcB := models.Source{ID: "my_gw", Type: models.SourceTypeGateway, Transport: models.TransportHTTP}
	meta := connectors.ToolMeta{Name: "echo", Description: "echo tool"}

	recA := NormalizeTool(srcA, meta, now)
	recB := NormalizeTool(srcB, meta, now)
	if recA.CanonicalName == recB.CanonicalName {
		t.Fatalf("canonical collision: source %q and %q both produce %q", srcA.ID, srcB.ID, recA.CanonicalName)
	}
}

func TestNormalizePrompt_kindAndCanonical(t *testing.T) {
	src := models.Source{ID: "gw", Type: models.SourceTypeGateway, Transport: models.TransportHTTP}
	meta := connectors.PromptMeta{
		Name:          "review",
		Description:   "Code review prompt",
		ArgumentsJSON: []byte(`[{"name":"path","required":true}]`),
	}
	rec := NormalizePrompt(src, meta, time.Now())
	if rec.Kind != models.CapabilityKindPrompt {
		t.Fatalf("kind %q", rec.Kind)
	}
	if !strings.Contains(rec.CanonicalName, "__p_") || !strings.Contains(rec.CanonicalName, "review") {
		t.Fatalf("canonical: %q", rec.CanonicalName)
	}
}

func TestBuildEmbeddingText_basic(t *testing.T) {
	rec := models.CapabilityRecord{
		Kind:                models.CapabilityKindTool,
		OriginalName:        "search_docs",
		OriginalDescription: "Search documentation. Returns matching pages.",
		GeneratedSummary:    "Searches Microsoft documentation for a given query string.",
		Tags:                []string{"query", "top_k"},
	}
	text := BuildEmbeddingText(&rec)
	// Should contain kind, name, summary (first sentence), and parameters.
	if !strings.Contains(text, "tool search_docs:") {
		t.Errorf("missing kind+name prefix: %q", text)
	}
	if !strings.Contains(text, "Searches Microsoft documentation") {
		t.Errorf("missing summary: %q", text)
	}
	if !strings.Contains(text, "Parameters: query, top_k") {
		t.Errorf("missing parameters: %q", text)
	}
	// Should NOT contain raw JSON.
	if strings.Contains(text, "{") || strings.Contains(text, "}") {
		t.Errorf("embedding text contains JSON noise: %q", text)
	}
}

func TestBuildEmbeddingText_userSummaryWithKeywords(t *testing.T) {
	rec := models.CapabilityRecord{
		Kind:             models.CapabilityKindTool,
		OriginalName:     "search_docs",
		GeneratedSummary: "Generated summary.",
		UserSummary:      "Searches Microsoft Learn docs for Azure services. [keywords: azure, firewall, nsg, docs]",
		Tags:             []string{"query"},
	}
	text := BuildEmbeddingText(&rec)
	// Should use UserSummary (first sentence) over GeneratedSummary.
	if !strings.Contains(text, "Searches Microsoft Learn docs for Azure services.") {
		t.Errorf("should use user summary first sentence: %q", text)
	}
	// Keywords should be extracted and appended.
	if !strings.Contains(text, "Keywords: azure, firewall, nsg, docs") {
		t.Errorf("missing keywords section: %q", text)
	}
}

func TestBuildEmbeddingText_fallbackToDescription(t *testing.T) {
	rec := models.CapabilityRecord{
		Kind:                models.CapabilityKindTool,
		OriginalName:        "echo",
		OriginalDescription: "Echoes input back",
	}
	text := BuildEmbeddingText(&rec)
	if !strings.Contains(text, "Echoes input back") {
		t.Errorf("should fall back to description: %q", text)
	}
}

func TestBuildEmbeddingText_noTags(t *testing.T) {
	rec := models.CapabilityRecord{
		Kind:                models.CapabilityKindPrompt,
		OriginalName:        "review",
		OriginalDescription: "Code review prompt",
	}
	text := BuildEmbeddingText(&rec)
	if strings.Contains(text, "Parameters:") {
		t.Errorf("should not include Parameters when no tags: %q", text)
	}
}

func TestComputeEmbeddingTextHash_deterministic(t *testing.T) {
	h1 := ComputeEmbeddingTextHash("tool search_docs: Searches docs.")
	h2 := ComputeEmbeddingTextHash("tool search_docs: Searches docs.")
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q vs %q", h1, h2)
	}
	h3 := ComputeEmbeddingTextHash("tool search_docs: Different text.")
	if h1 == h3 {
		t.Fatal("different text should produce different hash")
	}
}

func TestSplitSummaryKeywords(t *testing.T) {
	cases := []struct {
		input      string
		wantBody   string
		wantKW     string
	}{
		{"Simple summary.", "Simple summary.", ""},
		{"Does X. [keywords: a, b, c]", "Does X.", "a, b, c"},
		{"Does X. [Keywords: A, B]", "Does X.", "A, B"},
		{"No bracket end [keywords: oops", "No bracket end [keywords: oops", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		body, kw := splitSummaryKeywords(tc.input)
		if body != tc.wantBody || kw != tc.wantKW {
			t.Errorf("splitSummaryKeywords(%q) = (%q, %q), want (%q, %q)",
				tc.input, body, kw, tc.wantBody, tc.wantKW)
		}
	}
}

func TestFirstSentence(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Hello world. More text.", "Hello world."},
		{"Single sentence", "Single sentence"},
		{"Ends with period.", "Ends with period."},
		{"Line one.\nLine two.", "Line one."},
		{"", ""},
		{"A. B. C.", "A."},
	}
	for _, tc := range cases {
		got := firstSentence(tc.input)
		if got != tc.want {
			t.Errorf("firstSentence(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestRefreshSearchText_excludesJSON(t *testing.T) {
	rec := models.CapabilityRecord{
		SourceID:            "gh",
		SourceType:          "gateway",
		Kind:                models.CapabilityKindTool,
		OriginalName:        "create_issue",
		CanonicalName:       "gh__create_issue",
		OriginalDescription: "open issue",
		InputSchemaJSON:     `{"type":"object","properties":{"title":{"type":"string"}}}`,
		MetadataJSON:        `{"annotations":{"readOnlyHint":true}}`,
		Tags:                []string{"title"},
	}
	RefreshSearchText(&rec)
	// Should contain tags and names.
	if !strings.Contains(rec.SearchText, "title") {
		t.Errorf("SearchText missing tag 'title': %q", rec.SearchText)
	}
	if !strings.Contains(rec.SearchText, "gh") {
		t.Errorf("SearchText missing source_id 'gh': %q", rec.SearchText)
	}
	// Should NOT contain raw JSON.
	if strings.Contains(rec.SearchText, "properties") {
		t.Errorf("SearchText should not contain raw JSON schema: %q", rec.SearchText)
	}
	if strings.Contains(rec.SearchText, "annotations") {
		t.Errorf("SearchText should not contain raw metadata JSON: %q", rec.SearchText)
	}
}
