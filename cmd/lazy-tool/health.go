package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"lazy-tool/internal/app"
	"lazy-tool/internal/config"
	"lazy-tool/internal/connectors"
)

func newHealthCmd() *cobra.Command {
	var probe bool
	c := &cobra.Command{
		Use:   "health",
		Short: "Verify config loads and report basic status",
		Long: `Verify config loads and report basic status.

With --probe, lazy-tool-x creates a dedicated connectors.Factory (and MCP sessions) for each configured source.
That factory is separate from lazy-tool-x serve or reindex: probing does not attach to the server's upstream sessions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			log := slog.Default()
			path := resolveConfigPath()
			if path == "" {
				return fmt.Errorf("config path required: use --config or LAZY_TOOL_CONFIG")
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			a := app.New(cfg, log)
			name := cfg.App.Name
			if name == "" {
				name = "lazy-tool-x"
			}
			srcs, err := config.NormalizeSources(cfg.Sources)
			if err != nil {
				return err
			}
			reg, err := app.NewSourceRegistry(srcs)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(os.Stdout, "ok\tconfig=%s\tapp=%s\tsources=%d\n", path, name, len(reg.IDs())); err != nil {
				return err
			}
			_ = a
			if !probe {
				return nil
			}
			if len(reg.IDs()) == 0 {
				_, _ = fmt.Fprintln(os.Stdout, "probe:\t(no sources configured)")
				return nil
			}
			fact := connectors.NewFactory(connectors.FactoryOpts{
				HTTPReuseUpstreamSession: cfg.Connectors.HTTPReuseUpstreamSession,
				HTTPReuseIdleTimeout:     time.Duration(cfg.Connectors.HTTPReuseIdleTimeoutSeconds) * time.Second,
			})
			defer func() { _ = fact.Close() }()
			ctx := context.Background()
			for _, src := range reg.All() {
				conn, cerr := fact.New(ctx, src)
				if cerr != nil {
					if _, err := fmt.Fprintf(os.Stdout, "probe:\tsource=%s\tstatus=error\t%s\n", src.ID, cerr); err != nil {
						return err
					}
					continue
				}
				herr := conn.Health(ctx)
				if herr != nil {
					if _, err := fmt.Fprintf(os.Stdout, "probe:\tsource=%s\tstatus=error\t%s\n", src.ID, herr); err != nil {
						return err
					}
					continue
				}
				if _, err := fmt.Fprintf(os.Stdout, "probe:\tsource=%s\tstatus=ok\t(transport=%s)\n", src.ID, src.Transport); err != nil {
					return err
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&probe, "probe", false, "connect to each source via a fresh factory (not the serve stack) and run Health")
	return c
}
