package models

// SourceType identifies how lazy-tool reaches an upstream MCP endpoint.
type SourceType string

const (
	SourceTypeGateway SourceType = "gateway"
	SourceTypeServer  SourceType = "server"
)

// Transport is the wire protocol to the upstream MCP implementation.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
)

// Source describes one upstream MCP gateway or server from config.
type Source struct {
	ID        string
	Type      SourceType
	Transport Transport
	URL       string
	Command   string
	Args      []string
	// Cwd is the working directory for stdio transports (empty = process default).
	Cwd string
	// Adapter selects connector quirks; only "default" is implemented today (see docs/adr).
	Adapter string
	// Disabled skips indexing and proxy resolution; capabilities are removed on reindex (P2.3).
	Disabled bool `json:"disabled,omitempty"`
	// Fallback controls behavior when search returns zero results for this source.
	// "passthrough" returns the full capability list (capped at limit) as a safety net.
	Fallback string `json:"fallback,omitempty"`
	// Env is extra environment variables passed to stdio command processes.
	Env map[string]string `json:"env,omitempty"`
}
