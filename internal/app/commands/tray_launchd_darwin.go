//go:build darwin

package commands

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"
)

const (
	trayLaunchAgentLabel = "com.kriuchkov.tock.tray"
	trayPlistDirPerm     = 0o750
	trayPlistFilePerm    = 0o600

	trayBootoutPolls    = 20
	trayBootoutInterval = 100 * time.Millisecond
)

// trayPlistPath is the per-user LaunchAgent path for the menu bar icon.
func trayPlistPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, "Library", "LaunchAgents", trayLaunchAgentLabel+".plist")
}

// trayPlistContent builds a LaunchAgent that runs `tock tray` at login and
// restarts it only if it crashes (so the Quit menu item still quits).
func trayPlistContent(exe string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>tray</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`, xmlEscape(trayLaunchAgentLabel), xmlEscape(exe))
}

// xmlEscape escapes a string for use as XML element content.
func xmlEscape(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}

// installTrayAgent writes the LaunchAgent and (re)loads it so the menu bar icon
// starts now and at every login.
func installTrayAgent(cmd *cobra.Command) error {
	if !getRuntime(cmd).Config.Tray.Enabled {
		return errors.New(text(cmd, "tray.install.needs_enabled"))
	}

	exe, err := os.Executable()
	if err != nil {
		return errors.Wrap(err, "resolve executable")
	}
	if trayBinaryLooksUnstable(exe) {
		fmt.Fprintln(cmd.ErrOrStderr(), text(cmd, "tray.install.unstable_path", exe))
	}

	plistPath := trayPlistPath()
	if mkErr := os.MkdirAll(filepath.Dir(plistPath), trayPlistDirPerm); mkErr != nil {
		return errors.Wrap(mkErr, "create LaunchAgents dir")
	}
	if wErr := os.WriteFile(plistPath, []byte(trayPlistContent(exe)), trayPlistFilePerm); wErr != nil {
		return errors.Wrap(wErr, "write launch agent")
	}

	if lErr := reloadTrayAgent(cmd, plistPath); lErr != nil {
		return lErr
	}

	fmt.Fprintln(cmd.OutOrStdout(), text(cmd, "tray.install.done"))
	return nil
}

// uninstallTrayAgent unloads and removes the LaunchAgent.
func uninstallTrayAgent(cmd *cobra.Command) error {
	_ = runLaunchctl(cmd, "bootout", trayServiceTarget())

	if rmErr := os.Remove(trayPlistPath()); rmErr != nil && !os.IsNotExist(rmErr) {
		return errors.Wrap(rmErr, "remove launch agent")
	}

	fmt.Fprintln(cmd.OutOrStdout(), text(cmd, "tray.uninstall.done"))
	return nil
}

// reloadTrayAgent unloads any running instance, waits for launchd to finish the
// asynchronous teardown, then bootstraps the (possibly updated) plist. bootstrap
// is retried once because it can transiently fail while the old instance exits.
func reloadTrayAgent(cmd *cobra.Command, plistPath string) error {
	target := trayServiceTarget()

	if trayServiceLoaded(cmd, target) {
		_ = runLaunchctl(cmd, "bootout", target)
		for i := 0; i < trayBootoutPolls && trayServiceLoaded(cmd, target); i++ {
			time.Sleep(trayBootoutInterval)
		}
	}

	domain := trayDomainTarget()
	if err := runLaunchctl(cmd, "bootstrap", domain, plistPath); err == nil {
		return nil
	}

	// bootstrap can transiently fail while the old instance finishes exiting;
	// retry once and report that attempt's output on failure.
	time.Sleep(trayBootoutInterval)
	if out, err := launchctlOutput(cmd, "bootstrap", domain, plistPath); err != nil {
		return errors.Wrapf(err, "launchctl bootstrap: %s", out)
	}
	return nil
}

// trayBinaryLooksUnstable reports whether exe sits in a temporary or source-tree
// location, from which a login agent would break once the binary is moved,
// rebuilt elsewhere, or cleaned.
func trayBinaryLooksUnstable(exe string) bool {
	if strings.HasPrefix(exe, os.TempDir()) {
		return true
	}
	dir := filepath.Dir(exe)
	for _, marker := range []string{"go.mod", ".git"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

func trayServiceLoaded(cmd *cobra.Command, target string) bool {
	return runLaunchctl(cmd, "print", target) == nil
}

func trayDomainTarget() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func trayServiceTarget() string {
	return trayDomainTarget() + "/" + trayLaunchAgentLabel
}

func runLaunchctl(cmd *cobra.Command, args ...string) error {
	return exec.CommandContext(cmd.Context(), "launchctl", args...).Run()
}

func launchctlOutput(cmd *cobra.Command, args ...string) (string, error) {
	out, err := exec.CommandContext(cmd.Context(), "launchctl", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
