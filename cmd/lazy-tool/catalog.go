package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"lazy-tool/internal/catalog"
	"lazy-tool/internal/runtime"
	"lazy-tool/pkg/models"
)

func newCatalogCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "catalog",
		Short: "Export or import indexed capabilities as JSON",
	}
	root.AddCommand(newCatalogExportCmd())
	root.AddCommand(newCatalogImportCmd())
	root.AddCommand(newCatalogSetSummaryCmd())
	return root
}

func newCatalogExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Write all capability rows to stdout as JSON (includes embeddings when present)",
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
			ctx := context.Background()
			all, err := stack.Store.ListAll(ctx)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(all)
		},
	}
}

func newCatalogImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import PATH",
		Short: "Upsert capabilities from a JSON array file (run reindex afterward to rebuild vectors)",
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
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			b, err := io.ReadAll(f)
			if err != nil {
				return err
			}
			var rows []models.CapabilityRecord
			if err := json.Unmarshal(b, &rows); err != nil {
				return fmt.Errorf("parse json: %w", err)
			}
			ctx := context.Background()
			for i := range rows {
				if err := stack.Store.UpsertCapability(ctx, rows[i]); err != nil {
					return fmt.Errorf("upsert %s: %w", rows[i].CanonicalName, err)
				}
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "imported %d rows — run `reindex` to refresh the vector index\n", len(rows))
			return nil
		},
	}
}

func newCatalogSetSummaryCmd() *cobra.Command {
	clear := false
	c := &cobra.Command{
		Use:   "set-summary PROXY_NAME [TEXT...]",
		Short: "Set or clear manual user_summary for one capability (canonical proxy name)",
		Args:  cobra.MinimumNArgs(1),
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
			proxy := args[0]
			text := strings.TrimSpace(strings.Join(args[1:], " "))
			if clear {
				text = ""
			}
			return catalog.SetUserSummary(context.Background(), stack.Store, proxy, text)
		},
	}
	c.Flags().BoolVar(&clear, "clear", false, "clear manual summary override")
	return c
}
