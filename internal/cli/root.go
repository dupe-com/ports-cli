// Package cli wires the cobra command tree. The bare `ports` command opens
// the TUI; subcommands cover scripting and CI use.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dupe-com/ports-cli/internal/config"
	"github.com/dupe-com/ports-cli/internal/tui"
)

// Execute runs the CLI. Version info arrives from main's ldflags.
func Execute(version, commit, date string) error {
	root := &cobra.Command{
		Use:   "ports",
		Short: "See what's listening and reclaim ports in one keypress",
		Long: `ports — interactive TCP port manager.

Run with no arguments to open the TUI: a live, filterable table of every
listening port with one-keypress kill, favorites, watched-port
notifications, managed kubectl port-forwards, and Cloudflare tunnel
visibility.

Subcommands cover scripting: list (with --json), kill, watch, fwd.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			return tui.Run(cfg)
		},
	}

	root.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
	root.SetVersionTemplate("ports {{.Version}}\n")

	root.AddCommand(newListCmd(), newKillCmd(), newWatchCmd(), newFwdCmd())
	return root.Execute()
}
