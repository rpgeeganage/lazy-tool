package main

import (
	"errors"

	"github.com/spf13/cobra"

	"lazy-tool/internal/runtime"
	"lazy-tool/internal/web"
)

func newWebCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Run the local web UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := resolveConfigPath()
			if path == "" {
				return errors.New("config path required: use --config or LAZY_TOOL_X_CONFIG (fallback LAZY_TOOL_CONFIG)")
			}
			stack, err := runtime.OpenStack(path)
			if err != nil {
				return err
			}
			defer func() { _ = stack.Close() }()

			listenAddr := web.NormalizeListenAddr(addr)
			cmd.PrintErrf("Web UI available at http://%s\n", listenAddr)

			return web.ListenAndServe(listenAddr, stack)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8765", "listen host:port (if port omitted, :8765 is used)")
	return cmd
}
