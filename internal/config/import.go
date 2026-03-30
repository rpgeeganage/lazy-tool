package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// IDEHost identifies which IDE/app config to import from.
type IDEHost string

const (
	HostClaude IDEHost = "claude"
	HostCursor IDEHost = "cursor"
	HostVSCode IDEHost = "vscode"
)

// AllHosts lists every supported IDE config location.
var AllHosts = []IDEHost{HostClaude, HostCursor, HostVSCode}

// ideServerEntry is the JSON structure for one MCP server in IDE configs.
type ideServerEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url"`
}

// ideConfig wraps the top-level JSON structure of IDE MCP config files.
type ideConfig struct {
	MCPServers map[string]ideServerEntry `json:"mcpServers"`
}

// configPaths returns all candidate file paths for a given host.
func configPaths(host IDEHost) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	switch host {
	case HostClaude:
		switch runtime.GOOS {
		case "darwin":
			return []string{filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")}
		case "windows":
			appdata := os.Getenv("APPDATA")
			if appdata == "" {
				return nil
			}
			return []string{filepath.Join(appdata, "Claude", "claude_desktop_config.json")}
		case "linux":
			return []string{filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")}
		}
	case HostCursor:
		return []string{filepath.Join(home, ".cursor", "mcp.json")}
	case HostVSCode:
		return []string{filepath.Join(home, ".vscode", "mcp.json")}
	}
	return nil
}

// DiscoverSources scans IDE config files and returns SourceYAML entries.
// If hosts is empty, all known hosts are scanned.
func DiscoverSources(hosts ...IDEHost) ([]SourceYAML, error) {
	if len(hosts) == 0 {
		hosts = AllHosts
	}
	var out []SourceYAML
	seen := make(map[string]struct{})
	for _, host := range hosts {
		entries, err := discoverFromHost(host)
		if err != nil {
			continue // skip unreadable configs
		}
		for _, e := range entries {
			if _, dup := seen[e.ID]; dup {
				continue
			}
			seen[e.ID] = struct{}{}
			out = append(out, e)
		}
	}
	return out, nil
}

func discoverFromHost(host IDEHost) ([]SourceYAML, error) {
	paths := configPaths(host)
	if len(paths) == 0 {
		return nil, nil
	}
	for _, path := range paths {
		entries, err := parseIDEConfig(path, host)
		if err != nil {
			continue
		}
		return entries, nil
	}
	return nil, fmt.Errorf("no readable config for host %q", host)
}

func parseIDEConfig(path string, host IDEHost) ([]SourceYAML, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg ideConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(cfg.MCPServers) == 0 {
		return nil, nil
	}
	var out []SourceYAML
	for name, entry := range cfg.MCPServers {
		s := entryToSourceYAML(name, entry, host)
		if s.Command == "" && s.URL == "" {
			continue // skip entries we can't use
		}
		out = append(out, s)
	}
	return out, nil
}

func entryToSourceYAML(name string, entry ideServerEntry, host IDEHost) SourceYAML {
	id := sanitizeID(name, host)
	s := SourceYAML{
		ID:   id,
		Type: "server",
	}
	if entry.URL != "" {
		s.Transport = "http"
		s.URL = entry.URL
	} else {
		s.Transport = "stdio"
		s.Command = entry.Command
		s.Args = entry.Args
	}
	if len(entry.Env) > 0 {
		s.Env = entry.Env
	}
	return s
}

// sanitizeID creates a unique, valid source ID from a server name and host.
func sanitizeID(name string, host IDEHost) string {
	// Clean the name: lowercase, replace non-alphanumeric with dashes
	cleaned := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32 // lowercase
		}
		return '-'
	}, name)
	// Remove leading/trailing dashes and collapse repeated dashes
	parts := strings.FieldsFunc(cleaned, func(r rune) bool { return r == '-' })
	cleaned = strings.Join(parts, "-")
	if cleaned == "" {
		cleaned = "imported"
	}
	return cleaned
}

// DeduplicateSources detects sources that point to the same upstream server
// (same URL for HTTP, same command+args for stdio) and keeps only the first
// occurrence, returning warnings for the duplicates that were dropped.
func DeduplicateSources(sources []SourceYAML) (deduped []SourceYAML, warnings []string) {
	type entry struct {
		index int
		src   SourceYAML
	}
	groups := make(map[string][]entry)
	var order []string // preserve insertion order of fingerprints

	for i, s := range sources {
		fp := sourceFingerprint(s)
		if _, seen := groups[fp]; !seen {
			order = append(order, fp)
		}
		groups[fp] = append(groups[fp], entry{index: i, src: s})
	}

	for _, fp := range order {
		g := groups[fp]
		deduped = append(deduped, g[0].src)
		for _, dup := range g[1:] {
			warnings = append(warnings, fmt.Sprintf(
				"duplicate: '%s' and '%s' both point to %s — keeping '%s'",
				dup.src.ID, g[0].src.ID, fp, g[0].src.ID,
			))
		}
	}
	return deduped, warnings
}

// sourceFingerprint returns a string that uniquely identifies the upstream endpoint.
func sourceFingerprint(s SourceYAML) string {
	if s.URL != "" {
		return s.URL
	}
	return s.Command + "|" + strings.Join(s.Args, "|")
}

// GenerateConfigYAML produces a complete YAML config string from discovered sources.
func GenerateConfigYAML(sources []SourceYAML, sqlitePath string) string {
	if sqlitePath == "" {
		home, _ := os.UserHomeDir()
		sqlitePath = filepath.Join(home, ".lazy-tool", "catalog.db")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Generated by: lazy-tool import\n")
	fmt.Fprintf(&b, "app:\n  name: lazy-tool\n\n")
	fmt.Fprintf(&b, "storage:\n  sqlite_path: %q\n\n", sqlitePath)
	fmt.Fprintf(&b, "sources:\n")
	for _, s := range sources {
		fmt.Fprintf(&b, "  - id: %q\n", s.ID)
		fmt.Fprintf(&b, "    type: %s\n", s.Type)
		fmt.Fprintf(&b, "    transport: %s\n", s.Transport)
		if s.Command != "" {
			fmt.Fprintf(&b, "    command: %q\n", s.Command)
		}
		if len(s.Args) > 0 {
			fmt.Fprintf(&b, "    args:\n")
			for _, a := range s.Args {
				fmt.Fprintf(&b, "      - %q\n", a)
			}
		}
		if s.URL != "" {
			fmt.Fprintf(&b, "    url: %q\n", s.URL)
		}
		if len(s.Env) > 0 {
			fmt.Fprintf(&b, "    env:\n")
			for k, v := range s.Env {
				fmt.Fprintf(&b, "      %s: %q\n", k, v)
			}
		}
	}
	return b.String()
}
