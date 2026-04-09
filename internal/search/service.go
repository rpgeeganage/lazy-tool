package search

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"lazy-tool/internal/embeddings"
	"lazy-tool/internal/metrics"
	"lazy-tool/internal/storage"
	"lazy-tool/internal/vector"
	"lazy-tool/pkg/models"
)

// defaultEmptyQueryIDBatch limits rows loaded at once for empty-needle search (ordering matches ListAll).
const defaultEmptyQueryIDBatch = 128

type Service struct {
	Store       *storage.SQLiteStore
	Vec         *vector.Index
	Embed       embeddings.Embedder
	Weights     ScoreWeights
	LexicalOnly bool
	// FullCatalogSubstring enables SQL substring matching on search_text when FTS did not already return rows (default true).
	FullCatalogSubstring bool
	// EmptyQueryIDBatch overrides defaultEmptyQueryIDBatch when > 0.
	EmptyQueryIDBatch int
	// EmptyQueryMaxCatalogIDs caps IDs processed for '' query when > 0 (stable order).
	EmptyQueryMaxCatalogIDs int
}

func NewService(store *storage.SQLiteStore, vec *vector.Index, emb embeddings.Embedder, weights ScoreWeights, lexicalOnly bool) *Service {
	return &Service{
		Store:                store,
		Vec:                  vec,
		Embed:                emb,
		Weights:              MergeScoreWeights(weights),
		LexicalOnly:          lexicalOnly,
		FullCatalogSubstring: true,
	}
}

func sourceAllowed(sourceID string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, id := range filter {
		if id == sourceID {
			return true
		}
	}
	return false
}

func (s *Service) Search(ctx context.Context, q models.SearchQuery) (models.RankedResults, error) {
	started := time.Now()
	finalize := func(out models.RankedResults, err error) (models.RankedResults, error) {
		durationMS := time.Since(started).Milliseconds()
		metrics.SearchDuration(durationMS, out.CandidatePath, len(out.Results), err)
		if s.Store != nil {
			_ = s.Store.RecordOperation(ctx, storage.OperationLogEvent{
				Operation:  "search",
				DurationMS: durationMS,
				Metadata: map[string]any{
					"query":          q.Text,
					"candidate_path": out.CandidatePath,
					"result_count":   len(out.Results),
				},
				Error: errorString(err),
			})
		}
		return out, err
	}
	tokens := tokenize(q.Text)
	needle := strings.ToLower(strings.TrimSpace(q.Text))

	lexical := s.LexicalOnly || q.LexicalOnly
	qEmb := q.Embedding
	if !lexical && !q.HasEmbedding && s.Embed != nil && needle != "" {
		vecs, err := s.Embed.Embed(ctx, []string{q.Text})
		if err == nil && len(vecs) > 0 && len(vecs[0]) > 0 {
			qEmb = vecs[0]
		}
	}

	var vecHits map[string]float32
	if !lexical && len(qEmb) > 0 && s.Vec != nil {
		n := q.Limit * 15
		if n < 40 {
			n = 40
		}
		filterID := ""
		if len(q.SourceIDs) == 1 {
			filterID = q.SourceIDs[0]
		}
		res, err := VectorQuery(ctx, s.Store, s.Vec, qEmb, n, filterID)
		if err != nil {
			if errors.Is(err, vector.ErrClosed) {
				return finalize(models.RankedResults{}, err)
			}
			// Other chromem/query failures: omit vector leg (legacy lenient behavior).
		} else {
			vecHits = vector.ScoreMap(res)
		}
	}

	wt := s.Weights

	// Empty query: same behavior as legacy — scan full catalog; only vector (and token-less lexical) score.
	if needle == "" {
		out, err := s.searchEmptyQuery(ctx, q, tokens, vecHits, wt)
		return finalize(out, err)
	}

	candidateIDs, candidatePath, err := s.buildCandidates(ctx, q, needle, vecHits)
	if err != nil {
		return finalize(models.RankedResults{}, err)
	}
	if len(candidateIDs) == 0 {
		metrics.SearchExecuted(0)
		return finalize(models.RankedResults{CandidatePath: candidatePath}, nil)
	}

	byID, err := s.Store.GetCapabilitiesByIDs(ctx, candidateIDs)
	if err != nil {
		return finalize(models.RankedResults{}, err)
	}

	// Load invocation stats for relevance feedback if boost is configured
	if wt.InvocationBoost > 0 {
		var names []string
		for _, id := range candidateIDs {
			if rec, ok := byID[id]; ok {
				names = append(names, rec.CanonicalName)
			}
		}
		if len(names) > 0 {
			stats, err := s.Store.GetInvocationStats(ctx, names)
			if err == nil && len(stats) > 0 {
				q.InvocationStats = make(map[string]models.InvocationStat, len(stats))
				for name, st := range stats {
					q.InvocationStats[name] = models.InvocationStat{
						InvokeCount:  st.InvokeCount,
						SuccessCount: st.SuccessCount,
					}
				}
			}
		}
	}

	var results []models.SearchResult
	for _, id := range candidateIDs {
		rec, ok := byID[id]
		if !ok {
			continue
		}
		if !sourceAllowed(rec.SourceID, q.SourceIDs) {
			continue
		}
		if sr, ok := scoreCandidate(&rec, needle, tokens, vecHits, wt, q); ok {
			results = append(results, sr)
		}
	}
	out, err := s.rankAndFinalize(ctx, q, results, candidatePath)
	return finalize(out, err)
}

