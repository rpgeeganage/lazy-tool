package models

// Lexical candidate-gathering paths (RankedResults.CandidatePath). Stable for logs and clients.
const (
	SearchCandidatePathSubstringSkippedFTSHit = "substring_scan_skipped_fts_hit"
	// SearchCandidatePathSubstringFullCatalogNoFTSMatch is used when FTS MATCH is empty (no ≥2-char tokens), so BM25 is skipped and SQL substring match on search_text runs.
	SearchCandidatePathSubstringFullCatalogNoFTSMatch = "substring_scan_full_catalog_no_fts_match"
	// SearchCandidatePathSubstringFullCatalogFTSZeroRows is used when MATCH is non-empty but BM25 returned zero rows; SQL substring match on search_text runs as fallback.
	SearchCandidatePathSubstringFullCatalogFTSZeroRows = "substring_scan_full_catalog_fts_zero_rows"
	SearchCandidatePathEmptyQueryFullCatalog           = "empty_query_full_catalog"
	// SearchCandidatePathEmptyQueryTruncated is used when empty-query scan is capped by search.empty_query_max_catalog_ids.
	SearchCandidatePathEmptyQueryTruncated = "empty_query_full_catalog_truncated"
	// SearchCandidatePathFullCatalogSubstringDisabled is set when config disables the substring fallback (degraded retrieval).
	SearchCandidatePathFullCatalogSubstringDisabled = "full_catalog_substring_disabled"
)

// InvocationStat holds per-tool invocation statistics for search scoring.
type InvocationStat struct {
	InvokeCount  int64
	SuccessCount int64
}

// SearchQuery is input to hybrid retrieval.
type SearchQuery struct {
	Text         string
	Limit        int
	SourceIDs    []string
	Embedding    []float32
	HasEmbedding bool
	// LexicalOnly skips query embedding and vector retrieval (config or per-request, P2.4).
	LexicalOnly bool
	// GroupBySource fills RankedResults.Grouped with the same hits partitioned by source_id.
	GroupBySource bool
	// FavoriteIDs boosts scores for these capability ids (explainable; P2.3).
	FavoriteIDs map[string]struct{}
	// ExplainScores fills SearchResult.ScoreBreakdown with pre-ranker weight sums (part-3 ranking transparency).
	ExplainScores bool
	// InvocationStats maps canonical_name → stats for invocation-based relevance feedback.
	InvocationStats map[string]InvocationStat
}

// SearchResult is one ranked hit returned to callers (MCP / CLI / UI).
type SearchResult struct {
	Kind          CapabilityKind
	ProxyToolName string
	SourceID      string
	Summary       string
	Score         float64
	WhyMatched    []string
	CapabilityID  string
	// ScoreBreakdown is pre-ranker contribution totals (same weights as scoring); Score field is ranker-normalized. Filled when SearchQuery.ExplainScores.
	ScoreBreakdown map[string]float64 `json:"score_breakdown,omitempty"`
}

// SourceResultGroup is used when GroupBySource is requested (P2.4).
type SourceResultGroup struct {
	SourceID string         `json:"source_id"`
	Results  []SearchResult `json:"results"`
}

// RankedResults wraps ordered search output.
type RankedResults struct {
	Results []SearchResult
	Grouped []SourceResultGroup `json:"grouped,omitempty"`
	// CandidatePath describes how non-empty-query lexical candidates were gathered, or empty-query scan.
	// Omitted when unset (e.g. zero results before ranking).
	CandidatePath string `json:"candidate_path,omitempty"`
}
