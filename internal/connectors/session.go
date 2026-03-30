// Session lifecycle (local-only):
// withSession is the boundary for transport → mcp.Client → Connect → Close (stdio; or one-shot HTTP).
// ListForIndex batches list_* in one session per call.
// Stdio uses a new OS process per withSession (isolation).
// HTTP may reuse a session per source when connectors.FactoryOpts.HTTPReuseUpstreamSession is true; otherwise one-shot like stdio.
//
// lazy-tool serve / reindex share one Factory from the Stack. The health --probe command builds its own Factory
// (see cmd/lazy-tool/health.go) so probe connects do not mutate or share sessions with a running server.
package connectors

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"lazy-tool/internal/metrics"
	"lazy-tool/pkg/models"
)

func transportFor(src models.Source, hc *http.Client) (mcp.Transport, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	switch src.Transport {
	case models.TransportHTTP:
		return &mcp.StreamableClientTransport{
			Endpoint:             src.URL,
			HTTPClient:           hc,
			DisableStandaloneSSE: true,
		}, nil
	case models.TransportStdio:
		cmd := exec.Command(src.Command, src.Args...) //nolint:gosec // user-configured MCP server command
		if src.Cwd != "" {
			cmd.Dir = filepath.Clean(src.Cwd)
		}
		if len(src.Env) > 0 {
			cmd.Env = os.Environ()
			for k, v := range src.Env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
		}
		return &mcp.CommandTransport{Command: cmd}, nil
	default:
		return nil, fmt.Errorf("unsupported transport %q", src.Transport)
	}
}

// withSession connects to upstream MCP, runs fn, then closes the session (stdio and one-shot HTTP).
// When reuse is set (HTTP + factory opt-in), the session is reused across calls until an error or Factory.Close.
func withSession(ctx context.Context, src models.Source, hc *http.Client, reuse httpSessionRunner, fn func(*mcp.ClientSession) error) error {
	if reuse != nil {
		return reuse.withSession(ctx, fn)
	}
	t, err := transportFor(src, hc)
	if err != nil {
		return err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "lazy-tool", Version: "0.1.0"}, nil)
	ses, err := client.Connect(ctx, t, nil)
	metrics.UpstreamMCPConnect(src.ID, err)
	if err != nil {
		return err
	}
	upstreamDebugf("session start source_id=%s transport=%v reuse=false", src.ID, src.Transport)
	defer func() { metrics.UpstreamMCPSessionClosed(src.ID, ses.Close()) }()
	return fn(ses)
}
