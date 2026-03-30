package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"lazy-tool/internal/cache"
	"lazy-tool/internal/connectors"
	"lazy-tool/internal/metrics"
	"lazy-tool/internal/runtime"
	"lazy-tool/internal/tracing"
	"lazy-tool/pkg/models"
)

func getCapabilityByCanonicalName(ctx context.Context, stack *runtime.Stack, proxyName string) (models.CapabilityRecord, error) {
	rec, err := stack.Store.GetByCanonicalName(ctx, proxyName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rec, fmt.Errorf(
				"unknown proxy_tool_name %q; call search_tools first and reuse an exact proxy_tool_name from results",
				proxyName,
			)
		}
		return rec, err
	}
	return rec, nil
}

// proxyResult bundles the resolved record, connector, and circuit breaker for a proxy call.
type proxyResult struct {
	rec  models.CapabilityRecord
	conn connectors.Connector
	cb   *connectors.CircuitBreaker
}

// withProxyConn resolves a proxy name to a live upstream connection, validating kind and circuit state.
func withProxyConn(ctx context.Context, stack *runtime.Stack, log *slog.Logger,
	proxyName string, expectedKind models.CapabilityKind, kindLabel string,
) (proxyResult, error) {
	rec, err := getCapabilityByCanonicalName(ctx, stack, proxyName)
	if err != nil {
		tracing.LogInvocation(ctx, log, proxyName, "", "", err)
		return proxyResult{}, err
	}
	src, ok := stack.Registry.Get(rec.SourceID)
	if !ok {
		e := fmt.Errorf("unknown source %q", rec.SourceID)
		tracing.LogInvocation(ctx, log, proxyName, rec.SourceID, rec.OriginalName, e)
		return proxyResult{}, e
	}
	if rec.Kind != expectedKind {
		e := fmt.Errorf("%s (kind=%s); use search hits with kind=%s", kindLabel, rec.Kind, expectedKind)
		tracing.LogInvocation(ctx, log, proxyName, rec.SourceID, rec.OriginalName, e)
		return proxyResult{}, e
	}
	cb := stack.Factory.CircuitBreakerFor(rec.SourceID)
	if cb != nil {
		if err := cb.Allow(); err != nil {
			e := fmt.Errorf("source %q: %w", rec.SourceID, err)
			tracing.LogInvocation(ctx, log, proxyName, rec.SourceID, rec.OriginalName, e)
			return proxyResult{}, e
		}
	}
	conn, err := stack.Factory.New(ctx, src)
	if err != nil {
		recordCircuitAndTrace(proxyResult{rec: rec, cb: cb}, log, ctx, proxyName, err, "")
		return proxyResult{}, err
	}
	return proxyResult{rec: rec, conn: conn, cb: cb}, nil
}

// recordCircuitAndTrace records success/failure on the circuit breaker, fires metrics hooks, and logs the invocation.
func recordCircuitAndTrace(pr proxyResult, log *slog.Logger, ctx context.Context, proxyName string, err error, traceNameOverride string) {
	if pr.cb != nil {
		if err != nil {
			pr.cb.RecordFailure()
			if pr.cb.State() == connectors.CircuitOpen {
				metrics.CircuitBreakerTripped(pr.rec.SourceID)
			}
		} else {
			wasHalfOpen := pr.cb.State() == connectors.CircuitHalfOpen
			pr.cb.RecordSuccess()
			if wasHalfOpen {
				metrics.CircuitBreakerReset(pr.rec.SourceID)
			}
		}
	}
	tool := pr.rec.OriginalName
	if traceNameOverride != "" {
		tool = traceNameOverride
	}
	tracing.LogInvocation(ctx, log, proxyName, pr.rec.SourceID, tool, err)
}

// isCacheable checks tool annotations to determine if a tool result should be cached.
// Tools with destructiveHint=true are never cached. Tools with readOnlyHint=false are not cached.
// Tools with no annotations are assumed cacheable for backward compatibility.
func isCacheable(rec models.CapabilityRecord) bool {
	var meta struct {
		Annotations *struct {
			ReadOnlyHint    bool  `json:"readOnlyHint"`
			DestructiveHint *bool `json:"destructiveHint"`
		} `json:"annotations"`
	}
	_ = json.Unmarshal([]byte(rec.MetadataJSON), &meta)
	if meta.Annotations == nil {
		return true // no annotations = assume cacheable (backward compat)
	}
	if meta.Annotations.DestructiveHint != nil && *meta.Annotations.DestructiveHint {
		return false
	}
	return meta.Annotations.ReadOnlyHint
}

