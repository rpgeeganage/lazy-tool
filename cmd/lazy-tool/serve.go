package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"lazy-tool/internal/mcpserver"
	"lazy-tool/internal/runtime"
)

func newServeCmd() *cobra.Command {
	var (
		mode            string
		transport       string
		addr            string
		refreshInterval time.Duration
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run lazy-tool-x as an MCP server exposing search_tools (stdio) or all tools (direct/hybrid, stdio/http)",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := resolveConfigPath()
			if path == "" {
				return errors.New("config path required: use --config or LAZY_TOOL_CONFIG")
			}
			stack, err := runtime.OpenStack(path)
			if err != nil {
				return err
			}
			defer func() { _ = stack.Close() }()

			// Resolve mode: flag > config > default
			m := mode
			if m == "" && stack.Cfg != nil {
				m = stack.Cfg.Server.MCP.Mode
			}
			m = mcpserver.NormalizeMode(m)

			// Resolve transport: flag > config > default
			tr := transport
			if tr == "" && stack.Cfg != nil {
				tr = stack.Cfg.Server.MCP.Transport
			}
			if tr == "" {
				tr = "stdio"
			}
			tr = strings.TrimSpace(strings.ToLower(tr))

			// Resolve refresh interval: flag > config > disabled
			ri := refreshInterval
			if ri == 0 && stack.Cfg != nil && stack.Cfg.Server.MCP.RefreshIntervalSeconds > 0 {
				ri = time.Duration(stack.Cfg.Server.MCP.RefreshIntervalSeconds) * time.Second
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			// startRefresh launches a background goroutine that periodically calls dp.Refresh.
			startRefresh := func(dp *mcpserver.DirectProxy) {
				if dp == nil || ri <= 0 {
					return
				}
				slog.Info("direct proxy refresh enabled", "interval", ri)
				go func() {
					ticker := time.NewTicker(ri)
					defer ticker.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-ticker.C:
							dp.Refresh(ctx)
						}
					}
				}()
			}

			switch tr {
			case "http":
				if addr == "" {
					addr = ":8080"
				}
				handler, dp := mcpserver.NewHTTPHandler(stack, slog.Default(), m)
				startRefresh(dp)
				mux := http.NewServeMux()
				mux.Handle("/mcp", handler)
				mux.Handle("/mcp/", handler)
				slog.Info("serving MCP over HTTP", "addr", addr, "mode", m)
				server := &http.Server{Addr: addr, Handler: mux}
				go func() {
					<-ctx.Done()
					_ = server.Close()
				}()
				if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					return fmt.Errorf("http server: %w", err)
				}
				return nil
			default: // stdio
				bundle := mcpserver.NewServerWithMode(stack, slog.Default(), m)
				startRefresh(bundle.DirectProxy)
				err = mcpserver.RunStdioWithBundle(ctx, bundle)
				if err != nil {
					if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "server is closing: EOF") {
						return nil
					}
				}
				return err
			}
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "", "tool registration mode: search (default), direct, hybrid")
	cmd.Flags().StringVar(&transport, "transport", "", "server transport: stdio (default), http")
	cmd.Flags().StringVar(&addr, "addr", "", "listen address for HTTP transport (default :8080)")
	cmd.Flags().DurationVar(&refreshInterval, "refresh-interval", 0, "periodic catalog refresh interval for direct/hybrid mode (e.g. 10s, 1m); 0 disables")
	return cmd
}
