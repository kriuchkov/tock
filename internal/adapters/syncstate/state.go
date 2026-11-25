// Package syncstate keeps the local sync state of docs/sync.md in SQLite: state.db in ~/.tock/sync/
// on macOS, outside the sync folder.
//
// It holds the merged state of the journals as one row per field (type, uid, path, value, clock,
// device), deleted records included, with the activities that may be running listed apart; a cursor
// per segment; the last-sync image of the backend per record with its backend key (the uid map for
// backends without uid storage); the local reminder numbers; and the meta: the sync folder, this
// device's ID, its last clock, the backend's identity and change token, the time of the last sync and
// the count of skipped lines. Save applies a delta in one transaction. Exclusive is a flock on a
// sibling lock file (fileio.Update), so the CLI, the menu bar icon and the MCP server never sync at
// the same time.
//
// The database runs in WAL mode with synchronous FULL, so a save that returned survives a crash: the
// clock it holds must never fall behind this device's own journal, or the next sync would take the
// device for a clone. The layout has a version (PRAGMA user_version). A database of an older
// version is rebuilt in place: every table is made again, keeping only the sync folder, the device ID
// and its clock, so sync stays set up and the next run reads the journals again and matches the
// backend to them as after a backend switch. A database of a newer version is left alone and every
// method fails: a newer tock made it, and rebuilding it would make that tock rebuild it in turn. A
// file that is not a database is removed and made again.
//
// The database is opened on first use, so building the state creates nothing on disk, and reading
// creates nothing either: before the first save every method reads as empty. It uses the sqlite3
// driver of github.com/mattn/go-sqlite3 directly, not through database/sql's driver registry.
package syncstate

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/adapters/repositories/fileio"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const (
	// dirMode and fileMode keep the state private: it holds every activity.
	dirMode  = 0o700
	fileMode = 0o600
)

// state is state.db at path.
type state struct {
	path string

	// mu guards db and file and serializes the methods of this value; Exclusive does not take it.
	mu sync.Mutex
	// db is the open database, nil until first use; file is the file it was opened on.
	db   *sql.DB
	file os.FileInfo
}

var _ ports.SyncState = (*state)(nil)

// New returns the sync state kept in the SQLite database at path. Nothing is opened or created until
// a method is called, and only Save and Exclusive create files.
func New(path string) ports.SyncState {
	return &state{path: path}
}

// Exclusive runs fn while holding a flock on .state.db.lock next to the database, waiting for it no
// longer than ctx allows. The lock belongs to the open file, so it also keeps two goroutines of one
// process apart; a nested call waits until ctx is done.
func (s *state) Exclusive(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "lock sync state")
	}
	return fileio.UpdateContext(ctx, s.path, func() error {
		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, "lock sync state")
		}
		return fn(ctx)
	})
}

// read runs fn on the database, which it opens, or rebuilds when its layout is another one. While
// there is no database file fn does not run: the caller's zero result stands. fn gets ctx without its
// cancellation for the calls into SQLite (see quiet); a canceled ctx stops read before it starts.
func (s *state) read(ctx context.Context, fn func(dbCtx context.Context, db *sql.DB) error) error {
	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "read sync state")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	db, ok, err := s.database(ctx, false)
	if err != nil || !ok {
		return err
	}
	return fn(quiet(ctx), db)
}

// write runs fn in a transaction on the database, creating the file when it is missing. fn gets ctx
// without its cancellation for the calls into SQLite; canceling ctx rolls the transaction back, after
// which fn's statements and the commit fail.
func (s *state) write(ctx context.Context, fn func(dbCtx context.Context, tx *sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	db, _, err := s.database(ctx, true)
	if err != nil {
		return err
	}
	// The DSN makes this BEGIN IMMEDIATE: the write lock is taken at once, not on the first write.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin sync state transaction")
	}
	defer func() { _ = tx.Rollback() }()

	if err = fn(quiet(ctx), tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "commit sync state")
	}
	return nil
}

// database returns the open database with the current layout. A database opened on a file removed
// or replaced since is opened again, so a long-running process follows another one that made the file
// anew. ok is false when there is no file and create is false.
func (s *state) database(ctx context.Context, create bool) (*sql.DB, bool, error) {
	for attempt := 0; ; attempt++ {
		info, err := os.Stat(s.path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			s.close()
			if !create {
				return nil, false, nil
			}
			if info, err = createFile(s.path); err != nil {
				return nil, false, err
			}
		case err != nil:
			return nil, false, errors.Wrap(err, "stat sync state")
		}

		if s.db == nil || !os.SameFile(info, s.file) {
			s.close()
			s.db, s.file = openDB(s.path), info
		}

		err = ensureSchema(ctx, s.db)
		if err == nil {
			return s.db, true, nil
		}
		if !isDamaged(err) || attempt > 0 {
			return nil, false, err
		}
		// Not a database, or a damaged one: the state is a cache of the journals, so start over.
		damaged := s.file
		s.close()
		if err = removeDamaged(s.path, damaged); err != nil {
			return nil, false, err
		}
	}
}

// quiet returns ctx without its cancellation, for the calls into SQLite: go-sqlite3 starts a goroutine
// to watch every statement it runs under a context that can be canceled, which makes saving 100,000
// records several times slower. The transaction still watches ctx.
func quiet(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}

// close closes the open database, if any.
func (s *state) close() {
	if s.db != nil {
		_ = s.db.Close()
		s.db, s.file = nil, nil
	}
}

// createFile makes an empty database file with private permissions, which SQLite gives its WAL files
// too, unless another process made it first. Logs left without their database are removed first, so
// they are not taken for its logs. It runs under a lock of its own, so two processes that find no
// database never remove the logs of the one the other just made; Exclusive's lock cannot serve, as
// Save runs under it.
func createFile(path string) (os.FileInfo, error) {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return nil, errors.Wrap(err, "create sync state directory")
	}
	err := fileio.Update(path+".create", func() error {
		switch _, err := os.Stat(path); {
		case err == nil:
			return nil
		case !errors.Is(err, fs.ErrNotExist):
			return errors.Wrap(err, "stat sync state")
		}
		if err := removeLogs(path); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
		if err != nil {
			return errors.Wrap(err, "create sync state")
		}
		if err = f.Close(); err != nil {
			return errors.Wrap(err, "create sync state")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, errors.Wrap(err, "stat sync state")
	}
	return info, nil
}

// removeDamaged removes the database and its logs, under createFile's lock, while the file at path is
// still the damaged one: another process may have made a new one already.
func removeDamaged(path string, damaged os.FileInfo) error {
	return fileio.Update(path+".create", func() error {
		info, err := os.Stat(path)
		if err != nil || !os.SameFile(info, damaged) {
			return nil //nolint:nilerr // a missing or replaced file is not ours to remove
		}
		if err = os.Remove(path); err != nil {
			return errors.Wrap(err, "remove damaged sync state")
		}
		return removeLogs(path)
	})
}

func removeLogs(path string) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return errors.Wrap(err, "remove sync state log")
		}
	}
	return nil
}
