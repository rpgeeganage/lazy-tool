package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"lazy-tool/internal/mcpserver"
	"lazy-tool/internal/runtime"
)

func newSearchCmd() *cobra.Command {
	var limit int
	var sources string
	var groupBySource bool
	var lexicalOnly bool
	var explainScores bool
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the local tool catalog",
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
			q := strings.Join(args, " ")
			var sids []string
			if sources != "" {
				for _, p := range strings.Split(sources, ",") {
					p = strings.TrimSpace(p)
					if p != "" {
						sids = append(sids, p)
					}
				}
			}
			var opts *mcpserver.SearchCallOpts
			if groupBySource || lexicalOnly || explainScores {
				opts = &mcpserver.SearchCallOpts{GroupBySource: groupBySource, LexicalOnly: lexicalOnly, ExplainScores: explainScores}
			}
			b, err := mcpserver.SearchToolsResultJSON(context.Background(), stack, q, limit, sids, opts)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "max results")
	cmd.Flags().StringVar(&sources, "sources", "", "comma-separated source ids")
	cmd.Flags().BoolVar(&groupBySource, "group", false, "group results by upstream source id")
	cmd.Flags().BoolVar(&lexicalOnly, "lexical", false, "skip embeddings and vector retrieval for this query")
	cmd.Flags().BoolVar(&explainScores, "explain-scores", false, "include pre-ranker score_breakdown per hit in JSON")
	return cmd
}
