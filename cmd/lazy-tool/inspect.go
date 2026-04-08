package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"lazy-tool/internal/catalog"
	"lazy-tool/internal/runtime"
)

func newInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <proxy_tool_name>",
		Short: "Show one indexed capability by canonical proxy name",
		Args:  cobra.ExactArgs(1),
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
			v, err := catalog.BuildInspectView(context.Background(), stack.Store, stack.Registry, args[0])
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "# provenance: source_id=%q kind=%s canonical=%q\n",
				v.Record.SourceID, v.Record.Kind, v.Record.CanonicalName)
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(v)
		},
	}
}
