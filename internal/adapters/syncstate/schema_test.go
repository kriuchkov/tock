package syncstate

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// writeOldLayout makes a database of another layout version at path, with a meta table as every
// layout keeps it.
func writeOldLayout(t *testing.T, path string, version int, meta map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), dirMode))
	db := rawDB(t, path)
	ctx := t.Context()
	_, err := db.ExecContext(ctx, `
		CREATE TABLE meta (name TEXT PRIMARY KEY NOT NULL, value TEXT NOT NULL);
		CREATE TABLE fields (type TEXT, uid TEXT, blob BLOB);
		CREATE TABLE old_stuff (x INTEGER);
		CREATE INDEX old_index ON old_stuff (x);
		CREATE VIEW old_view AS SELECT x FROM old_stuff;
		INSERT INTO fields VALUES ('activity', 'u', x'00');
	`)
	require.NoError(t, err)
	for name, value := range meta {
		_, err = db.ExecContext(ctx, "INSERT INTO meta (name, value) VALUES (?, ?)", name, value)
		require.NoError(t, err)
	}
	_, err = db.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(version))
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func TestSchemaMismatchRebuildsKeepingIdentity(t *testing.T) {
	ctx := t.Context()
	path := statePath(t)
	writeOldLayout(t, path, schemaVersion-1, map[string]string{
		metaDir:         "/sync",
		metaDevice:      devA,
		metaClock:       "0001795025074932.000003",
		metaBackendName: "file",
		metaBackendPath: "/data/tock.txt",
		metaToken:       `{"tock.txt":"1:2"}`,
		metaLastSync:    "2026-11-18T19:00:00Z",
		metaSkipped:     "4",
		"unknown":       "x",
	})

	st := newState(t, path)
	meta, err := st.Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, ports.SyncMeta{
		Dir: "/sync", Device: devA, Clock: hlc(1795025074932, 3),
		Backend:      ports.SyncBackendIdentity{Name: "file", Path: "/data/tock.txt"},
		MatchBackend: true,
	}, meta, "the folder, the device and its clock outlive a rebuild, and so does the backend, to be matched again; "+
		"its token and its images do not")

	records, err := st.AllRecords(ctx, journal.TypeActivity)
	require.NoError(t, err)
	assert.Empty(t, records)

	db := rawDB(t, path)
	assert.Equal(t, schemaVersion, readUserVersion(t, db))
	var left int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_schema WHERE name IN ('old_stuff', 'old_index', 'old_view')").Scan(&left))
	assert.Zero(t, left, "the old tables go")

	// The rebuilt database works as a new one.
	record := activity(1, "2026-11-18T10:00:00+03:00", "")
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Records: []journal.Record{record}}))
	records, err = newState(t, path).AllRecords(ctx, journal.TypeActivity)
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{record}, records)
}

func TestSchemaMismatchDropsDeviceWithoutItsClock(t *testing.T) {
	path := statePath(t)
	writeOldLayout(t, path, schemaVersion-1, map[string]string{metaDir: "/sync", metaDevice: devA, metaClock: "garbage"})

	meta, err := newState(t, path).Meta(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ports.SyncMeta{Dir: "/sync"}, meta)
}

// A database a newer tock made is left alone: rebuilding it would drop what that tock keeps.
func TestSchemaNewerIsLeftAlone(t *testing.T) {
	ctx := t.Context()
	path := statePath(t)
	writeOldLayout(t, path, schemaVersion+1, map[string]string{metaDir: "/sync", metaDevice: devA})

	st := newState(t, path)
	_, err := st.Meta(ctx)
	require.ErrorIs(t, err, errNewerState)
	_, err = st.AllRecords(ctx, journal.TypeActivity)
	require.ErrorIs(t, err, errNewerState)
	require.ErrorIs(t, st.Save(ctx, ports.SyncDelta{Meta: &ports.SyncMeta{Dir: "/other"}}), errNewerState)

	db := rawDB(t, path)
	assert.Equal(t, schemaVersion+1, readUserVersion(t, db))
	var left int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_schema WHERE name IN ('old_stuff', 'old_index', 'old_view')").Scan(&left))
	assert.Equal(t, 3, left, "nothing is dropped")
}

// The starts table lists every activity record by its merged start, for ActivitiesStartingIn.
func TestActivitiesStartingIn(t *testing.T) {
	ctx := t.Context()
	st := newState(t, statePath(t))
	early := activity(1, "2026-11-18T10:00:00+03:00", "")
	late := activity(2, "2026-11-18T10:00:59+03:00", "")
	other := activity(3, "2026-11-18T10:01:00+03:00", "")
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Records: []journal.Record{early, late, other}}))

	from := time.Date(2026, 11, 18, 7, 0, 0, 0, time.UTC)
	got, err := st.ActivitiesStartingIn(ctx, ports.SyncSpan{From: from, To: from.Add(time.Minute)})
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{early, late}, got)

	// A start moved by a later save moves its row.
	moved := activity(1, "2026-11-18T12:00:00+03:00", "")
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Records: []journal.Record{moved}}))
	got, err = st.ActivitiesStartingIn(ctx, ports.SyncSpan{From: from, To: from.Add(time.Minute)})
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{late}, got)
	got, err = st.ActivitiesStartingIn(ctx, ports.SyncSpan{From: from.Add(2 * time.Hour)})
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{moved}, got)
}

func TestSchemaForeignDatabase(t *testing.T) {
	ctx := t.Context()
	path := statePath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), dirMode))
	db := rawDB(t, path)
	_, err := db.ExecContext(ctx, "CREATE TABLE activities (id INTEGER PRIMARY KEY, project TEXT)")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	st := newState(t, path)
	meta, err := st.Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, ports.SyncMeta{}, meta)
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Meta: &ports.SyncMeta{Dir: "/sync"}}))
}

func TestSchemaChangedByAnotherProcess(t *testing.T) {
	ctx := t.Context()
	path := statePath(t)
	st := newState(t, path)
	meta := fullMeta()
	require.NoError(t, st.Save(ctx, ports.SyncDelta{
		Meta:    &meta,
		Records: []journal.Record{activity(1, "2026-11-18T10:00:00+03:00", "")},
	}))

	// An older tock rebuilds the layout to its own while this one keeps its connection open.
	db := rawDB(t, path)
	_, err := db.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(schemaVersion-1))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	got, err := st.Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, ports.SyncMeta{Dir: meta.Dir, Device: meta.Device, Clock: meta.Clock, Backend: meta.Backend, MatchBackend: true}, got)
	records, err := st.RunningActivities(ctx)
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestDamagedFileStartsOver(t *testing.T) {
	ctx := t.Context()
	path := statePath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), dirMode))
	require.NoError(t, os.WriteFile(path, []byte("this is not a database, not even close to one........"), fileMode))

	st := newState(t, path)
	meta, err := st.Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, ports.SyncMeta{}, meta)

	saved := fullMeta()
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Meta: &saved}))
	meta, err = newState(t, path).Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, saved.Device, meta.Device)
}

func TestFileRemovedWhileOpen(t *testing.T) {
	ctx := t.Context()
	path := statePath(t)
	st := newState(t, path)
	meta := fullMeta()
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Meta: &meta}))

	for _, file := range []string{path, path + "-wal", path + "-shm"} {
		require.NoError(t, os.Remove(file))
	}
	got, err := st.Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, ports.SyncMeta{}, got, "a removed file reads as empty")

	again := ports.SyncMeta{Dir: "/again"}
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Meta: &again}))
	got, err = newState(t, path).Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, again, got, "the save goes to the new file")
}
