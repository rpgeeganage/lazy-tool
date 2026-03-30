package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"lazy-tool/internal/runtime"
)

func newCacheClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cache-clear",
		Short: "Clear the response cache",
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
			if stack.Cache == nil {
				fmt.Println("Cache is not enabled in config.")
				return nil
			}
			hits, misses, size := stack.Cache.Stats()
			stack.Cache.Clear()
			fmt.Printf("Cache cleared. Was: %d entries, %d hits, %d misses.\n", size, hits, misses)
			return nil
		},
	}
}