func userSummaryWeight(wt ScoreWeights, r *models.CapabilityRecord) (float64, []string) {
	if strings.TrimSpace(r.UserSummary) == "" || wt.UserSummary <= 0 {
		return 0, nil
	}
	return wt.UserSummary, []string{"user:edited-summary"}
}

func favoriteWeight(wt ScoreWeights, q models.SearchQuery, r *models.CapabilityRecord) (float64, []string) {
	if q.FavoriteIDs == nil || wt.Favorite <= 0 {
		return 0, nil
	}
	if _, ok := q.FavoriteIDs[r.ID]; !ok {
		return 0, nil
	}
	return wt.Favorite, []string{"user:favorite"}
}

// scoreCandidate computes score, why-matched signals, and optional breakdown for a single capability record.
// Returns ok=false when the candidate should be dropped (score <= 0).
func scoreCandidate(rec *models.CapabilityRecord, needle string, tokens []string, vecHits map[string]float32, wt ScoreWeights, q models.SearchQuery) (models.SearchResult, bool) {
	var sc float64
	var why []string
	var bd map[string]float64
	if q.ExplainScores {
		bd = make(map[string]float64)
	}
	addBD := func(key string, delta float64) {
		if bd == nil || delta == 0 {
			return
		}
		bd[key] += delta
	}
	if needle != "" {
		if rec.CanonicalName == needle {
			sc += wt.ExactCanonical
			addBD("exact_canonical", wt.ExactCanonical)
			why = append(why, "exact:canonical")
		} else if strings.EqualFold(rec.OriginalName, needle) {
			sc += wt.ExactName
			addBD("exact_name", wt.ExactName)
			why = append(why, "exact:name")
		} else if strings.Contains(rec.SearchText, needle) {
			sc += wt.Substring
			addBD("substring", wt.Substring)
			why = append(why, "text:substring")
		}
	}
	ls, wLex := scoreLexical(needle, tokens, rec)
	sc += ls
	addBD("lexical", ls)
	why = mergeWhyUnique(why, wLex)
	if vecHits != nil {
		if v, ok := vecHits[rec.ID]; ok {
			nv := normalizeCosine(v)
			vecPts := nv * wt.VectorMultiplier
			sc += vecPts
			addBD("vector", vecPts)
			why = append(why, "vector:similarity")
		}
	}
	if u, wu := userSummaryWeight(wt, rec); u > 0 {
		sc += u
		addBD("user_summary", u)
		why = mergeWhyUnique(why, wu)
	}
	if f, wf := favoriteWeight(wt, q, rec); f > 0 {
		sc += f
		addBD("favorite", f)
		why = mergeWhyUnique(why, wf)
	}
	if wt.InvocationBoost > 0 && q.InvocationStats != nil {
		if st, ok := q.InvocationStats[rec.CanonicalName]; ok && st.InvokeCount > 0 {
			boost := math.Log2(1+float64(st.InvokeCount)) * wt.InvocationBoost
			sc += boost
			addBD("invocation", boost)
			why = append(why, "history:invoked")
		}
	}
	if sc <= 0 {
		return models.SearchResult{}, false
	}
	sr := models.SearchResult{
		Kind:          rec.Kind,
		ProxyToolName: rec.CanonicalName,
		SourceID:      rec.SourceID,
		Summary:       rec.EffectiveSummary(),
		Score:         sc,
		WhyMatched:    why,
		CapabilityID:  rec.ID,
	}
	if len(bd) > 0 {
		sr.ScoreBreakdown = bd
	}
	return sr, true
}

// rankAndFinalize applies ranking, score normalization, explain-scores scaling, group-by-source, and metrics.
func (s *Service) rankAndFinalize(_ context.Context, q models.SearchQuery, results []models.SearchResult, candidatePath string) (models.RankedResults, error) {
	rawByCap := map[string]float64{}
	if q.ExplainScores {
		for _, r := range results {
			rawByCap[r.CapabilityID] = r.Score
		}
	}
	ranked := rankResults(q, results)
	if q.ExplainScores && len(ranked.Results) > 0 {
		scaleScoreBreakdownsAfterRank(ranked.Results, rawByCap)
	}
	ranked.CandidatePath = candidatePath
	if q.GroupBySource && len(ranked.Results) > 0 {
		ranked.Grouped = GroupResultsBySource(ranked.Results)
	}
	metrics.SearchExecuted(len(ranked.Results))
	return ranked, nil
}

