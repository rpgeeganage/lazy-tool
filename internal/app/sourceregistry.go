package app

import (
	"fmt"

	"lazy-tool/pkg/models"
)

// SourceRegistry provides lookup and filtering for configured upstream sources.
type SourceRegistry struct {
	byID  map[string]models.Source
	order []string
}

// NewSourceRegistry builds a registry from normalized sources (must have unique ids).
func NewSourceRegistry(sources []models.Source) (*SourceRegistry, error) {
	r := &SourceRegistry{
		byID:  make(map[string]models.Source, len(sources)),
		order: make([]string, 0, len(sources)),
	}
	for _, s := range sources {
		if s.ID == "" {
			return nil, fmt.Errorf("source registry: empty source id")
		}
		if _, exists := r.byID[s.ID]; exists {
			return nil, fmt.Errorf("source registry: duplicate id %q", s.ID)
		}
		r.byID[s.ID] = s
		r.order = append(r.order, s.ID)
	}
	return r, nil
}

// Get returns an enabled source by id (disabled sources are not routable).
func (r *SourceRegistry) Get(id string) (models.Source, bool) {
	s, ok := r.byID[id]
	if !ok || s.Disabled {
		return models.Source{}, false
	}
	return s, true
}

// GetConfigured returns any source by id, including disabled entries.
func (r *SourceRegistry) GetConfigured(id string) (models.Source, bool) {
	s, ok := r.byID[id]
	return s, ok
}

// SourceEnabled reports whether the id exists and is not disabled.
func (r *SourceRegistry) SourceEnabled(id string) bool {
	s, ok := r.byID[id]
	return ok && !s.Disabled
}

// All returns enabled sources in config order.
func (r *SourceRegistry) All() []models.Source {
	out := make([]models.Source, 0, len(r.order))
	for _, id := range r.order {
		s := r.byID[id]
		if s.Disabled {
			continue
		}
		out = append(out, s)
	}
	return out
}

// AllConfigured returns every source in config order (including disabled).
func (r *SourceRegistry) AllConfigured() []models.Source {
	out := make([]models.Source, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.byID[id])
	}
	return out
}

// Filter returns sources whose ids appear in filter. Empty filter means all sources.
func (r *SourceRegistry) Filter(sourceIDs []string) []models.Source {
	if len(sourceIDs) == 0 {
		return r.All()
	}
	out := make([]models.Source, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		if s, ok := r.Get(id); ok {
			out = append(out, s)
		}
	}
	return out
}

// SourcesWithFallback returns enabled sources that have the given fallback mode.
func (r *SourceRegistry) SourcesWithFallback(mode string) []models.Source {
	var out []models.Source
	for _, id := range r.order {
		s := r.byID[id]
		if !s.Disabled && s.Fallback == mode {
			out = append(out, s)
		}
	}
	return out
}

// IDs returns enabled source ids in config order.
func (r *SourceRegistry) IDs() []string {
	var out []string
	for _, id := range r.order {
		if !r.byID[id].Disabled {
			out = append(out, id)
		}
	}
	return out
}
