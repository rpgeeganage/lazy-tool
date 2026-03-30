package mcpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"lazy-tool/internal/connectors"
	"lazy-tool/internal/runtime"
	"lazy-tool/pkg/models"
)

// DirectProxy manages direct-mode tool/prompt/resource registration and supports
// periodic refresh to add new capabilities, remove stale ones, and skip sources
// whose circuit breakers are open (Feature 6: upstream health propagation).
type DirectProxy struct {
	mu    sync.Mutex
	srv   *mcp.Server
	stack *runtime.Stack
	log   *slog.Logger

	// tracked state: canonical_name → source_id
	tools     map[string]string
	prompts   map[string]string
	resources map[string]string // key = canonical_name, value = resource URI (needed for RemoveResources)
}

// NewDirectProxy creates a DirectProxy. Call RegisterAll() to perform initial registration.
func NewDirectProxy(srv *mcp.Server, stack *runtime.Stack, log *slog.Logger) *DirectProxy {
	return &DirectProxy{
		srv:       srv,
		stack:     stack,
		log:       log,
		tools:     make(map[string]string),
		prompts:   make(map[string]string),
		resources: make(map[string]string),
	}
}

// RegisterAll performs the initial bulk registration of all catalog capabilities.
func (dp *DirectProxy) RegisterAll() {
	dp.mu.Lock()
	defer dp.mu.Unlock()

	ctx := context.Background()
	recs, err := dp.stack.Store.ListAll(ctx)
	if err != nil {
		dp.log.Error("direct proxy: failed to list capabilities", "err", err)
		return
	}
	registered := 0
	for _, rec := range recs {
		if !dp.stack.Registry.SourceEnabled(rec.SourceID) {
			continue
		}
		dp.registerOne(rec)
		registered++
	}
	dp.log.Info("direct proxy: registered capabilities", "count", registered)
}

// Refresh loads the current catalog, diffs against tracked state, removes stale
// capabilities via SDK Remove methods, and registers new ones. Sources with open
// circuit breakers are skipped — their tools are removed and re-added when the
// circuit recovers.
func (dp *DirectProxy) Refresh(ctx context.Context) {
	dp.mu.Lock()
	defer dp.mu.Unlock()

	recs, err := dp.stack.Store.ListAll(ctx)
	if err != nil {
		dp.log.Error("direct proxy refresh: failed to list capabilities", "err", err)
		return
	}

	// Build the desired set from the catalog
	wantTools := make(map[string]string)
	wantPrompts := make(map[string]string)
	wantResources := make(map[string]string) // canonical → URI

	for _, rec := range recs {
		if !dp.stack.Registry.SourceEnabled(rec.SourceID) {
			continue
		}

		// Feature 6: skip tools from sources with open circuit breakers
		cb := dp.stack.Factory.CircuitBreakerFor(rec.SourceID)
		if cb != nil && cb.State() == connectors.CircuitOpen {
			continue
		}

		switch rec.Kind {
		case models.CapabilityKindTool:
			wantTools[rec.CanonicalName] = rec.SourceID
		case models.CapabilityKindPrompt:
			wantPrompts[rec.CanonicalName] = rec.SourceID
		case models.CapabilityKindResource:
			uri, uriErr := resourceReadURI(rec)
			if uriErr != nil {
				continue
			}
			wantResources[rec.CanonicalName] = uri
		}
	}

	// Remove stale tools
	var staleTools []string
	for name := range dp.tools {
		if _, ok := wantTools[name]; !ok {
			staleTools = append(staleTools, name)
		}
	}
	if len(staleTools) > 0 {
		dp.srv.RemoveTools(staleTools...)
		for _, name := range staleTools {
			delete(dp.tools, name)
		}
		dp.log.Info("direct proxy refresh: removed stale tools", "count", len(staleTools))
	}

	// Remove stale prompts
	var stalePrompts []string
	for name := range dp.prompts {
		if _, ok := wantPrompts[name]; !ok {
			stalePrompts = append(stalePrompts, name)
		}
	}
	if len(stalePrompts) > 0 {
		dp.srv.RemovePrompts(stalePrompts...)
		for _, name := range stalePrompts {
			delete(dp.prompts, name)
		}
		dp.log.Info("direct proxy refresh: removed stale prompts", "count", len(stalePrompts))
	}

	// Remove stale resources
	var staleResourceURIs []string
	for name, uri := range dp.resources {
		if _, ok := wantResources[name]; !ok {
			staleResourceURIs = append(staleResourceURIs, uri)
			delete(dp.resources, name)
		}
	}
	if len(staleResourceURIs) > 0 {
		dp.srv.RemoveResources(staleResourceURIs...)
		dp.log.Info("direct proxy refresh: removed stale resources", "count", len(staleResourceURIs))
	}

	// Register new capabilities
	var added int
	for _, rec := range recs {
		if !dp.stack.Registry.SourceEnabled(rec.SourceID) {
			continue
		}
		cb := dp.stack.Factory.CircuitBreakerFor(rec.SourceID)
		if cb != nil && cb.State() == connectors.CircuitOpen {
			continue
		}
		switch rec.Kind {
		case models.CapabilityKindTool:
			if _, exists := dp.tools[rec.CanonicalName]; !exists {
				dp.registerOne(rec)
				added++
			}
		case models.CapabilityKindPrompt:
			if _, exists := dp.prompts[rec.CanonicalName]; !exists {
				dp.registerOne(rec)
				added++
			}
		case models.CapabilityKindResource:
			if _, exists := dp.resources[rec.CanonicalName]; !exists {
				dp.registerOne(rec)
				added++
			}
		}
	}

	dp.log.Info("direct proxy refresh: complete",
		"tools", len(dp.tools),
		"prompts", len(dp.prompts),
		"resources", len(dp.resources),
		"added", added,
		"removed_tools", len(staleTools),
		"removed_prompts", len(stalePrompts),
		"removed_resources", len(staleResourceURIs),
	)
}

