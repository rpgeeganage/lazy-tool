package main

import (
	"errors"

	"github.com/spf13/cobra"

	tea "github.com/charmbracelet/bubbletea"
	"lazy-tool/internal/runtime"
	"lazy-tool/internal/tui"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Interactive terminal UI",
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
			m := tui.NewModel(stack)
			p := tea.NewProgram(m, tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}
}
