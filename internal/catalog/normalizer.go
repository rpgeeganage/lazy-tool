package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode"

	"lazy-tool/internal/connectors"
	"lazy-tool/pkg/models"
)

// SanitizeSegment normalises a name fragment for use in canonical names.
// Letters, digits, hyphens, and underscores are preserved (lowercased);
// everything else becomes a hyphen. Runs of consecutive separators are
// collapsed to a single character so that the double-underscore "__"
// delimiter used between segments can never appear inside a segment.
func SanitizeSegment(s string) string {
	var b strings.Builder
	prevSep := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevSep = false
		case r == '_':
			if !prevSep {
				b.WriteByte('_')
				prevSep = true
			}
		case r == '-':
			if !prevSep {
				b.WriteByte('-')
				prevSep = true
			}
		default:
			if !prevSep {
				b.WriteByte('-')
				prevSep = true
			}
		}
	}
	out := b.String()
	// Trim leading/trailing separators.
	out = strings.TrimFunc(out, func(r rune) bool { return r == '_' || r == '-' })
	return out
}

func NormalizeTool(src models.Source, meta connectors.ToolMeta, now time.Time) models.CapabilityRecord {
	canonical := SanitizeSegment(src.ID) + "__" + SanitizeSegment(meta.Name)
	if SanitizeSegment(meta.Name) == "" {
		canonical = SanitizeSegment(src.ID) + "__tool"
	}
	metadataJSON := "{}"
	if len(meta.AnnotationsJSON) > 0 {
		metaObj := map[string]any{}
		var ann any
		if json.Unmarshal(meta.AnnotationsJSON, &ann) == nil {
			metaObj["annotations"] = ann
		}
		if mb, err := json.Marshal(metaObj); err == nil {
			metadataJSON = string(mb)
		}
	}
	rec := models.CapabilityRecord{
		ID:                  CapabilityID(src.ID, meta.Name),
		Kind:                models.CapabilityKindTool,
		SourceID:            src.ID,
		SourceType:          string(src.Type),
		CanonicalName:       canonical,
		OriginalName:        meta.Name,
		OriginalDescription: meta.Description,
		InputSchemaJSON:     string(meta.InputSchema),
		VersionHash:         VersionHash(meta),
		LastSeenAt:          now,
		MetadataJSON:        metadataJSON,
	}
	if rec.InputSchemaJSON == "" {
		rec.InputSchemaJSON = "{}"
	}
	rec.Tags = SchemaArgNames(rec.InputSchemaJSON)
	rec.Tags = append(rec.Tags, SchemaSignals(rec.InputSchemaJSON)...)
	RefreshSearchText(&rec)
	return rec
}

// RefreshSearchText rebuilds the FTS-indexed search_text from structured fields.
// Excludes raw JSON schema and metadata to reduce lexical noise; parameter names
// are already captured in Tags.
func RefreshSearchText(rec *models.CapabilityRecord) {
	parts := []string{
		rec.SourceID,
		strings.ToLower(string(rec.SourceType)),
		string(rec.Kind),
		rec.OriginalName,
		rec.CanonicalName,
		rec.OriginalDescription,
		rec.GeneratedSummary,
		rec.UserSummary,
		strings.Join(rec.Tags, " "),
	}
	rec.SearchText = strings.ToLower(strings.Join(parts, " "))
}

// BuildEmbeddingText produces a clean, semantic-friendly string for vector embeddings.
// The format is designed to maximise cosine-similarity relevance while providing
// complementary coverage to the FTS5 index (which uses SearchText built from
// user_summary + metadata).
//
// Strategy: embed the full OriginalDescription (rich upstream content) so the vector
// index captures semantic nuances that the curated user_summary may omit. The
// user_summary keywords are still appended to anchor the embedding to discovery terms.
//
//	"{Kind} {OriginalName}: {description}. Parameters: {tags}. Keywords: {kw}"
//
// When no OriginalDescription is available, falls back to EffectiveSummary.
// If the effective summary contains a "[keywords: ...]" suffix, those keywords are
// extracted and appended separately.
func BuildEmbeddingText(rec *models.CapabilityRecord) string {
	return BuildEmbeddingTextWithStrategy(rec, "original_first")
}

