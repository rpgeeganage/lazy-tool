package search

import (
	"math"
	"sort"

	"lazy-tool/pkg/models"
)

// rankResults applies primary sort, limit, normalize, and rerank to produce final ordered results.
func rankResults(q models.SearchQuery, in []models.SearchResult) models.RankedResults {
	if len(in) == 0 {
		return models.RankedResults{}
	}

	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Score == in[j].Score {
			return in[i].ProxyToolName < in[j].ProxyToolName
		}
		return in[i].Score > in[j].Score
	})

	lim := q.Limit
	if lim <= 0 {
		lim = 10
	}
	if len(in) > lim {
		in = in[:lim]
	}

	normalizeScores01(in)
	rerankStable(in)
	return models.RankedResults{Results: in}
}

func normalizeScores01(in []models.SearchResult) {
	if len(in) == 0 {
		return
	}
	minS := in[0].Score
	maxS := in[0].Score
	for i := 1; i < len(in); i++ {
		if in[i].Score < minS {
			minS = in[i].Score
		}
		if in[i].Score > maxS {
			maxS = in[i].Score
		}
	}
	den := maxS - minS
	if den < 1e-9 {
		for i := range in {
			in[i].Score = 1
		}
		return
	}
	for i := range in {
		t := (in[i].Score - minS) / den
		in[i].Score = math.Round(t*100) / 100
		if in[i].Score < 0.01 {
			in[i].Score = 0.01
		}
	}
}

// rerankStable applies a light second pass: tie-break by name, prefer more why_matched signals.
func rerankStable(in []models.SearchResult) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Score != in[j].Score {
			return in[i].Score > in[j].Score
		}
		li, lj := len(in[i].WhyMatched), len(in[j].WhyMatched)
		if li != lj {
			return li > lj
		}
		return in[i].ProxyToolName < in[j].ProxyToolName
	})
}
