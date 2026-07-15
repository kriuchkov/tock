//go:build darwin && !cgo

package commands

import (
	"github.com/go-faster/errors"
	"github.com/spf13/cobra"
)

// The macOS menu bar integration (fyne.io/systray) is implemented in
// Objective-C and needs cgo, so it is compiled out of CGO-disabled builds
// (e.g. the cross-compiled release binaries). Build from source with cgo
// enabled — the default for `go build`/`go install` on macOS — to get it.

// runTray is unavailable without cgo; the menu bar integration needs the
// Objective-C status bar bridge.
func runTray(cmd *cobra.Command) error {
	return errors.New(text(cmd, "tray.needs_cgo"))
}

// ensureTrayRunning is a no-op without cgo: there is no menu bar icon to spawn.
func ensureTrayRunning(*cobra.Command) {}

func installTrayAgent(cmd *cobra.Command) error {
	return errors.New(text(cmd, "tray.needs_cgo"))
}

func uninstallTrayAgent(cmd *cobra.Command) error {
	return errors.New(text(cmd, "tray.needs_cgo"))
}
