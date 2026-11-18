package commands

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// rootPassthroughFlags forwards the root flags that select the backend and data, so a tock
// process started from this one (the menu bar icon, an MCP server) tracks the same activities.
// Paths are made absolute: that process may run from another directory.
func rootPassthroughFlags(cmd *cobra.Command) []string {
	var args []string
	root := cmd.Root().PersistentFlags()
	for _, name := range []string{"backend", "file", "config", "lang"} {
		v, err := root.GetString(name)
		if err != nil || v == "" {
			continue
		}
		if name == "file" || name == "config" {
			if abs, absErr := filepath.Abs(v); absErr == nil {
				v = abs
			}
		}
		args = append(args, "--"+name, v)
	}
	return args
}

// binaryLooksUnstable reports whether exe sits in a temporary or source-tree location, from
// which an installed agent or client entry would break once the binary is moved, rebuilt
// elsewhere, or cleaned.
func binaryLooksUnstable(exe string) bool {
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
