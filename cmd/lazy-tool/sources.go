package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/spf13/cobra"

	"lazy-tool/internal/runtime"
	"lazy-tool/pkg/models"
)

func newSourcesCmd() *cobra.Command {
	var status bool
	var allSrc bool
	c := &cobra.Command{
		Use:   "sources",
		Short: "List configured MCP sources from the config file",
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
			if !status {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				srcs := stack.Registry.All()
				if allSrc {
					srcs = stack.Registry.AllConfigured()
				}
				return enc.Encode(srcs)
			}
			rows, err := stack.Store.ListSourceHealth(context.Background())
			if err != nil {
				return err
			}
			byID := make(map[string]struct {
				OK        bool
				Message   string
				UpdatedAt time.Time
			})
			for _, r := range rows {
				byID[r.SourceID] = struct {
					OK        bool
					Message   string
					UpdatedAt time.Time
				}{OK: r.OK, Message: r.Message, UpdatedAt: r.UpdatedAt}
			}
			type row struct {
				models.Source
				// ReindexState is ok | failed | unknown (no persisted source_health row yet).
				ReindexState       string  `json:"reindex_state"`
				LastReindexOK      *bool   `json:"last_reindex_ok,omitempty"`
				LastReindexMessage string  `json:"last_reindex_message,omitempty"`
				LastReindexAt      *string `json:"last_reindex_at,omitempty"`
			}
			out := make([]row, 0, len(stack.Registry.AllConfigured()))
			for _, src := range stack.Registry.AllConfigured() {
				r := row{Source: src, ReindexState: "unknown"}
				if h, ok := byID[src.ID]; ok {
					okCopy := h.OK
					r.LastReindexOK = &okCopy
					r.LastReindexMessage = h.Message
					s := h.UpdatedAt.UTC().Format(time.RFC3339)
					r.LastReindexAt = &s
					if h.OK {
						r.ReindexState = "ok"
					} else {
						r.ReindexState = "failed"
					}
				}
				out = append(out, r)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		},
	}
	c.Flags().BoolVar(&status, "status", false, "include last reindex outcome per source (from SQLite)")
	c.Flags().BoolVar(&allSrc, "all", false, "list all configured sources including disabled (default: enabled only)")
	return c
}
