package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewTrayCmd returns the command that runs the macOS menu bar (status bar)
// integration. The tray shows a live timer for the running activity and lets
// you start the last activity or stop the current one. It is opt-in via the
// tray.enabled config option and only supported on macOS.
func NewTrayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tray",
		Short: defaultText("tray.short"),
		RunE:  func(cmd *cobra.Command, _ []string) error { return runTrayCmd(cmd) },
	}
}

func runTrayCmd(cmd *cobra.Command) error {
	rt := getRuntime(cmd)
	if !rt.Config.Tray.Enabled {
		fmt.Fprintln(cmd.OutOrStdout(), text(cmd, "tray.disabled"))
		return nil
	}
	return runTray(cmd)
}
