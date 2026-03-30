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
