package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

var configPath string

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	root := &cobra.Command{
		Use:   "lazy-tool-x",
		Short: "Local-first MCP discovery proxy for token-efficient tool search",
	}
	root.PersistentFlags().StringVar(&configPath, "config", "", "path to YAML config (or set LAZY_TOOL_CONFIG)")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newHealthCmd())
	root.AddCommand(newReindexCmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newSearchCmd())
	root.AddCommand(newInspectCmd())
	root.AddCommand(newSourcesCmd())
	root.AddCommand(newWebCmd())
	root.AddCommand(newTUICmd())
	root.AddCommand(newCatalogCmd())
	root.AddCommand(newPinCmd())
	root.AddCommand(newImportCmd())
	root.AddCommand(newCacheClearCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func resolveConfigPath() string {
	if configPath != "" {
		return configPath
	}
	if v := os.Getenv("LAZY_TOOL_CONFIG"); v != "" {
		return v
	}
	return ""
}
