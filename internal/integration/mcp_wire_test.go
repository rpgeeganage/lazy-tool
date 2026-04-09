package integration

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"lazy-tool/internal/catalog"
	"lazy-tool/internal/mcpserver"
	"lazy-tool/internal/runtime"
	"lazy-tool/pkg/models"
)

// §38.2: mock MCP over HTTP (streamable transport), reindex, search, proxy invoke.
func TestGatewayReindexSearchProxyInvoke(t *testing.T) {
	ctx := context.Background()

	up := &mcp.Implementation{Name: "lazy-tool-it-upstream", Version: "0.1"}
	srv := mcp.NewServer(up, nil)
	type echoIn struct {
		Msg string `json:"msg"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "echo", Description: "Echo msg"}, func(_ context.Context, _ *mcp.CallToolRequest, in echoIn) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ECHO:" + in.Msg}}}, map[string]any{"ok": true}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	root := t.TempDir()
	dbPath := filepath.Join(root, "data", "it.db")
	vecPath := filepath.Join(root, "data", "vec")
	cfgPath := filepath.Join(root, "config.yaml")
	cfgBody := strings.ReplaceAll(`app:
  name: lazy-tool-it
storage:
  sqlite_path: DBPATH
  vector_path: VECPATH
summary:
  enabled: false
embeddings:
  provider: noop
sources:
  - id: itgw
    type: gateway
    transport: http
    url: UPSTREAM
`, "DBPATH", dbPath)
	cfgBody = strings.ReplaceAll(cfgBody, "VECPATH", vecPath)
	cfgBody = strings.ReplaceAll(cfgBody, "UPSTREAM", ts.URL)
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o600); err != nil {
		t.Fatal(err)
	}

	stack, err := runtime.OpenStack(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stack.Close() })

	ix := &catalog.Indexer{
		Registry: stack.Registry,
		Factory:  stack.Factory,
		Summary:  stack.Summarizer,
		Embed:    stack.Embedder,
		EmbeddingTextStrategy: stack.Cfg.Embeddings.TextStrategy,
		Store:    stack.Store,
		Vec:      stack.Vec,
		Log:      slog.Default(),
	}
	if err := ix.Run(ctx); err != nil {
		t.Fatal(err)
	}

	inspectRaw, err := mcpserver.InspectCapabilityJSON(ctx, stack, "itgw__echo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(inspectRaw), "itgw__echo") {
		t.Fatalf("inspect JSON missing record: %s", inspectRaw)
	}

	ranked, err := stack.Search.Search(ctx, models.SearchQuery{Text: "echo", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked.Results) == 0 {
		t.Fatal("expected search hit for echo tool")
	}
	proxy := ranked.Results[0].ProxyToolName
	if want := "itgw__echo"; proxy != want {
		t.Fatalf("proxy name: got %q want %q", proxy, want)
	}

	res, _, err := mcpserver.ExecuteProxy(ctx, stack, slog.Default(), proxy, map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "ECHO:hello") {
		t.Fatalf("invoke result missing ECHO:hello: %s", raw)
	}
}

// §38.2 extension: prompts/get and resources/read through the same gateway stack as tools.
func TestGatewayPromptResourceProxy(t *testing.T) {
	ctx := context.Background()

	up := &mcp.Implementation{Name: "lazy-tool-it-upstream", Version: "0.1"}
	srv := mcp.NewServer(up, nil)

	srv.AddPrompt(&mcp.Prompt{
		Name:        "welcome",
		Description: "Greeting",
		Arguments:   []*mcp.PromptArgument{{Name: "name"}},
	}, func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		name := ""
		if req.Params.Arguments != nil {
			name = req.Params.Arguments["name"]
		}
		return &mcp.GetPromptResult{
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: "Hello, " + name}},
			},
		}, nil
	})

	srv.AddResource(&mcp.Resource{
		URI:      "itest://note",
		Name:     "note",
		MIMEType: "text/plain",
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{URI: req.Params.URI, MIMEType: "text/plain", Text: "integration-resource"},
			},
		}, nil
	})

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	root := t.TempDir()
	dbPath := filepath.Join(root, "data", "it.db")
	vecPath := filepath.Join(root, "data", "vec")
	cfgPath := filepath.Join(root, "config.yaml")
	cfgBody := strings.ReplaceAll(`app:
  name: lazy-tool-it
storage:
  sqlite_path: DBPATH
  vector_path: VECPATH
summary:
  enabled: false
embeddings:
  provider: noop
sources:
  - id: itgw
    type: gateway
    transport: http
    url: UPSTREAM
`, "DBPATH", dbPath)
	cfgBody = strings.ReplaceAll(cfgBody, "VECPATH", vecPath)
	cfgBody = strings.ReplaceAll(cfgBody, "UPSTREAM", ts.URL)
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o600); err != nil {
		t.Fatal(err)
	}

	stack, err := runtime.OpenStack(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stack.Close() })

	ix := &catalog.Indexer{
		Registry: stack.Registry,
		Factory:  stack.Factory,
		Summary:  stack.Summarizer,
		Embed:    stack.Embedder,
		EmbeddingTextStrategy: stack.Cfg.Embeddings.TextStrategy,
		Store:    stack.Store,
		Vec:      stack.Vec,
		Log:      slog.Default(),
	}
	if err := ix.Run(ctx); err != nil {
		t.Fatal(err)
	}

	promptRanked, err := stack.Search.Search(ctx, models.SearchQuery{Text: "welcome greeting", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var promptProxy string
	for _, h := range promptRanked.Results {
		if h.Kind == models.CapabilityKindPrompt {
			promptProxy = h.ProxyToolName
			break
		}
	}
	if promptProxy == "" {
		t.Fatal("expected search hit for welcome prompt")
	}
	if want := "itgw__p_welcome"; promptProxy != want {
		t.Fatalf("prompt proxy name: got %q want %q", promptProxy, want)
	}

	_, rawPrompt, err := mcpserver.ExecuteGetPrompt(ctx, stack, slog.Default(), promptProxy, map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawPrompt), "Hello, Ada") {
		t.Fatalf("get prompt result missing greeting: %s", rawPrompt)
	}

	resRanked, err := stack.Search.Search(ctx, models.SearchQuery{Text: "itest note", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var resProxy string
	for _, h := range resRanked.Results {
		if h.Kind == models.CapabilityKindResource && strings.Contains(h.ProxyToolName, "itest") {
			resProxy = h.ProxyToolName
			break
		}
	}
	if resProxy == "" {
		t.Fatal("expected search hit for itest resource")
	}

	_, rawRes, err := mcpserver.ExecuteReadResource(ctx, stack, slog.Default(), resProxy)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawRes), "integration-resource") {
		t.Fatalf("read resource result missing body: %s", rawRes)
	}
}