func (s *Service) searchEmptyQuery(ctx context.Context, q models.SearchQuery, tokens []string, vecHits map[string]float32, wt ScoreWeights) (models.RankedResults, error) {
	ids, err := s.Store.ListAllIDs(ctx)
	if err != nil {
		return models.RankedResults{}, err
	}
	totalCatalog := len(ids)
	truncated := false
	if s.EmptyQueryMaxCatalogIDs > 0 && len(ids) > s.EmptyQueryMaxCatalogIDs {
		ids = ids[:s.EmptyQueryMaxCatalogIDs]
		truncated = true
	}
	metrics.SearchEmptyQueryScan(totalCatalog, len(ids), truncated)
	batch := defaultEmptyQueryIDBatch
	if s.EmptyQueryIDBatch > 0 {
		batch = s.EmptyQueryIDBatch
	}
	var results []models.SearchResult
	for i := 0; i < len(ids); i += batch {
		j := min(i+batch, len(ids))
		chunk := ids[i:j]
		byID, err := s.Store.GetCapabilitiesByIDs(ctx, chunk)
		if err != nil {
			return models.RankedResults{}, err
		}
		for _, id := range chunk {
			rec, ok := byID[id]
			if !ok {
				continue
			}
			if !sourceAllowed(rec.SourceID, q.SourceIDs) {
				continue
			}
			if sr, ok := scoreCandidate(&rec, "", tokens, vecHits, wt, q); ok {
				results = append(results, sr)
			}
		}
	}
	candidatePath := models.SearchCandidatePathEmptyQueryFullCatalog
	if truncated {
		candidatePath = models.SearchCandidatePathEmptyQueryTruncated
	}
	return s.rankAndFinalize(ctx, q, results, candidatePath)
}

func (s *Service) buildCandidates(ctx context.Context, q models.SearchQuery, needle string, vecHits map[string]float32) ([]string, string, error) {
	seen := make(map[string]struct{})
	var out []string
	add := func(id string) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}

	if rec, err := s.Store.GetByCanonicalName(ctx, needle); err == nil {
		if sourceAllowed(rec.SourceID, q.SourceIDs) {
			add(rec.ID)
		}
	}

	origIDs, err := s.Store.ListIDsByOriginalNameFold(ctx, needle, q.SourceIDs)
	if err != nil {
		return nil, "", err
	}
	for _, id := range origIDs {
		add(id)
	}

	ftsLimit := q.Limit * 15
	if ftsLimit < 40 {
		ftsLimit = 40
	}
	match := storage.BuildFTSMatchQuery(q.Text)
	ftsIDs, err := s.Store.SearchFTSCandidateIDs(ctx, match, q.SourceIDs, ftsLimit)
	if err != nil {
		return nil, "", err
	}
	for _, id := range ftsIDs {
		add(id)
	}

	if len(vecHits) > 0 {
		type pair struct {
			id  string
			sim float32
		}
		var pairs []pair
		for id, v := range vecHits {
			pairs = append(pairs, pair{id: id, sim: v})
		}
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].sim > pairs[j].sim
		})
		ids := make([]string, 0, len(pairs))
		for _, p := range pairs {
			ids = append(ids, p.id)
		}
		byID, err := s.Store.GetCapabilitiesByIDs(ctx, ids)
		if err != nil {
			return nil, "", err
		}
		for _, p := range pairs {
			rec, ok := byID[p.id]
			if !ok {
				continue
			}
			if sourceAllowed(rec.SourceID, q.SourceIDs) {
				add(p.id)
			}
		}
	}

	// Substring scan over the full catalog: skip only when FTS returned enough candidates
	// to fill the request. When FTS returns sparse results (fewer than limit), augment with
	// substring scan so that near-matches are not lost to BM25 ranking gaps.
	if match != "" && len(ftsIDs) >= q.Limit {
		metrics.SearchCandidateGeneration(models.SearchCandidatePathSubstringSkippedFTSHit)
		return out, models.SearchCandidatePathSubstringSkippedFTSHit, nil
	}

	if !s.FullCatalogSubstring {
		metrics.SearchCandidateGeneration(models.SearchCandidatePathFullCatalogSubstringDisabled)
		return out, models.SearchCandidatePathFullCatalogSubstringDisabled, nil
	}

	var subPath string
	switch {
	case match == "":
		subPath = models.SearchCandidatePathSubstringFullCatalogNoFTSMatch
	case len(ftsIDs) == 0:
		subPath = models.SearchCandidatePathSubstringFullCatalogFTSZeroRows
	default:
		subPath = models.SearchCandidatePathSubstringAugmentedFTSSparse
	}
	metrics.SearchCandidateGeneration(subPath)
	subIDs, err := s.Store.ListIDsBySearchTextSubstring(ctx, needle, q.SourceIDs)
	if err != nil {
		return nil, "", err
	}
	for _, id := range subIDs {
		add(id)
	}

	return out, subPath, nil
}

func mergeWhyUnique(prefix []string, extra []string) []string {
	seen := make(map[string]struct{}, len(prefix)+len(extra))
	var out []string
	for _, w := range prefix {
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	for _, w := range extra {
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	return out
}
