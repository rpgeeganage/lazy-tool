package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"lazy-tool/internal/version"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("lazy-tool-x %s (%s)\n", version.Version, version.Commit)
		},
	}
}
