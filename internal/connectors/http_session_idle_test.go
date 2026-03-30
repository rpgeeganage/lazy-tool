package connectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"lazy-tool/internal/metrics"
	"lazy-tool/pkg/models"
)

func TestHTTPReuse_idleTTL_recyclesSession(t *testing.T) {
	ctx := context.Background()
	up := &mcp.Implementation{Name: "idle-test", Version: "0.1"}
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

	var connects int32
	var idleRecycle int32
	prevConn := metrics.UpstreamMCPConnect
	metrics.UpstreamMCPConnect = func(id string, err error) {
		if err == nil {
			atomic.AddInt32(&connects, 1)
		}
		prevConn(id, err)
	}
	prevIdle := metrics.UpstreamMCPIdleSessionRecycled
	metrics.UpstreamMCPIdleSessionRecycled = func(id string) {
		atomic.AddInt32(&idleRecycle, 1)
		prevIdle(id)
	}
	t.Cleanup(func() {
		metrics.UpstreamMCPConnect = prevConn
		metrics.UpstreamMCPIdleSessionRecycled = prevIdle
	})

	f := NewFactory(FactoryOpts{
		HTTPReuseUpstreamSession: true,
		HTTPReuseIdleTimeout:     40 * time.Millisecond,
		Timeout:                  5 * time.Second,
	})
	t.Cleanup(func() { _ = f.Close() })

	src := models.Source{
		ID: "gw", Type: models.SourceTypeGateway, Transport: models.TransportHTTP,
		URL: ts.URL,
	}
	conn, err := f.New(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ListTools(ctx); err != nil {
		t.Fatal(err)
	}
	if c := atomic.LoadInt32(&connects); c != 1 {
		t.Fatalf("after first call: connects=%d", c)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := conn.ListTools(ctx); err != nil {
		t.Fatal(err)
	}
	if c := atomic.LoadInt32(&connects); c != 2 {
		t.Fatalf("after idle expiry want 2 connects, got %d", c)
	}
	if r := atomic.LoadInt32(&idleRecycle); r != 1 {
		t.Fatalf("want 1 idle recycle before reconnect, got %d", r)
	}
}
