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
	cmd := &cobra.Command{
		Use:   "tray",
		Short: defaultText("tray.short"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runTrayCmd(cmd) },
	}
	// Internal flag used by `tock start` auto-spawn: quit once the activity that
	// was running at launch is stopped.
	cmd.Flags().Bool("until-idle", false, "")
	_ = cmd.Flags().MarkHidden("until-idle")

	cmd.AddCommand(newTrayInstallCmd())
	cmd.AddCommand(newTrayUninstallCmd())
	return cmd
}

func newTrayInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: defaultText("tray.install.short"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return installTrayAgent(cmd) },
	}
}

func newTrayUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: defaultText("tray.uninstall.short"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return uninstallTrayAgent(cmd) },
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
