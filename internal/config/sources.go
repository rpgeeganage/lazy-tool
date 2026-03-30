package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"lazy-tool/pkg/models"
)

// SourceYAML is one entry under `sources:` in the config file.
type SourceYAML struct {
	ID        string            `yaml:"id"`
	Type      string            `yaml:"type"`
	Transport string            `yaml:"transport"`
	URL       string            `yaml:"url"`
	Command   string            `yaml:"command"`
	Args      []string          `yaml:"args"`
	Cwd       string            `yaml:"cwd"`
	Adapter   string            `yaml:"adapter"`
	Disabled  bool              `yaml:"disabled"`
	Fallback  string            `yaml:"fallback"`
	Env       map[string]string `yaml:"env"`
}

// NormalizeSources validates YAML entries and returns model sources.
func NormalizeSources(entries []SourceYAML) ([]models.Source, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(entries))
	out := make([]models.Source, 0, len(entries))
	for i, e := range entries {
		s, err := normalizeOneSource(i, e, seen)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func normalizeOneSource(index int, e SourceYAML, seen map[string]struct{}) (models.Source, error) {
	prefix := fmt.Sprintf("sources[%d]", index)
	id := strings.TrimSpace(e.ID)
	if id == "" {
		return models.Source{}, fmt.Errorf("%s: id is required", prefix)
	}
	if _, dup := seen[id]; dup {
		return models.Source{}, fmt.Errorf("%s: duplicate source id %q", prefix, id)
	}
	seen[id] = struct{}{}

	st := strings.TrimSpace(strings.ToLower(e.Type))
	switch st {
	case "gateway":
	case "server":
	default:
		return models.Source{}, fmt.Errorf("%s (%q): type must be gateway or server", prefix, id)
	}

	tr := strings.TrimSpace(strings.ToLower(e.Transport))
	switch tr {
	case "stdio", "http":
	default:
		return models.Source{}, fmt.Errorf("%s (%q): transport must be stdio or http", prefix, id)
	}

	cmd := strings.TrimSpace(e.Command)
	u := strings.TrimSpace(e.URL)

	switch tr {
	case "http":
		if u == "" {
			return models.Source{}, fmt.Errorf("%s (%q): url is required for transport http", prefix, id)
		}
		pu, err := url.Parse(u)
		if err != nil {
			return models.Source{}, fmt.Errorf("%s (%q): invalid url: %w", prefix, id, err)
		}
		if pu.Scheme != "http" && pu.Scheme != "https" {
			return models.Source{}, fmt.Errorf("%s (%q): url must use http or https scheme (got %q)", prefix, id, pu.Scheme)
		}
		if pu.Host == "" {
			return models.Source{}, fmt.Errorf("%s (%q): url must include a host (e.g. http://127.0.0.1:8811/mcp)", prefix, id)
		}
	case "stdio":
		if cmd == "" {
			return models.Source{}, fmt.Errorf("%s (%q): command is required for transport stdio", prefix, id)
		}
	}

	ad := strings.TrimSpace(e.Adapter)
	if ad == "" {
		ad = "default"
	}
	if ad != "default" {
		return models.Source{}, fmt.Errorf("%s (%q): unknown adapter %q (supported: default)", prefix, id, ad)
	}

	cwd := strings.TrimSpace(e.Cwd)
	if cwd != "" {
		if tr != "stdio" {
			return models.Source{}, fmt.Errorf("%s (%q): cwd is only valid for transport stdio", prefix, id)
		}
		fi, err := os.Stat(cwd)
		if err != nil {
			return models.Source{}, fmt.Errorf("%s (%q): cwd %q: %w", prefix, id, cwd, err)
		}
		if !fi.IsDir() {
			return models.Source{}, fmt.Errorf("%s (%q): cwd %q is not a directory", prefix, id, cwd)
		}
	}

	fb := strings.TrimSpace(strings.ToLower(e.Fallback))
	switch fb {
	case "", "passthrough":
	default:
		return models.Source{}, fmt.Errorf("%s (%q): fallback must be empty or \"passthrough\" (got %q)", prefix, id, fb)
	}

	var envMap map[string]string
	if len(e.Env) > 0 {
		envMap = make(map[string]string, len(e.Env))
		for k, v := range e.Env {
			envMap[k] = v
		}
	}

	return models.Source{
		ID:        id,
		Type:      models.SourceType(st),
		Transport: models.Transport(tr),
		URL:       u,
		Command:   cmd,
		Args:      append([]string(nil), e.Args...),
		Cwd:       cwd,
		Adapter:   ad,
		Disabled:  e.Disabled,
		Fallback:  fb,
		Env:       envMap,
	}, nil
}
