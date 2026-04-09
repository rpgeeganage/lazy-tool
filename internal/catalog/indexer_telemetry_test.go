package catalog

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"lazy-tool/internal/app"
	"lazy-tool/internal/connectors"
	"lazy-tool/internal/storage"
	"lazy-tool/internal/summarizer"
	"lazy-tool/pkg/models"
)

type stubIndexerFactory struct {
	conn connectors.Connector
}

func (s stubIndexerFactory) New(ctx context.Context, src models.Source) (connectors.Connector, error) {
	_, _ = ctx, src
	return s.conn, nil
}

func (s stubIndexerFactory) CircuitBreakerFor(sourceID string) *connectors.CircuitBreaker {
	_ = sourceID
	return nil
}

func (s stubIndexerFactory) SeedCircuitBreaker(sourceID string, state connectors.CircuitState, failures int, lastFailedAt time.Time) {
	_, _, _, _ = sourceID, state, failures, lastFailedAt
}

func (s stubIndexerFactory) Close() error { return nil }

type stubIndexerConnector struct {
	snap *connectors.IndexSnapshot
}

func (s stubIndexerConnector) ListForIndex(ctx context.Context) (*connectors.IndexSnapshot, error) {
	_ = ctx
	return s.snap, nil
}

func (s stubIndexerConnector) Health(ctx context.Context) error { _ = ctx; return nil }
func (s stubIndexerConnector) SourceID() string                 { return "src1" }
func (s stubIndexerConnector) ListTools(ctx context.Context) ([]connectors.ToolMeta, error) {
	_ = ctx
	return nil, nil
}
func (s stubIndexerConnector) ListPrompts(ctx context.Context) ([]connectors.PromptMeta, error) {
	_ = ctx
	return nil, nil
}
func (s stubIndexerConnector) ListResources(ctx context.Context) ([]connectors.ResourceMeta, error) {
	_ = ctx
	return nil, nil
}
func (s stubIndexerConnector) ListResourceTemplates(ctx context.Context) ([]connectors.ResourceTemplateMeta, error) {
	_ = ctx
	return nil, nil
}
func (s stubIndexerConnector) CallTool(ctx context.Context, toolName string, arguments map[string]any) (*mcp.CallToolResult, error) {
	_, _, _ = ctx, toolName, arguments
	return nil, nil
}
func (s stubIndexerConnector) GetPrompt(ctx context.Context, name string, arguments map[string]string) (*mcp.GetPromptResult, error) {
	_, _, _ = ctx, name, arguments
	return nil, nil
}
func (s stubIndexerConnector) ReadResource(ctx context.Context, uri string) (*mcp.ReadResourceResult, error) {
	_ = ctx
	_ = uri
	return nil, nil
}
func (s stubIndexerConnector) Close() error { return nil }

type stubEmbedder struct{}

func (stubEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	_ = ctx
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

func (stubEmbedder) ModelName() string { return "stub" }

func TestIndexerRecordsEmbedAndReindexTelemetry(t *testing.T) {
	ctx := context.Background()
	st, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "indexer-telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	reg, err := app.NewSourceRegistry([]models.Source{{ID: "src1", Type: models.SourceTypeGateway, Transport: models.TransportHTTP}})
	if err != nil {
		t.Fatal(err)
	}
	ix := &Indexer{
		Registry: reg,
		Factory: stubIndexerFactory{conn: stubIndexerConnector{snap: &connectors.IndexSnapshot{
			Tools: []connectors.ToolMeta{{
				Name:        "echo",
				Description: "Echo input",
				InputSchema: []byte(`{"type":"object"}`),
			}},
		}}},
		Summary: summarizer.Noop{},
		Embed:   stubEmbedder{},
		EmbeddingTextStrategy: "auto",
		Store:   st,
		Log:     slog.Default(),
	}
	if err := ix.Run(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ListRecentOperations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.Operation] = true
	}
	if !seen["embed"] {
		t.Fatal("expected embed telemetry row")
	}
	if !seen["reindex"] {
		t.Fatal("expected reindex telemetry row")
	}
}
