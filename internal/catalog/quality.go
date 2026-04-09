package catalog

import (
	"strings"
	"unicode"

	"lazy-tool/pkg/models"
)

type VaguenessScore struct {
	Score   float64
	Reasons []string
}

func ScoreVagueness(rec *models.CapabilityRecord) VaguenessScore {
	desc := strings.TrimSpace(rec.OriginalDescription)
	words := splitWords(desc)
	var score float64
	var reasons []string
	if len(desc) < 20 {
		score += 0.4
		reasons = append(reasons, "too_short")
	} else if len(desc) < 50 {
		score += 0.2
		reasons = append(reasons, "short")
	}
	if !containsActionVerb(words) {
		score += 0.2
		reasons = append(reasons, "no_action_verb")
	}
	if genericWordRatio(words) >= 0.3 {
		score += 0.2
		reasons = append(reasons, "generic_words")
	}
	if len(rec.Tags) > 0 && !mentionsAny(desc, rec.Tags) {
		score += 0.1
		reasons = append(reasons, "no_parameters_mentioned")
	}
	if !containsObject(words) {
		score += 0.1
		reasons = append(reasons, "missing_object")
	}
	if score > 1 {
		score = 1
	}
	return VaguenessScore{Score: score, Reasons: reasons}
}

func EnrichFromSchema(rec *models.CapabilityRecord) string {
	action := inferAction(rec.Tags)
	if action == "" {
		return ""
	}
	object := inferObject(rec)
	if object == "" {
		object = "data"
	}
	params := topTags(rec.Tags, 3)
	var b strings.Builder
	b.WriteString(titleWord(string(rec.Kind)))
	b.WriteString(" that ")
	b.WriteString(action)
	b.WriteByte(' ')
	b.WriteString(object)
	if len(params) > 0 {
		b.WriteString(" using ")
		b.WriteString(strings.Join(params, ", "))
	}
	return b.String()
}

func enrichRecord(rec *models.CapabilityRecord) {
	quality := ScoreVagueness(rec)
	if quality.Score >= 0.5 {
		enrichment := EnrichFromSchema(rec)
		if enrichment != "" && !strings.Contains(strings.ToLower(rec.GeneratedSummary), strings.ToLower(enrichment)) {
			if strings.TrimSpace(rec.GeneratedSummary) == "" {
				rec.GeneratedSummary = enrichment
			} else {
				rec.GeneratedSummary = strings.TrimSpace(rec.GeneratedSummary) + " " + enrichment
			}
		}
	}
	if enrichmentTag := inferAction(rec.Tags); enrichmentTag != "" && !containsExact(rec.Tags, enrichmentTag) {
		rec.Tags = append(rec.Tags, enrichmentTag)
	}
}

func splitWords(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	return fields
}

func containsActionVerb(words []string) bool {
	verbs := map[string]struct{}{"search": {}, "find": {}, "list": {}, "get": {}, "fetch": {}, "read": {}, "create": {}, "update": {}, "delete": {}, "write": {}, "generate": {}, "send": {}, "run": {}, "manage": {}}
	for _, w := range words {
		if _, ok := verbs[w]; ok {
			return true
		}
	}
	return false
}

func genericWordRatio(words []string) float64 {
	if len(words) == 0 {
		return 1
	}
	generic := map[string]struct{}{"tool": {}, "helper": {}, "utility": {}, "manage": {}, "handle": {}, "resource": {}, "resources": {}, "operations": {}}
	count := 0
	for _, w := range words {
		if _, ok := generic[w]; ok {
			count++
		}
	}
	return float64(count) / float64(len(words))
}

func mentionsAny(desc string, tags []string) bool {
	low := strings.ToLower(desc)
	for _, tag := range tags {
		if strings.Contains(low, strings.ToLower(strings.ReplaceAll(tag, "_", " "))) || strings.Contains(low, strings.ToLower(tag)) {
			return true
		}
	}
	return false
}

func containsObject(words []string) bool {
	for _, w := range words {
		if len(w) > 3 && !containsActionVerb([]string{w}) {
			return true
		}
	}
	return false
}

func inferAction(tags []string) string {
	for _, tag := range tags {
		low := strings.ToLower(tag)
		switch {
		case strings.Contains(low, "query") || strings.Contains(low, "search") || strings.Contains(low, "filter"):
			return "searches"
		case strings.Contains(low, "url") || strings.Contains(low, "uri"):
			return "fetches"
		case strings.Contains(low, "path") && containsTag(tags, "content"):
			return "writes"
		case strings.Contains(low, "id"):
			return "reads"
		}
	}
	return ""
}

func inferObject(rec *models.CapabilityRecord) string {
	for _, part := range []string{rec.OriginalName, rec.SourceID, rec.OriginalDescription} {
		words := splitWords(part)
		for _, w := range words {
			if len(w) > 3 && !containsActionVerb([]string{w}) {
				return w
			}
		}
	}
	return ""
}

func topTags(tags []string, n int) []string {
	if len(tags) <= n {
		return append([]string(nil), tags...)
	}
	return append([]string(nil), tags[:n]...)
}

func containsTag(tags []string, want string) bool {
	for _, tag := range tags {
		if strings.EqualFold(tag, want) {
			return true
		}
	}
	return false
}

func containsExact(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

func titleWord(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
