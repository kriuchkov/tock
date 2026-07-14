//go:build !darwin

package commands

import (
	"github.com/go-faster/errors"
	"github.com/spf13/cobra"
)

// runTray is unavailable on non-macOS platforms; the menu bar integration
// relies on the macOS status bar API.
func runTray(cmd *cobra.Command) error {
	return errors.New(text(cmd, "tray.unsupported"))
}