// ExecuteProxy routes a proxy tool name to the correct upstream MCP server and returns the raw result.
func ExecuteProxy(ctx context.Context, stack *runtime.Stack, log *slog.Logger, proxyName string, input map[string]any) (*mcp.CallToolResult, []byte, error) {
	if input == nil {
		input = map[string]any{}
	}

	// Resolve the record first (cheap SQLite lookup) so we can check annotations before connecting upstream.
	rec, err := getCapabilityByCanonicalName(ctx, stack, proxyName)
	if err != nil {
		tracing.LogInvocation(ctx, log, proxyName, "", "", err)
		return nil, nil, err
	}

	// Check cache before connecting upstream — annotations determine cacheability
	cacheable := isCacheable(rec)
	var cacheKey string
	if c := stack.Cache; c != nil && cacheable {
		cacheKey = cache.Key(proxyName, input)
		if raw, ok := c.Get(cacheKey); ok {
			var res mcp.CallToolResult
			if err := json.Unmarshal(raw, &res); err == nil {
				log.Info("cache_hit", "proxy", proxyName)
				return &res, raw, nil
			}
		}
	}

	pr, err := withProxyConn(ctx, stack, log, proxyName, models.CapabilityKindTool, "invoke_proxy_tool only supports tools")
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = pr.conn.Close() }()

	res, err := pr.conn.CallTool(ctx, pr.rec.OriginalName, input)
	recordCircuitAndTrace(pr, log, ctx, proxyName, err, "")
	if err != nil {
		return nil, nil, err
	}
	raw, _ := json.Marshal(res)

	// Store in cache if enabled, cacheable, and source is not excluded
	if c := stack.Cache; c != nil && cacheKey != "" && cacheable && !c.IsSourceExcluded(pr.rec.SourceID) {
		c.Put(cacheKey, raw)
	}

	return res, raw, nil
}

func stringArgumentsFromAny(m map[string]any) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			out[k] = t
		default:
			out[k] = fmt.Sprint(t)
		}
	}
	return out
}

// resourceReadURI returns the MCP resource URI for a catalog row from resources/list (not templates).
func resourceReadURI(rec models.CapabilityRecord) (string, error) {
	var meta struct {
		URI              string `json:"uri"`
		ResourceTemplate bool   `json:"resource_template"`
	}
	_ = json.Unmarshal([]byte(rec.MetadataJSON), &meta)
	if meta.ResourceTemplate {
		return "", fmt.Errorf("read_proxy_resource does not support resource templates (canonical %q); use a concrete resource from search", rec.CanonicalName)
	}
	if meta.URI != "" {
		return meta.URI, nil
	}
	if strings.Contains(rec.OriginalName, "://") {
		return rec.OriginalName, nil
	}
	return "", fmt.Errorf("catalog record has no resource URI (canonical %q)", rec.CanonicalName)
}

// ExecuteGetPrompt loads a prompt from the upstream MCP server named in the catalog record.
func ExecuteGetPrompt(ctx context.Context, stack *runtime.Stack, log *slog.Logger, proxyName string, arguments map[string]any) (*mcp.GetPromptResult, []byte, error) {
	pr, err := withProxyConn(ctx, stack, log, proxyName, models.CapabilityKindPrompt, "get_proxy_prompt only supports prompts")
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = pr.conn.Close() }()
	strArgs := stringArgumentsFromAny(arguments)
	res, err := pr.conn.GetPrompt(ctx, pr.rec.OriginalName, strArgs)
	recordCircuitAndTrace(pr, log, ctx, proxyName, err, "")
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return res, nil, err
	}
	return res, raw, nil
}

// ExecuteReadResource reads a concrete resource URI from the upstream MCP server.
func ExecuteReadResource(ctx context.Context, stack *runtime.Stack, log *slog.Logger, proxyName string) (*mcp.ReadResourceResult, []byte, error) {
	pr, err := withProxyConn(ctx, stack, log, proxyName, models.CapabilityKindResource, "read_proxy_resource only supports resources")
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = pr.conn.Close() }()
	uri, err := resourceReadURI(pr.rec)
	if err != nil {
		tracing.LogInvocation(ctx, log, proxyName, pr.rec.SourceID, pr.rec.OriginalName, err)
		return nil, nil, err
	}
	res, err := pr.conn.ReadResource(ctx, uri)
	recordCircuitAndTrace(pr, log, ctx, proxyName, err, uri)
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return res, nil, err
	}
	return res, raw, nil
}
