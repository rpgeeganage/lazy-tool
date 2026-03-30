package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"lazy-tool/internal/config"
)

func newImportCmd() *cobra.Command {
	var (
		from    string
		write   bool
		outPath string
	)
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import MCP server configs from Claude Desktop, Cursor, or VS Code",
		Long:  "Discovers MCP server configurations from IDE config files and generates a lazy-tool YAML config.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var hosts []config.IDEHost
			if from != "" {
				switch strings.TrimSpace(strings.ToLower(from)) {
				case "claude":
					hosts = append(hosts, config.HostClaude)
				case "cursor":
					hosts = append(hosts, config.HostCursor)
				case "vscode":
					hosts = append(hosts, config.HostVSCode)
				default:
					return fmt.Errorf("unknown host %q (supported: claude, cursor, vscode)", from)
				}
			}

			sources, err := config.DiscoverSources(hosts...)
			if err != nil {
				return fmt.Errorf("discover sources: %w", err)
			}
			if len(sources) == 0 {
				fmt.Fprintln(os.Stderr, "No MCP servers found in any IDE config.")
				return nil
			}

			sources, dedupWarnings := config.DeduplicateSources(sources)
			for _, w := range dedupWarnings {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
			}

			fmt.Fprintf(os.Stderr, "Discovered %d MCP server(s)\n", len(sources))
			for _, s := range sources {
				fmt.Fprintf(os.Stderr, "  - %s (%s/%s)\n", s.ID, s.Type, s.Transport)
			}

			yamlOut := config.GenerateConfigYAML(sources, "")

			if !write {
				fmt.Print(yamlOut)
				return nil
			}

			// Write to config file
			target := outPath
			if target == "" {
				target = resolveConfigPath()
			}
			if target == "" {
				home, _ := os.UserHomeDir()
				target = filepath.Join(home, ".lazy-tool", "config.yaml")
			}

			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create config dir: %w", err)
			}
			if err := os.WriteFile(target, []byte(yamlOut), 0o644); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Config written to %s\n", target)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "scan specific host only: claude, cursor, vscode")
	cmd.Flags().BoolVar(&write, "write", false, "write config to file instead of stdout")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "output file path (with --write)")
	return cmd
}