// registerOne registers a single capability and tracks it. Must be called with dp.mu held.
func (dp *DirectProxy) registerOne(rec models.CapabilityRecord) {
	switch rec.Kind {
	case models.CapabilityKindTool:
		registerDirectTool(dp.srv, dp.stack, dp.log, rec)
		dp.tools[rec.CanonicalName] = rec.SourceID
	case models.CapabilityKindPrompt:
		registerDirectPrompt(dp.srv, dp.stack, dp.log, rec)
		dp.prompts[rec.CanonicalName] = rec.SourceID
	case models.CapabilityKindResource:
		uri, err := resourceReadURI(rec)
		if err != nil {
			return
		}
		registerDirectResource(dp.srv, dp.stack, dp.log, rec)
		dp.resources[rec.CanonicalName] = uri
	}
}

func registerDirectTool(srv *mcp.Server, stack *runtime.Stack, log *slog.Logger, rec models.CapabilityRecord) {
	schema := parseInputSchema(rec.InputSchemaJSON)
	canonicalName := rec.CanonicalName
	srv.AddTool(&mcp.Tool{
		Name:        rec.CanonicalName,
		Description: rec.EffectiveSummary(),
		InputSchema: schema,
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input map[string]any
		if req.Params != nil && len(req.Params.Arguments) > 0 {
			_ = json.Unmarshal(req.Params.Arguments, &input)
		}
		res, _, err := ExecuteProxy(ctx, stack, log, canonicalName, input)
		return res, err
	})
}

func registerDirectPrompt(srv *mcp.Server, stack *runtime.Stack, log *slog.Logger, rec models.CapabilityRecord) {
	canonicalName := rec.CanonicalName
	srv.AddPrompt(&mcp.Prompt{
		Name:        rec.CanonicalName,
		Description: rec.EffectiveSummary(),
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		var args map[string]any
		if req.Params != nil && len(req.Params.Arguments) > 0 {
			args = make(map[string]any, len(req.Params.Arguments))
			for k, v := range req.Params.Arguments {
				args[k] = v
			}
		}
		res, _, err := ExecuteGetPrompt(ctx, stack, log, canonicalName, args)
		return res, err
	})
}

func registerDirectResource(srv *mcp.Server, stack *runtime.Stack, log *slog.Logger, rec models.CapabilityRecord) {
	canonicalName := rec.CanonicalName
	uri, err := resourceReadURI(rec)
	if err != nil {
		return // skip resource templates and records without URIs
	}
	srv.AddResource(&mcp.Resource{
		Name:        rec.CanonicalName,
		URI:         uri,
		Description: rec.EffectiveSummary(),
	}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		res, _, err := ExecuteReadResource(ctx, stack, log, canonicalName)
		return res, err
	})
}

// parseInputSchema converts a JSON schema string to a map suitable for mcp.Tool.InputSchema.
// Returns a minimal valid object schema if parsing fails.
func parseInputSchema(schemaJSON string) map[string]any {
	if schemaJSON == "" || schemaJSON == "{}" {
		return map[string]any{"type": "object"}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(schemaJSON), &m); err != nil {
		return map[string]any{"type": "object"}
	}
	if _, ok := m["type"]; !ok {
		m["type"] = "object"
	}
	return m
}
