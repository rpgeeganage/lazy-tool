package connectors

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"lazy-tool/pkg/models"
)

// ToolMeta is raw tool metadata from an upstream MCP list_tools response.
type ToolMeta struct {
	Name            string
	Description     string
	InputSchema     []byte
	AnnotationsJSON []byte // JSON-encoded mcp.ToolAnnotations (nil if absent)
}

// PromptMeta is raw prompt metadata from prompts/list.
type PromptMeta struct {
	Name          string
	Description   string
	ArgumentsJSON []byte
}

// ResourceMeta is raw resource metadata from resources/list.
type ResourceMeta struct {
	URI         string
	Name        string
	Description string
	MIMEType    string
}

// ResourceTemplateMeta is raw metadata from resources/templates/list.
type ResourceTemplateMeta struct {
	URITemplate string
	Name        string
	Description string
}

// IndexSnapshot holds list_tools / list_prompts / resources / templates from **one** MCP session.
// Non-nil *Err fields mean that list was skipped (reindex logs a warning); tools failure aborts the snapshot.
type IndexSnapshot struct {
	Tools                []ToolMeta
	Prompts              []PromptMeta
	PromptsErr           error
	Resources            []ResourceMeta
	ResourcesErr         error
	ResourceTemplates    []ResourceTemplateMeta
	ResourceTemplatesErr error
}

// Connector fetches capabilities from one upstream source and can proxy tool calls.
type Connector interface {
	IndexConnector
	ProxyConnector
	SourceID() string
	ListTools(ctx context.Context) ([]ToolMeta, error)
	ListPrompts(ctx context.Context) ([]PromptMeta, error)
	ListResources(ctx context.Context) ([]ResourceMeta, error)
	ListResourceTemplates(ctx context.Context) ([]ResourceTemplateMeta, error)
	// Close releases connector-local resources after indexing or proxy use. Base implementations return nil;
	// callers may defer when a release hook is needed later. HTTP session reuse stays with Factory.Close.
	Close() error
}

// IndexConnector is the subset of Connector needed by the catalog indexer.
type IndexConnector interface {
	ListForIndex(ctx context.Context) (*IndexSnapshot, error)
	Health(ctx context.Context) error
	Close() error
}

// ProxyConnector is the subset of Connector needed by the proxy executor.
type ProxyConnector interface {
	CallTool(ctx context.Context, toolName string, arguments map[string]any) (*mcp.CallToolResult, error)
	GetPrompt(ctx context.Context, name string, arguments map[string]string) (*mcp.GetPromptResult, error)
	ReadResource(ctx context.Context, uri string) (*mcp.ReadResourceResult, error)
	Close() error
}

// Factory builds connectors from source definitions.
type Factory interface {
	New(ctx context.Context, src models.Source) (Connector, error)
	// CircuitBreakerFor returns the circuit breaker for a source. Returns nil if circuit breaking is disabled.
	CircuitBreakerFor(sourceID string) *CircuitBreaker
	SeedCircuitBreaker(sourceID string, state CircuitState, failures int, lastFailedAt time.Time)
	// Close releases reused HTTP MCP sessions (no-op if none). Call from process shutdown (e.g. Stack.Close).
	Close() error
}