func BuildEmbeddingTextWithStrategy(rec *models.CapabilityRecord, strategy string) string {
	_, keywords := splitSummaryKeywords(rec.EffectiveSummary())
	desc := embeddingPrimaryText(rec, strategy, &keywords)

	var b strings.Builder
	b.WriteString(string(rec.Kind))
	b.WriteByte(' ')
	b.WriteString(rec.OriginalName)
	b.WriteString(": ")
	b.WriteString(desc)

	if len(rec.Tags) > 0 {
		b.WriteString(". Parameters: ")
		b.WriteString(strings.Join(rec.Tags, ", "))
	}

	if keywords != "" {
		b.WriteString(". Keywords: ")
		b.WriteString(keywords)
	}

	return b.String()
}

func embeddingPrimaryText(rec *models.CapabilityRecord, strategy string, keywords *string) string {
	original := strings.TrimSpace(rec.OriginalDescription)
	summary, kw := splitSummaryKeywords(rec.EffectiveSummary())
	if *keywords == "" {
		*keywords = kw
	}
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "summary_first":
		if summary != "" {
			return summary
		}
		return original
	case "combined":
		parts := []string{}
		if original != "" {
			parts = append(parts, original)
		}
		if summary != "" && !strings.EqualFold(summary, original) {
			parts = append(parts, summary)
		}
		return strings.Join(parts, ". ")
	case "auto":
		if ScoreVagueness(rec).Score >= 0.5 && summary != "" {
			return summary
		}
		fallthrough
	case "original_first":
		if original != "" {
			return original
		}
		return summary
	default:
		if original != "" {
			return original
		}
		return summary
	}
}

// ComputeEmbeddingTextHash returns a hex-encoded SHA-256 of the embedding text.
// This is stored alongside the embedding vector so we can detect when the text
// changes (e.g. user_summary edit) even when VersionHash hasn't changed.
func ComputeEmbeddingTextHash(embeddingText string) string {
	h := sha256.Sum256([]byte(embeddingText))
	return hex.EncodeToString(h[:])
}

// splitSummaryKeywords splits a summary like "Does X. [keywords: azure, firewall]"
// into ("Does X.", "azure, firewall"). Returns ("original", "") if no keyword block.
func splitSummaryKeywords(summary string) (string, string) {
	idx := strings.LastIndex(summary, "[keywords:")
	if idx < 0 {
		idx = strings.LastIndex(summary, "[Keywords:")
	}
	if idx < 0 {
		return summary, ""
	}
	end := strings.LastIndex(summary, "]")
	if end <= idx {
		return summary, ""
	}
	kwBlock := summary[idx+len("[keywords:") : end]
	kwBlock = strings.TrimSpace(kwBlock)
	body := strings.TrimSpace(summary[:idx])
	return body, kwBlock
}

// firstSentence extracts the first sentence (up to the first ". " or ".\n" or end of string).
// Avoids cutting mid-word for very long descriptions.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '.' && (s[i+1] == ' ' || s[i+1] == '\n') {
			return s[:i+1]
		}
	}
	return s
}

func promptArgsToInputSchemaJSON(argsJSON []byte) string {
	if len(argsJSON) == 0 || string(argsJSON) == "null" {
		return "{}"
	}
	var raw []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(argsJSON, &raw); err != nil {
		return "{}"
	}
	props := make(map[string]any)
	for _, a := range raw {
		if a.Name == "" {
			continue
		}
		props[a.Name] = map[string]any{"type": "string"}
	}
	sch := map[string]any{"type": "object", "properties": props}
	b, _ := json.Marshal(sch)
	return string(b)
}

