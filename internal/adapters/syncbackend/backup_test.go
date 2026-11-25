package syncbackend_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/sqlite"
	"github.com/kriuchkov/tock/internal/adapters/syncbackend"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const suffix = ".before-restore-20261125-101500"

// writeFiles writes each file of files, by path relative to dir.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
}

// readFile returns the content of the file at path.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// The file backend copies its data file and its notes when activities are rewritten, reminders.yaml
// when reminders are, and never replaces a copy.
func TestFileBackendBackup(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"tock.txt":                          "2026-11-18 09:00 - 2026-11-18 09:30 | P | d\n",
		".tock/notes/2026-11-18/a1b2c3.txt": "a note",
		".tock/reminders.yaml":              "reminders: []\n",
	})
	backend := syncbackend.NewFile(filepath.Join(dir, "tock.txt"), filepath.Join(dir, ".tock", "reminders.yaml"))
	scope := ports.SyncScope{Activities: []ports.SyncSpan{second(time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC))}}

	made, err := backend.Backup(t.Context(), scope, suffix)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, "tock.txt"+suffix), filepath.Join(dir, ".tock", "notes"+suffix)}, made)
	assert.Equal(t, "2026-11-18 09:00 - 2026-11-18 09:30 | P | d\n", readFile(t, made[0]))
	assert.Equal(t, "a note", readFile(t, filepath.Join(made[1], "2026-11-18", "a1b2c3.txt")))

	_, err = backend.Backup(t.Context(), scope, suffix)
	require.ErrorIs(t, err, fs.ErrExist)

	made, err = backend.Backup(t.Context(), ports.SyncScope{Reminders: true}, suffix+"1")
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, ".tock", "reminders.yaml"+suffix+"1")}, made)
}

// timewarrior copies the month files that can hold a start in the scope, a day past each end of their
// UTC month, and a copy is no month file: it keeps no .data suffix.
func TestTimewarriorBackupCopiesTheMonthsInScope(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	writeFiles(t, data, map[string]string{"2026-10.data": "oct", "2026-11.data": "nov", "2026-12.data": "dec"})
	backend := syncbackend.NewTimewarrior(data, filepath.Join(dir, ".tock", "reminders.yaml"))
	scope := ports.SyncScope{Activities: []ports.SyncSpan{second(time.Date(2026, 11, 30, 23, 0, 0, 0, time.UTC))}}

	made, err := backend.Backup(t.Context(), scope, suffix)
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(data, "2026-11.data"+suffix), filepath.Join(data, "2026-12.data"+suffix)}, made)
	assert.Equal(t, "dec", readFile(t, made[1]))
	token, err := backend.Token(t.Context())
	require.NoError(t, err)
	assert.Len(t, token, 4, "three months and reminders.yaml")
}

// A sqlite backup is a database of its own that reads back what the backend held, the rows still in
// the write-ahead log of a database in WAL mode included.
func TestSQLiteBackupIsAReadableDatabase(t *testing.T) {
	activities, _, path := openSQLite(t)
	ctx := t.Context()
	activities.DB.SetMaxOpenConns(1)
	_, err := activities.DB.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0`)
	require.NoError(t, err)
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	require.NoError(t, activities.Save(ctx, models.Activity{Project: "P", Description: "d", StartTime: start, Notes: "n"}))
	require.FileExists(t, path+"-wal")
	backend := syncbackend.NewSQLite(activities.DB, path)

	made, err := backend.Backup(ctx, ports.SyncScope{}, suffix)
	require.NoError(t, err)
	assert.Empty(t, made, "nothing to rewrite")
	made, err = backend.Backup(ctx, ports.SyncScope{AllActivities: true}, suffix)
	require.NoError(t, err)
	require.Equal(t, []string{path + suffix}, made)

	copied, err := sqlite.NewSQLiteActivityRepository(ctx, made[0])
	require.NoError(t, err)
	t.Cleanup(func() { _ = copied.DB.Close() })
	held, err := copied.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, held, 1)
	assert.Equal(t, "P", held[0].Project)
	assert.True(t, held[0].StartTime.Equal(start))

	_, err = backend.Backup(ctx, ports.SyncScope{AllActivities: true}, suffix)
	require.ErrorIs(t, err, fs.ErrExist)
}
