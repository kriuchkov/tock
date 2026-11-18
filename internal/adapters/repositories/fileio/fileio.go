// Package fileio writes data files so that readers never see half a file and tock processes
// running side by side (CLI, menu bar, MCP server) don't lose each other's updates.
package fileio

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-faster/errors"
)

const (
	dirMode  = 0750
	fileMode = 0644
	lockMode = 0600
)

// WriteAtomic replaces the file at path with data through a temporary sibling and a rename.
// A symlinked path is followed, so the link itself stays in place, and an existing file keeps its mode.
func WriteAtomic(path string, data []byte) error {
	target := resolve(path)
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return errors.Wrap(err, "create directory")
	}

	mode := os.FileMode(fileMode)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return errors.Wrap(err, "create temporary file")
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return errors.Wrap(err, "write temporary file")
	}
	if err = tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return errors.Wrap(err, "chmod temporary file")
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errors.Wrap(err, "sync temporary file")
	}
	if err = tmp.Close(); err != nil {
		return errors.Wrap(err, "close temporary file")
	}
	if err = os.Rename(tmp.Name(), target); err != nil {
		return errors.Wrap(err, "replace file")
	}
	return nil
}

// Update runs fn while holding an exclusive lock on path, so a read-modify-write of the file
// doesn't interleave with one in another process. The lock lives in a hidden sibling file.
func Update(path string, fn func() error) error {
	lockPath := lockPathFor(resolve(path))
	if err := os.MkdirAll(filepath.Dir(lockPath), dirMode); err != nil {
		return errors.Wrap(err, "create lock directory")
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, lockMode)
	if err != nil {
		return errors.Wrap(err, "open lock file")
	}
	defer f.Close()

	if err = lock(f); err != nil {
		return errors.Wrap(err, "lock file")
	}
	defer func() { _ = unlock(f) }()

	return fn()
}

// lockPathFor returns the hidden sibling lock file of path: ".<name>.lock", or "<name>.lock" when
// the name is already hidden. It is never removed, so every process locks the same inode.
func lockPathFor(path string) string {
	name := filepath.Base(path)
	if !strings.HasPrefix(name, ".") {
		name = "." + name
	}
	return filepath.Join(filepath.Dir(path), name+".lock")
}

// resolve follows a symlink at path; a missing or unreadable path is used as given.
func resolve(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}