// NormalizePrompt builds a searchable record for MCP prompts/list (Phase 16).
func NormalizePrompt(src models.Source, meta connectors.PromptMeta, now time.Time) models.CapabilityRecord {
	seg := SanitizeSegment(meta.Name)
	if seg == "" {
		seg = "prompt"
	}
	canonical := SanitizeSegment(src.ID) + "__p_" + seg
	inSchema := promptArgsToInputSchemaJSON(meta.ArgumentsJSON)
	rec := models.CapabilityRecord{
		ID:                  CapabilityID(src.ID, "prompt:"+meta.Name),
		Kind:                models.CapabilityKindPrompt,
		SourceID:            src.ID,
		SourceType:          string(src.Type),
		CanonicalName:       canonical,
		OriginalName:        meta.Name,
		OriginalDescription: meta.Description,
		InputSchemaJSON:     inSchema,
		VersionHash:         VersionHashPrompt(meta),
		LastSeenAt:          now,
		MetadataJSON:        "{}",
	}
	rec.Tags = SchemaArgNames(rec.InputSchemaJSON)
	rec.Tags = append(rec.Tags, SchemaSignals(rec.InputSchemaJSON)...)
	RefreshSearchText(&rec)
	return rec
}

// NormalizeResource builds a searchable record for MCP resources/list (Phase 16).
func NormalizeResource(src models.Source, meta connectors.ResourceMeta, now time.Time) models.CapabilityRecord {
	uriSeg := SanitizeSegment(meta.URI)
	if uriSeg == "" {
		uriSeg = "resource"
	}
	name := meta.Name
	if name == "" {
		name = meta.URI
	}
	canonical := SanitizeSegment(src.ID) + "__r_" + uriSeg
	rec := models.CapabilityRecord{
		ID:                  CapabilityID(src.ID, "resource:"+meta.URI),
		Kind:                models.CapabilityKindResource,
		SourceID:            src.ID,
		SourceType:          string(src.Type),
		CanonicalName:       canonical,
		OriginalName:        name,
		OriginalDescription: meta.Description,
		InputSchemaJSON:     "{}",
		VersionHash:         VersionHashResource(meta),
		LastSeenAt:          now,
		MetadataJSON:        resourceMetaJSON(meta.URI, meta.MIMEType),
	}
	if meta.MIMEType != "" {
		rec.Tags = []string{meta.MIMEType}
	}
	RefreshSearchText(&rec)
	return rec
}

func resourceMetaJSON(uri, mime string) string {
	m := map[string]string{}
	if uri != "" {
		m["uri"] = uri
	}
	if mime != "" {
		m["mimeType"] = mime
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// NormalizeResourceTemplate builds a record for resources/templates/list (Phase 16).
func NormalizeResourceTemplate(src models.Source, meta connectors.ResourceTemplateMeta, now time.Time) models.CapabilityRecord {
	tplSeg := SanitizeSegment(meta.URITemplate)
	if tplSeg == "" {
		tplSeg = "template"
	}
	name := meta.Name
	if name == "" {
		name = meta.URITemplate
	}
	canonical := SanitizeSegment(src.ID) + "__rt_" + tplSeg
	metaObj := map[string]any{"resource_template": true, "uriTemplate": meta.URITemplate}
	mb, _ := json.Marshal(metaObj)
	rec := models.CapabilityRecord{
		ID:                  CapabilityID(src.ID, "resourceTemplate:"+meta.URITemplate),
		Kind:                models.CapabilityKindResource,
		SourceID:            src.ID,
		SourceType:          string(src.Type),
		CanonicalName:       canonical,
		OriginalName:        name,
		OriginalDescription: meta.Description,
		InputSchemaJSON:     "{}",
		VersionHash:         VersionHashResourceTemplate(meta),
		LastSeenAt:          now,
		MetadataJSON:        string(mb),
	}
	RefreshSearchText(&rec)
	return rec
}
