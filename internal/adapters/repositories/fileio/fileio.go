// Package fileio writes data files so that readers never see half a file and tock processes
// running side by side (CLI, menu bar, MCP server) don't lose each other's updates.
package fileio

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	return WriteAtomicAll([]File{{Path: path, Data: data}})
}

// File is one file of WriteAtomicAll: its path and its new content.
type File struct {
	Path string
	Data []byte
}

// WriteAtomicAll replaces several files as WriteAtomic does, in the order given, but writes and syncs
// every temporary file before it renames the first one into place. A failure before the renames
// leaves every file as it was; only a crash between two renames leaves the first ones replaced.
func WriteAtomicAll(files []File) error {
	staged := make([]stagedFile, 0, len(files))
	defer func() {
		for _, s := range staged {
			_ = os.Remove(s.tmp)
		}
	}()
	for _, f := range files {
		s, err := stage(f.Path, f.Data)
		if err != nil {
			return err
		}
		staged = append(staged, s)
	}
	for _, s := range staged {
		if err := os.Rename(s.tmp, s.target); err != nil {
			return errors.Wrap(err, "replace file")
		}
	}
	return nil
}

// stagedFile is a temporary file written and synced, waiting to be renamed over target.
type stagedFile struct {
	tmp    string
	target string
}

// stage writes data to a temporary sibling of the file at path, with the file's mode, and syncs it.
// A symlinked path is followed.
func stage(path string, data []byte) (stagedFile, error) {
	target := resolve(path)
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return stagedFile{}, errors.Wrap(err, "create directory")
	}

	mode := os.FileMode(fileMode)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return stagedFile{}, errors.Wrap(err, "create temporary file")
	}
	if err = fill(tmp, data, mode); err != nil {
		_ = os.Remove(tmp.Name())
		return stagedFile{}, err
	}
	return stagedFile{tmp: tmp.Name(), target: target}, nil
}

// fill writes data to a temporary file, gives it mode, syncs it and closes it.
func fill(tmp *os.File, data []byte, mode os.FileMode) error {
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return errors.Wrap(err, "write temporary file")
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return errors.Wrap(err, "chmod temporary file")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errors.Wrap(err, "sync temporary file")
	}
	if err := tmp.Close(); err != nil {
		return errors.Wrap(err, "close temporary file")
	}
	return nil
}

// Update runs fn while holding an exclusive lock on path, so a read-modify-write of the file
// doesn't interleave with one in another process. The lock lives in a hidden sibling file.
func Update(path string, fn func() error) error {
	f, err := openLock(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if err = lock(f); err != nil {
		return errors.Wrap(err, "lock file")
	}
	defer func() { _ = unlock(f) }()

	return fn()
}

// lockRetry is how often UpdateContext tries again for a lock another process holds.
const lockRetry = 20 * time.Millisecond

// UpdateContext is Update that stops waiting for the lock once ctx is done, and then returns ctx's
// error without running fn.
func UpdateContext(ctx context.Context, path string, fn func() error) error {
	f, err := openLock(path)
	if err != nil {
		return err
	}
	defer f.Close()

	retry := time.NewTimer(lockRetry)
	defer retry.Stop()
	for {
		locked, lockErr := tryLock(f)
		if lockErr != nil {
			return errors.Wrap(lockErr, "lock file")
		}
		if locked {
			break
		}
		retry.Reset(lockRetry)
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "wait for the lock")
		case <-retry.C:
		}
	}
	defer func() { _ = unlock(f) }()

	return fn()
}

// openLock opens the lock file of path, creating it and its directory when missing.
func openLock(path string) (*os.File, error) {
	lockPath := lockPathFor(resolve(path))
	if err := os.MkdirAll(filepath.Dir(lockPath), dirMode); err != nil {
		return nil, errors.Wrap(err, "create lock directory")
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, lockMode)
	if err != nil {
		return nil, errors.Wrap(err, "open lock file")
	}
	return f, nil
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
