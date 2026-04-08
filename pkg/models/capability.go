package models

import (
	"strings"
	"time"
)

// CapabilityKind distinguishes indexed capability types (tools + Phase 16 prompts/resources/templates in index).
type CapabilityKind string

const (
	CapabilityKindTool     CapabilityKind = "tool"
	CapabilityKindPrompt   CapabilityKind = "prompt"
	CapabilityKindResource CapabilityKind = "resource"
)

// CapabilityRecord is the normalized, searchable unit stored in the catalog.
type CapabilityRecord struct {
	ID                  string
	Kind                CapabilityKind
	SourceID            string
	SourceType          string
	CanonicalName       string
	OriginalName        string
	OriginalDescription string
	GeneratedSummary    string
	UserSummary         string `json:"user_summary"`
	SearchText          string
	InputSchemaJSON     string
	MetadataJSON        string
	Tags                []string
	EmbeddingModel      string
	EmbeddingVector     []float32
	EmbeddingTextHash   string
	VersionHash         string
	LastSeenAt          time.Time
}

// EffectiveSummary returns the manual override when set, otherwise the generated summary.
func (r CapabilityRecord) EffectiveSummary() string {
	if strings.TrimSpace(r.UserSummary) != "" {
		return strings.TrimSpace(r.UserSummary)
	}
	return r.GeneratedSummary
}
