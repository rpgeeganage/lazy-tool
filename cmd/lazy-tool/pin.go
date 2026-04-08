package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"lazy-tool/internal/runtime"
)

func newPinCmd() *cobra.Command {
	add := &cobra.Command{
		Use:   "add <proxy_tool_name>",
		Short: "Pin a capability by canonical proxy name (favorites boost in search)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return pinMutate(args[0], true)
		},
	}
	rm := &cobra.Command{
		Use:     "remove <proxy_tool_name>",
		Aliases: []string{"rm"},
		Short:   "Remove a pinned capability",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return pinMutate(args[0], false)
		},
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List pinned capability ids and canonical names",
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
			ids, err := stack.Store.ListFavoriteIDs(ctx)
			if err != nil {
				return err
			}
			type row struct {
				ID            string `json:"capability_id"`
				CanonicalName string `json:"proxy_tool_name,omitempty"`
				SourceID      string `json:"source_id,omitempty"`
			}
			var out []row
			for _, id := range ids {
				r := row{ID: id}
				if rec, err := stack.Store.GetCapability(ctx, id); err == nil {
					r.CanonicalName = rec.CanonicalName
					r.SourceID = rec.SourceID
				}
				out = append(out, r)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		},
	}
	root := &cobra.Command{
		Use:   "pin",
		Short: "Manage pinned (favorite) catalog capabilities",
	}
	root.AddCommand(add, rm, list)
	return root
}

func pinMutate(canonical string, add bool) error {
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
	rec, err := stack.Store.GetByCanonicalName(ctx, canonical)
	if err != nil {
		return err
	}
	if add {
		if err := stack.Store.AddFavorite(ctx, rec.ID); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(os.Stdout, "pinned %q (%s)\n", canonical, rec.ID)
		return nil
	}
	if err := stack.Store.RemoveFavorite(ctx, rec.ID); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "unpinned %q (%s)\n", canonical, rec.ID)
	return nil
}
