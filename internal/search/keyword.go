package search

import (
	"sort"
	"strings"
	"unicode"
)

func tokenize(s string) []string {
	s = strings.ToLower(s)
	var cur strings.Builder
	var out []string
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		out = append(out, cur.String())
		cur.Reset()
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func keywordScore(searchText string, tokens []string) float64 {
	if len(tokens) == 0 {
		return 0
	}
	var score float64
	for _, t := range tokens {
		if len(t) < 2 {
			continue
		}
		c := strings.Count(searchText, t)
		if c > 0 {
			score += float64(c) * (1 + 0.1*float64(len(t)))
		}
	}
	return score
}

func fallbackConjunctionTokens(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	stop := map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "as": {}, "at": {}, "for": {}, "from": {},
		"in": {}, "into": {}, "is": {}, "new": {}, "of": {}, "or": {}, "the": {},
		"this": {}, "to": {}, "with": {},
	}
	seen := make(map[string]struct{}, len(tokens))
	filtered := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if len(t) < 3 {
			continue
		}
		if _, ok := stop[t]; ok {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		filtered = append(filtered, t)
	}
	if len(filtered) == 0 {
		return nil
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return len(filtered[i]) > len(filtered[j])
	})
	if len(filtered) > 4 {
		filtered = filtered[:4]
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i] < filtered[j]
	})
	return filtered
}
