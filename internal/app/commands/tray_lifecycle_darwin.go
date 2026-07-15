//go:build darwin && cgo

package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
)

const (
	trayLockDirPerm  = 0o750
	trayLockFilePerm = 0o600
)

// trayLockPath returns the single-instance lock file path, shared by all tock
// invocations for the current user.
func trayLockPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "tock-tray.lock")
	}
	dir := filepath.Join(home, ".tock")
	_ = os.MkdirAll(dir, trayLockDirPerm)
	return filepath.Join(dir, "tray.lock")
}

// acquireTrayLock takes an exclusive, non-blocking flock held for the lifetime
// of the returned file (closing it releases the lock). ok is false when another
// tray process already holds it.
func acquireTrayLock() (*os.File, bool) {
	f, err := os.OpenFile(trayLockPath(), os.O_CREATE|os.O_RDWR, trayLockFilePerm)
	if err != nil {
		return nil, false
	}
	fd := int(f.Fd()) //nolint:gosec // a file descriptor from *os.File always fits in int
	if flockErr := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); flockErr != nil {
		_ = f.Close()
		return nil, false
	}
	return f, true
}

// trayAlreadyRunning reports whether a tray process currently holds the lock.
func trayAlreadyRunning() bool {
	f, ok := acquireTrayLock()
	if !ok {
		return true
	}
	_ = f.Close() // release; we were only probing
	return false
}

// ensureTrayRunning spawns a detached menu bar icon when tray.enabled and
// tray.auto_start are set and no tray is already running. The spawned process
// runs `tock tray --until-idle`, so it closes itself once the activity stops.
func ensureTrayRunning(cmd *cobra.Command) {
	rt := getRuntime(cmd)
	if !rt.Config.Tray.Enabled || !rt.Config.Tray.AutoStart || trayAlreadyRunning() {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		return
	}

	args := append([]string{"tray", "--until-idle"}, trayPassthroughFlags(cmd)...)
	//nolint:noctx // a detached background process must outlive the parent command's context
	proc := exec.Command(exe, args...)
	proc.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach from the terminal

	if devnull, devErr := os.OpenFile(os.DevNull, os.O_RDWR, 0); devErr == nil {
		proc.Stdin, proc.Stdout, proc.Stderr = devnull, devnull, devnull
	}
	if startErr := proc.Start(); startErr != nil {
		return
	}
	_ = proc.Process.Release()
}

// trayPassthroughFlags forwards the root flags that select the backend/data so
// the spawned tray tracks the same activities as the current command.
func trayPassthroughFlags(cmd *cobra.Command) []string {
	var args []string
	root := cmd.Root().PersistentFlags()
	for _, name := range []string{"backend", "file", "config", "lang"} {
		if v, err := root.GetString(name); err == nil && v != "" {
			args = append(args, "--"+name, v)
		}
	}
	return args
}
