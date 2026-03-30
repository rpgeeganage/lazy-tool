package mcpserver

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"lazy-tool/internal/runtime"
)

// ServerMode controls which tools are registered on the MCP server.
const (
	ModeSearch = "search" // default: 5 meta-tools (search, invoke, prompt, resource, inspect)
	ModeDirect = "direct" // proxy all cataloged tools as first-class MCP tools
	ModeHybrid = "hybrid" // both search meta-tools and direct proxy tools
)

// NormalizeMode returns a valid mode string, defaulting to "search".
func NormalizeMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case ModeDirect:
		return ModeDirect
	case ModeHybrid:
		return ModeHybrid
	default:
		return ModeSearch
	}
}

func NewServer(stack *runtime.Stack, log *slog.Logger) *ServerBundle {
	return NewServerWithMode(stack, log, ModeSearch)
}

// ServerBundle wraps the MCP server with an optional DirectProxy for refresh support.
type ServerBundle struct {
	Server      *mcp.Server
	DirectProxy *DirectProxy // nil in search-only mode
}

func NewServerWithMode(stack *runtime.Stack, log *slog.Logger, mode string) *ServerBundle {
	mode = NormalizeMode(mode)
	srv := mcp.NewServer(&mcp.Implementation{Name: "lazy-tool", Version: "0.1.0"}, nil)

	if mode == ModeSearch || mode == ModeHybrid {
		registerSearchTools(srv, stack)
		registerInspectCapability(srv, stack)
		registerInvokeTool(srv, stack, log)
		registerGetProxyPrompt(srv, stack, log)
		registerReadProxyResource(srv, stack, log)
	}

	var dp *DirectProxy
	if mode == ModeDirect || mode == ModeHybrid {
		dp = NewDirectProxy(srv, stack, log)
		dp.RegisterAll()
	}

	return &ServerBundle{Server: srv, DirectProxy: dp}
}

func RunStdio(ctx context.Context, stack *runtime.Stack, log *slog.Logger) error {
	return RunStdioWithMode(ctx, stack, log, ModeSearch)
}

func RunStdioWithMode(ctx context.Context, stack *runtime.Stack, log *slog.Logger, mode string) error {
	bundle := NewServerWithMode(stack, log, mode)
	return bundle.Server.Run(ctx, &mcp.StdioTransport{})
}

// RunStdioWithBundle starts stdio transport using a pre-built ServerBundle (allows caller to access DirectProxy).
func RunStdioWithBundle(ctx context.Context, bundle *ServerBundle) error {
	return bundle.Server.Run(ctx, &mcp.StdioTransport{})
}

// NewHTTPHandler creates an http.Handler that serves MCP over Streamable HTTP.
func NewHTTPHandler(stack *runtime.Stack, log *slog.Logger, mode string) (http.Handler, *DirectProxy) {
	bundle := NewServerWithMode(stack, log, mode)
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return bundle.Server
	}, nil)
	return handler, bundle.DirectProxy
}
