package mcpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"lazy-tool/internal/app"
	"lazy-tool/internal/connectors"
	"lazy-tool/internal/runtime"
	"lazy-tool/internal/storage"
	"lazy-tool/pkg/models"
)

type stubProxyConnector struct {
	res *mcp.CallToolResult
	err error
}

func (s stubProxyConnector) ListForIndex(ctx context.Context) (*connectors.IndexSnapshot, error) {
	_ = ctx
	return nil, nil
}

func (s stubProxyConnector) Health(ctx context.Context) error {
	_ = ctx
	return nil
}

func (s stubProxyConnector) CallTool(ctx context.Context, toolName string, arguments map[string]any) (*mcp.CallToolResult, error) {
	_, _, _ = ctx, toolName, arguments
	return s.res, s.err
}

func (s stubProxyConnector) GetPrompt(ctx context.Context, name string, arguments map[string]string) (*mcp.GetPromptResult, error) {
	_, _, _ = ctx, name, arguments
	return nil, nil
}

func (s stubProxyConnector) ReadResource(ctx context.Context, uri string) (*mcp.ReadResourceResult, error) {
	_ = ctx
	_ = uri
	return nil, nil
}

func (s stubProxyConnector) SourceID() string { return "src1" }
func (s stubProxyConnector) ListTools(ctx context.Context) ([]connectors.ToolMeta, error) {
	_ = ctx
	return nil, nil
}
func (s stubProxyConnector) ListPrompts(ctx context.Context) ([]connectors.PromptMeta, error) {
	_ = ctx
	return nil, nil
}
func (s stubProxyConnector) ListResources(ctx context.Context) ([]connectors.ResourceMeta, error) {
	_ = ctx
	return nil, nil
}
func (s stubProxyConnector) ListResourceTemplates(ctx context.Context) ([]connectors.ResourceTemplateMeta, error) {
	_ = ctx
	return nil, nil
}
func (s stubProxyConnector) Close() error { return nil }

type stubFactory struct{ conn connectors.Connector }

func (s stubFactory) New(ctx context.Context, src models.Source) (connectors.Connector, error) {
	_, _ = ctx, src
	return s.conn, nil
}

func (s stubFactory) CircuitBreakerFor(sourceID string) *connectors.CircuitBreaker {
	_ = sourceID
	return nil
}

func (s stubFactory) SeedCircuitBreaker(sourceID string, state connectors.CircuitState, failures int, lastFailedAt time.Time) {
	_, _, _, _ = sourceID, state, failures, lastFailedAt
}

func (s stubFactory) Close() error { return nil }

func TestExecuteProxyRecordsProxyTelemetry(t *testing.T) {
	ctx := context.Background()
	st, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "proxy-telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	rec := models.CapabilityRecord{
		ID:              "1",
		Kind:            models.CapabilityKindTool,
		SourceID:        "src1",
		SourceType:      "gateway",
		CanonicalName:   "src1__echo",
		OriginalName:    "echo",
		SearchText:      "echo",
		InputSchemaJSON: "{}",
		MetadataJSON:    string(mustJSON(map[string]any{"annotations": map[string]any{"readOnlyHint": true}})),
		VersionHash:     "v1",
		LastSeenAt:      time.Now().UTC(),
	}
	if err := st.UpsertCapability(ctx, rec); err != nil {
		t.Fatal(err)
	}
	reg, err := app.NewSourceRegistry([]models.Source{{ID: "src1", Type: models.SourceTypeGateway, Transport: models.TransportHTTP}})
	if err != nil {
		t.Fatal(err)
	}
	stack := &runtime.Stack{
		Store:    st,
		Registry: reg,
		Factory: stubFactory{conn: stubProxyConnector{res: &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
		}}},
	}
	_, _, err = ExecuteProxy(ctx, stack, slog.Default(), "src1__echo", map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListRecentOperations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].Operation != "proxy_invoke" {
		t.Fatalf("expected proxy_invoke row, got %#v", rows)
	}
	if rows[0].SourceID != "src1" {
		t.Fatalf("source = %q want src1", rows[0].SourceID)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
