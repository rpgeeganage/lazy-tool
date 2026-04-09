package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"lazy-tool/internal/catalog"
	"lazy-tool/internal/runtime"
)

func newReindexCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Fetch upstream tools and rebuild local catalog + vector index",
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
			ix := &catalog.Indexer{
				Registry: stack.Registry,
				Factory:  stack.Factory,
				Summary:  stack.Summarizer,
				Embed:    stack.Embedder,
				EmbeddingTextStrategy: stack.Cfg.Embeddings.TextStrategy,
				AutoRefineVague: stack.Cfg.Summary.AutoRefine,
				VaguenessThreshold: stack.Cfg.Summary.VaguenessThreshold,
				SchemaEnrichment: stack.Cfg.Summary.SchemaEnrichment,
				Store:    stack.Store,
				Vec:      stack.Vec,
				Log:      slog.Default(),
			}
			if dryRun {
				result, err := ix.DryRun(context.Background())
				if err != nil {
					return err
				}
				totalNew, totalUpdated, totalUnchanged, totalStale := 0, 0, 0, 0
				for _, sr := range result.PerSource {
					status := "ok"
					if sr.Error != nil {
						status = fmt.Sprintf("error: %v", sr.Error)
					}
					fmt.Printf("  %-20s  new=%-3d  updated=%-3d  unchanged=%-3d  stale=%-3d  %s\n",
						sr.SourceID, sr.New, sr.Updated, sr.Unchanged, sr.Stale, status)
					for _, change := range sr.Changes {
						fmt.Printf("    %-7s  %-8s  %s\n", change.Status, change.Kind, change.CanonicalName)
					}
					totalNew += sr.New
					totalUpdated += sr.Updated
					totalUnchanged += sr.Unchanged
					totalStale += sr.Stale
				}
				fmt.Printf("\n  total: %d new, %d updated, %d unchanged, %d would be removed\n",
					totalNew, totalUpdated, totalUnchanged, totalStale)
				return nil
			}
			return ix.Run(context.Background())
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would change without writing to the catalog")
	return cmd
}
