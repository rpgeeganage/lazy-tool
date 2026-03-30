package search

import (
	"testing"

	"lazy-tool/pkg/models"
)

func TestRankResults_NormalizeScores(t *testing.T) {
	in := []models.SearchResult{
		{ProxyToolName: "a", Score: 10},
		{ProxyToolName: "b", Score: 20},
	}
	out := rankResults(models.SearchQuery{Limit: 10}, in)
	if len(out.Results) != 2 {
		t.Fatal(len(out.Results))
	}
	if out.Results[0].Score < out.Results[1].Score {
		t.Fatal("order")
	}
	if out.Results[0].Score > 1.01 || out.Results[0].Score < 0.99 {
		t.Fatalf("top score want ~1 got %v", out.Results[0].Score)
	}
}

func TestRankResults_Empty(t *testing.T) {
	out := rankResults(models.SearchQuery{Limit: 5}, nil)
	if len(out.Results) != 0 {
		t.Fatal("expected empty")
	}
}

func TestRankResults_LimitApplied(t *testing.T) {
	in := []models.SearchResult{
		{ProxyToolName: "a", Score: 30},
		{ProxyToolName: "b", Score: 20},
		{ProxyToolName: "c", Score: 10},
	}
	out := rankResults(models.SearchQuery{Limit: 2}, in)
	if len(out.Results) != 2 {
		t.Fatalf("expected 2, got %d", len(out.Results))
	}
}
