package syncbackend

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/ports"
)

// backupDirMode is the mode of a directory a backup makes.
const backupDirMode = 0o750

// Backup copies the data file, or each timewarrior month file that can hold a start in the scope's
// spans, and the notes directory, when the scope covers activities; reminders.yaml when it covers the
// reminders. The notes directory sits next to reminders.yaml in .tock.
func (f *files) Backup(_ context.Context, scope ports.SyncScope, suffix string) ([]string, error) {
	var paths []string
	if scope.AllActivities || len(scope.Activities) > 0 {
		data, err := f.dataPaths(scope)
		if err != nil {
			return nil, err
		}
		paths = append(paths, data...)
		if f.reminders != "" {
			paths = append(paths, filepath.Join(filepath.Dir(f.reminders), "notes"))
		}
	}
	if scope.Reminders && f.reminders != "" {
		paths = append(paths, f.reminders)
	}
	return backUp(paths, suffix)
}

// dataPaths returns the data file, or the timewarrior month files that can hold a start in scope.
func (f *files) dataPaths(scope ports.SyncScope) ([]string, error) {
	if !f.monthly {
		return []string{f.identity.Path}, nil
	}
	entries, err := os.ReadDir(f.identity.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "list timewarrior data")
	}
	var paths []string
	for _, entry := range entries {
		name, found := strings.CutSuffix(entry.Name(), monthSuffix)
		month, ok := parseMonth(name)
		if !found || !ok || entry.IsDir() {
			continue
		}
		held := monthSpan(month)
		if scope.AllActivities || slices.ContainsFunc(scope.Activities, func(span ports.SyncSpan) bool {
			return span.From.Before(held.To) && held.From.Before(span.To)
		}) {
			paths = append(paths, filepath.Join(f.identity.Path, entry.Name()))
		}
	}
	return paths, nil
}

// Backup copies the database with VACUUM INTO, which reads it in one transaction: a consistent copy
// whatever its journal mode, with what the write-ahead log holds.
func (d *database) Backup(ctx context.Context, scope ports.SyncScope, suffix string) ([]string, error) {
	if scope.IsEmpty() {
		return nil, nil
	}
	if ok, err := exists(d.path); err != nil || !ok {
		return nil, err
	}
	target := d.path + suffix
	if _, err := os.Lstat(target); err == nil {
		return nil, errors.Wrapf(fs.ErrExist, "back up %s", d.path)
	}
	if _, err := d.db.ExecContext(ctx, `VACUUM INTO ?`, target); err != nil {
		return nil, errors.Wrapf(err, "back up %s", d.path)
	}
	return []string{target}, nil
}

// backUp copies each path that exists, a file or a directory, to the path with suffix added, never over
// a file that is there, and returns the copies made.
func backUp(paths []string, suffix string) ([]string, error) {
	var made []string
	for _, path := range paths {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return made, errors.Wrapf(err, "stat %s", path)
		}
		target := path + suffix
		if info.IsDir() {
			err = copyDir(path, target)
		} else {
			err = copyFile(path, target, info.Mode().Perm())
		}
		if err != nil {
			return made, errors.Wrapf(err, "back up %s", path)
		}
		made = append(made, target)
	}
	return made, nil
}

// copyDir copies the directories and regular files under from to to, which must not exist.
func copyDir(from, to string) error {
	return filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		switch {
		case entry.IsDir():
			return os.Mkdir(target, backupDirMode)
		case entry.Type().IsRegular():
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			return copyFile(path, target, info.Mode().Perm())
		default:
			return nil
		}
	})
}

// copyFile copies the file at from to a new file at to, with mode, and syncs it.
func copyFile(from, to string, mode os.FileMode) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err = io.Copy(dst, src); err == nil {
		err = dst.Sync()
	}
	return errors.Join(err, dst.Close())
}
