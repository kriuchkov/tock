package syncstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3" // register the sqlite3 database driver for the raw checks

	"github.com/go-faster/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const (
	devA = "7f3a9c2e5b1d4a60"
	devB = "0123456789abcdef"
)

func statePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "sync", "state.db")
}

// newState returns the state at path, closed when the test ends.
func newState(t *testing.T, path string) ports.SyncState {
	t.Helper()
	st := New(path)
	t.Cleanup(func() {
		impl, ok := st.(*state)
		require.True(t, ok)
		impl.mu.Lock()
		defer impl.mu.Unlock()
		impl.close()
	})
	return st
}

func uid(n int) string {
	return fmt.Sprintf("019a9b3c-7d2e-7c41-9a55-%012x", n)
}

func hlc(wall int64, counter int) journal.HLC {
	return journal.HLC{Wall: wall, Counter: counter}
}

func field(value string, wall int64, dev string) journal.Field {
	return journal.Field{Value: json.RawMessage(value), HLC: hlc(wall, 0), Dev: dev}
}

// activity is an activity record with a start and a project; end is left out when empty.
func activity(id int, start, end string) journal.Record {
	fields := map[string]journal.Field{
		"start":       field(`"`+start+`"`, 1000, devA),
		"project":     field(`"Backend"`, 1000, devA),
		"description": field(`"API"`, 1001, devB),
		"tags/dev":    field(`true`, 1002, devB),
	}
	if end != "" {
		fields["end"] = field(end, 1003, devA)
	}
	return journal.Record{Type: journal.TypeActivity, UID: uid(id), Fields: fields}
}

func reminder(id int) journal.Record {
	return journal.Record{Type: journal.TypeReminder, UID: uid(id), Fields: map[string]journal.Field{
		"title":                 field(`"Standup"`, 2000, devA),
		"due":                   field(`"2026-11-18T10:00"`, 2000, devA),
		"every":                 field(`"weekdays"`, 2000, devA),
		"occ/2026-11-18/done":   field(`"2026-11-18T07:05:00Z"`, 2001, devB),
		"occ/2026-11-19/future": field(`{"kept": [1, 2]}`, 2002, devB),
	}}
}

func fullMeta() ports.SyncMeta {
	return ports.SyncMeta{
		Dir:      "/sync",
		Device:   devA,
		Clock:    hlc(1795025074932, 7),
		Backend:  ports.SyncBackendIdentity{Name: "file", Path: "/data/tock.txt"},
		Token:    ports.SyncToken{"tock.txt": "120:1795025074", "reminders.yaml": "0:0"},
		LastSync: time.Date(2026, 11, 18, 19, 0, 0, 123456789, time.FixedZone("MSK", 3*3600)),
		Skipped:  2,
		// Set only between a rebuild and the next sync, but round-tripped like the rest.
		MatchBackend: true,
	}
}

func TestNewTouchesNothingAndReadsEmpty(t *testing.T) {
	ctx := t.Context()
	path := statePath(t)
	st := newState(t, path)

	meta, err := st.Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, ports.SyncMeta{}, meta)

	cursors, err := st.Cursors(ctx)
	require.NoError(t, err)
	assert.Empty(t, cursors)

	records, err := st.AllRecords(ctx, journal.TypeActivity)
	require.NoError(t, err)
	assert.Empty(t, records)

	records, err = st.Records(ctx, journal.TypeActivity, []string{uid(1)})
	require.NoError(t, err)
	assert.Empty(t, records)

	records, err = st.RunningActivities(ctx)
	require.NoError(t, err)
	assert.Empty(t, records)

	images, err := st.Images(ctx, journal.TypeActivity)
	require.NoError(t, err)
	assert.Empty(t, images)

	images, err = st.ImagesByUID(ctx, journal.TypeActivity, []string{uid(1)})
	require.NoError(t, err)
	assert.Empty(t, images)

	images, err = st.ImagesByKey(ctx, journal.TypeActivity, []string{"2026-11-18T07:00:00Z"})
	require.NoError(t, err)
	assert.Empty(t, images)

	ids, err := st.ReminderIDs(ctx)
	require.NoError(t, err)
	assert.Empty(t, ids)

	_, err = os.Stat(filepath.Dir(path))
	assert.ErrorIs(t, err, os.ErrNotExist, "reading must not create the state directory")
}

func TestSaveRoundTrip(t *testing.T) {
	ctx := t.Context()
	path := statePath(t)
	st := newState(t, path)

	meta := fullMeta()
	running := activity(1, "2026-11-18T10:00:00+03:00", "null")
	stopped := activity(2, "2026-11-18T08:00:00+03:00", `"2026-11-18T09:00:00+03:00"`)
	rem := reminder(3)
	delta := ports.SyncDelta{
		Meta:    &meta,
		Records: []journal.Record{running, stopped, rem},
		Cursors: []ports.SyncCursor{
			{Device: devB, Month: "2026-11", Offset: 512, Size: 600},
			{Device: devA, Month: "2026-10", Offset: 4096, Size: 900, Compressed: true},
		},
		Images: []ports.SyncImage{
			{
				Type: journal.TypeActivity, UID: uid(1), Key: "2026-11-18T07:00:00Z",
				Fields: map[string]json.RawMessage{"start": json.RawMessage(`"2026-11-18T10:00:00+03:00"`), "end": json.RawMessage(`null`)},
			},
			{
				Type: journal.TypeReminder, UID: uid(3), Key: "4",
				Fields: map[string]json.RawMessage{"title": json.RawMessage(`"Standup"`)},
			},
		},
		ReminderIDs: map[string]int{uid(3): 4, uid(9): 1},
	}
	require.NoError(t, st.Save(ctx, delta))

	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(fileMode), info.Mode().Perm())

	// The same state and a second one on the file, as another process, read the same.
	for name, reader := range map[string]ports.SyncState{"same": st, "other": newState(t, path)} {
		t.Run(name, func(t *testing.T) {
			got, err := reader.Meta(ctx)
			require.NoError(t, err)
			assert.Equal(t, meta.Dir, got.Dir)
			assert.Equal(t, meta.Device, got.Device)
			assert.Equal(t, meta.Clock, got.Clock)
			assert.Equal(t, meta.Backend, got.Backend)
			assert.Equal(t, meta.Token, got.Token)
			assert.True(t, meta.LastSync.Equal(got.LastSync), "last sync %v, got %v", meta.LastSync, got.LastSync)
			assert.Equal(t, meta.Skipped, got.Skipped)

			activities, err := reader.AllRecords(ctx, journal.TypeActivity)
			require.NoError(t, err)
			assert.Equal(t, []journal.Record{running, stopped}, activities)

			reminders, err := reader.AllRecords(ctx, journal.TypeReminder)
			require.NoError(t, err)
			assert.Equal(t, []journal.Record{rem}, reminders)

			some, err := reader.Records(ctx, journal.TypeActivity, []string{uid(2), uid(3), uid(7)})
			require.NoError(t, err)
			assert.Equal(t, []journal.Record{stopped}, some, "only activities of the given uids")

			cursors, err := reader.Cursors(ctx)
			require.NoError(t, err)
			assert.Equal(t, delta.Cursors, cursors, "by device, then month")

			images, err := reader.Images(ctx, journal.TypeActivity)
			require.NoError(t, err)
			assert.Equal(t, delta.Images[:1], images)

			images, err = reader.ImagesByUID(ctx, journal.TypeReminder, []string{uid(3)})
			require.NoError(t, err)
			assert.Equal(t, delta.Images[1:], images)

			ids, err := reader.ReminderIDs(ctx)
			require.NoError(t, err)
			assert.Equal(t, delta.ReminderIDs, ids)
		})
	}
}

func TestSaveKeepsValuesByteForByte(t *testing.T) {
	ctx := t.Context()
	st := newState(t, statePath(t))

	odd := map[string]json.RawMessage{
		"description": json.RawMessage(`"a <b> & \"c\" é"`),
		"tags/a/b":    json.RawMessage(`true`),
		"extra":       json.RawMessage(`{ "spaced" : [1,  2] }`),
		"count":       json.RawMessage(`1.50`),
	}
	record := journal.Record{Type: journal.TypeActivity, UID: uid(1), Fields: map[string]journal.Field{}}
	for path, value := range odd {
		record.Fields[path] = journal.Field{Value: value, HLC: hlc(5, 1), Dev: devA}
	}
	image := ports.SyncImage{Type: journal.TypeActivity, UID: uid(1), Key: "k", Fields: odd}
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Records: []journal.Record{record}, Images: []ports.SyncImage{image}}))

	records, err := st.Records(ctx, journal.TypeActivity, []string{uid(1)})
	require.NoError(t, err)
	require.Len(t, records, 1)
	images, err := st.ImagesByKey(ctx, journal.TypeActivity, []string{"k"})
	require.NoError(t, err)
	require.Len(t, images, 1)
	for path, value := range odd {
		assert.Equal(t, string(value), string(records[0].Fields[path].Value), path)
		assert.Equal(t, string(value), string(images[0].Fields[path]), path)
	}
}

func TestSaveReplacesRecordsAndRunning(t *testing.T) {
	ctx := t.Context()
	st := newState(t, statePath(t))

	first := activity(1, "2026-11-18T10:00:00+03:00", "")
	first.Fields["notes"] = field(`"draft"`, 1004, devA)
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Records: []journal.Record{first}}))

	running, err := st.RunningActivities(ctx)
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{first}, running)

	// The record saved again holds only its new fields, and no longer runs.
	second := activity(1, "2026-11-18T10:00:00+03:00", `"2026-11-18T11:00:00+03:00"`)
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Records: []journal.Record{second}}))

	records, err := st.AllRecords(ctx, journal.TypeActivity)
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{second}, records)

	running, err = st.RunningActivities(ctx)
	require.NoError(t, err)
	assert.Empty(t, running)
}

func TestRunningActivities(t *testing.T) {
	ctx := t.Context()
	st := newState(t, statePath(t))

	noEnd := activity(1, "2026-11-18T10:00:00+03:00", "")
	nullEnd := activity(2, "2026-11-18T09:00:00+03:00", "null")
	ended := activity(3, "2026-11-18T08:00:00+03:00", `"2026-11-18T08:30:00+03:00"`)
	deleted := activity(4, "2026-11-18T07:00:00+03:00", "null")
	deleted.Fields["deleted"] = field(`true`, 1010, devB)
	undone := activity(5, "2026-11-18T06:00:00+03:00", "null")
	undone.Fields["deleted"] = field(`false`, 1011, devB)
	badEnd := activity(6, "2026-11-18T05:00:00+03:00", `42`) // an end of the wrong type counts as missing
	noProject := activity(7, "2026-11-18T04:00:00+03:00", "null")
	delete(noProject.Fields, "project")
	rem := reminder(8)

	require.NoError(t, st.Save(ctx, ports.SyncDelta{
		Records: []journal.Record{noEnd, nullEnd, ended, deleted, undone, badEnd, noProject, rem},
	}))

	running, err := st.RunningActivities(ctx)
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{noEnd, nullEnd, undone, badEnd}, running)
}

// The activities an auto_end stops are listed apart from the running ones, by that auto_end.
func TestAutoEndedActivities(t *testing.T) {
	ctx := t.Context()
	st := newState(t, statePath(t))
	autoEnded := func(id int, start, autoEnd string) journal.Record {
		record := activity(id, start, "null")
		record.Fields["auto_end"] = field(`"`+autoEnd+`"`, 1004, devB)
		return record
	}

	runs := activity(1, "2026-11-18T10:00:00Z", "null")
	later := autoEnded(2, "2026-11-18T09:00:00Z", "2026-11-18T10:30:00Z")
	earlier := autoEnded(3, "2026-11-18T08:00:00Z", "2026-11-18T09:00:00Z")
	atTen := autoEnded(4, "2026-11-18T09:30:00Z", "2026-11-18T10:00:00+00:00")
	stale := autoEnded(5, "2026-11-18T10:20:00Z", "2026-11-18T10:15:00Z") // before its start: it runs
	ended := autoEnded(6, "2026-11-18T09:00:00Z", "2026-11-18T11:00:00Z")
	ended.Fields["end"] = field(`"2026-11-18T09:20:00Z"`, 1005, devA)
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Records: []journal.Record{runs, later, earlier, atTen, stale, ended}}))

	running, err := st.RunningActivities(ctx)
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{runs, stale}, running)

	ten := time.Date(2026, time.November, 18, 10, 0, 0, 0, time.UTC)
	got, err := st.AutoEndedActivities(ctx, ten)
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{later}, got, "only those that end after 10:00")
	got, err = st.AutoEndedActivities(ctx, ten.Add(-time.Second))
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{later, atTen}, got)

	// A record saved again with an end leaves the list.
	later.Fields["end"] = field(`"2026-11-18T09:40:00Z"`, 1006, devA)
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Records: []journal.Record{later}}))
	got, err = st.AutoEndedActivities(ctx, ten)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSaveReset(t *testing.T) {
	ctx := t.Context()
	st := newState(t, statePath(t))

	meta := fullMeta()
	require.NoError(t, st.Save(ctx, ports.SyncDelta{
		Meta:        &meta,
		Records:     []journal.Record{activity(1, "2026-11-18T10:00:00+03:00", "null"), reminder(2)},
		Cursors:     []ports.SyncCursor{{Device: devB, Month: "2026-11", Offset: 10, Size: 10}},
		Images:      []ports.SyncImage{{Type: journal.TypeActivity, UID: uid(1), Key: "k", Fields: map[string]json.RawMessage{}}},
		ReminderIDs: map[string]int{uid(2): 1},
	}))

	again := activity(3, "2026-11-18T12:00:00+03:00", "")
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Reset: true, Records: []journal.Record{again}}))

	records, err := st.AllRecords(ctx, journal.TypeActivity)
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{again}, records)
	records, err = st.AllRecords(ctx, journal.TypeReminder)
	require.NoError(t, err)
	assert.Empty(t, records)
	running, err := st.RunningActivities(ctx)
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{again}, running)
	cursors, err := st.Cursors(ctx)
	require.NoError(t, err)
	assert.Empty(t, cursors)

	images, err := st.Images(ctx, journal.TypeActivity)
	require.NoError(t, err)
	assert.Len(t, images, 1, "a reset keeps the images")
	ids, err := st.ReminderIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{uid(2): 1}, ids, "a reset keeps the reminder numbers")
	got, err := st.Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, meta.Device, got.Device, "a reset keeps the meta")
}

func TestImagesByKeyAndDrop(t *testing.T) {
	ctx := t.Context()
	st := newState(t, statePath(t))

	shared := "2026-11-18T07:00:00Z"
	images := []ports.SyncImage{
		{Type: journal.TypeActivity, UID: uid(2), Key: shared, Fields: map[string]json.RawMessage{"project": json.RawMessage(`"B"`)}},
		{Type: journal.TypeActivity, UID: uid(1), Key: shared, Fields: map[string]json.RawMessage{"project": json.RawMessage(`"A"`)}},
		{Type: journal.TypeActivity, UID: uid(3), Key: "2026-11-18T08:00:00Z", Fields: map[string]json.RawMessage{}},
		{Type: journal.TypeReminder, UID: uid(4), Key: shared, Fields: map[string]json.RawMessage{}},
	}
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Images: images}))

	got, err := st.ImagesByKey(ctx, journal.TypeActivity, []string{shared, "missing"})
	require.NoError(t, err)
	assert.Equal(t, []ports.SyncImage{images[1], images[0]}, got, "a key can match several images, by uid")

	require.NoError(t, st.Save(ctx, ports.SyncDelta{
		DropImages: []ports.SyncRecordRef{{Type: journal.TypeActivity, UID: uid(1)}, {Type: journal.TypeActivity, UID: uid(4)}},
	}))
	got, err = st.Images(ctx, journal.TypeActivity)
	require.NoError(t, err)
	assert.Equal(t, []ports.SyncImage{images[0], images[2]}, got)
	got, err = st.Images(ctx, journal.TypeReminder)
	require.NoError(t, err)
	assert.Equal(t, images[3:], got, "a drop names the type too")
}

func TestSaveRollsBackOnError(t *testing.T) {
	ctx := t.Context()
	st := newState(t, statePath(t))

	meta := fullMeta()
	kept := activity(1, "2026-11-18T10:00:00+03:00", "null")
	require.NoError(t, st.Save(ctx, ports.SyncDelta{
		Meta:    &meta,
		Records: []journal.Record{kept},
		Cursors: []ports.SyncCursor{{Device: devB, Month: "2026-11", Offset: 10, Size: 10}},
	}))

	// Everything before the invalid image is written first, and must all be undone.
	newMeta := ports.SyncMeta{Dir: "/other", Device: devB}
	invalid := ports.SyncImage{
		Type: journal.TypeActivity, UID: uid(2), Key: "k",
		Fields: map[string]json.RawMessage{"x": json.RawMessage(`{`)},
	}
	err := st.Save(ctx, ports.SyncDelta{
		Reset:       true,
		Meta:        &newMeta,
		Records:     []journal.Record{activity(2, "2026-11-18T12:00:00+03:00", "")},
		Cursors:     []ports.SyncCursor{{Device: devB, Month: "2026-11", Offset: 99, Size: 99}},
		Images:      []ports.SyncImage{invalid},
		ReminderIDs: map[string]int{uid(5): 5},
	})
	require.Error(t, err)

	got, err := st.Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, meta.Dir, got.Dir)
	assert.Equal(t, meta.Device, got.Device)
	records, err := st.AllRecords(ctx, journal.TypeActivity)
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{kept}, records)
	running, err := st.RunningActivities(ctx)
	require.NoError(t, err)
	assert.Equal(t, []journal.Record{kept}, running)
	cursors, err := st.Cursors(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ports.SyncCursor{{Device: devB, Month: "2026-11", Offset: 10, Size: 10}}, cursors)
	ids, err := st.ReminderIDs(ctx)
	require.NoError(t, err)
	assert.Empty(t, ids)

	// A record without a uid fails the same way.
	require.Error(t, st.Save(ctx, ports.SyncDelta{Records: []journal.Record{{Type: journal.TypeActivity}}}))
}

func TestSaveCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	path := statePath(t)
	st := newState(t, path)

	err := st.Save(ctx, ports.SyncDelta{Records: []journal.Record{activity(1, "2026-11-18T10:00:00+03:00", "")}})
	require.ErrorIs(t, err, context.Canceled)

	records, err := st.AllRecords(t.Context(), journal.TypeActivity)
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestMetaZeroValuesRoundTrip(t *testing.T) {
	ctx := t.Context()
	st := newState(t, statePath(t))

	meta := ports.SyncMeta{Dir: "/sync", Token: ports.SyncToken{}}
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Meta: &meta}))
	got, err := st.Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, meta, got)

	// A later meta replaces every value, zero ones included.
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Meta: &ports.SyncMeta{}}))
	got, err = st.Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, ports.SyncMeta{}, got)
}

func TestDatabaseMode(t *testing.T) {
	ctx := t.Context()
	path := statePath(t)
	st := newState(t, path)
	require.NoError(t, st.Save(ctx, ports.SyncDelta{}))

	db := rawDB(t, path)
	var mode string
	require.NoError(t, db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
	assert.Equal(t, "wal", mode)
	assert.Equal(t, schemaVersion, readUserVersion(t, db))
}

func TestExclusive(t *testing.T) {
	path := statePath(t)
	st := newState(t, path)
	other := newState(t, path)

	var (
		inside  atomic.Int32
		overlap atomic.Bool
		wg      sync.WaitGroup
	)
	for i := range 8 {
		wg.Go(func() {
			target := st
			if i%2 == 1 {
				target = other
			}
			err := target.Exclusive(t.Context(), func(context.Context) error {
				if inside.Add(1) > 1 {
					overlap.Store(true)
				}
				time.Sleep(5 * time.Millisecond)
				inside.Add(-1)
				return nil
			})
			assert.NoError(t, err)
		})
	}
	wg.Wait()
	assert.False(t, overlap.Load(), "two holders of the lock at once")

	failure := errors.New("sync failed")
	require.ErrorIs(t, st.Exclusive(t.Context(), func(context.Context) error { return failure }), failure)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	err := st.Exclusive(ctx, func(context.Context) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, called)
}

func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func readUserVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var version int
	require.NoError(t, db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version))
	return version
}

func TestSaveUnderExclusiveCreatesTheFile(t *testing.T) {
	ctx := t.Context()
	path := statePath(t)
	st := newState(t, path)

	meta := ports.SyncMeta{Dir: "/sync", Device: devA}
	require.NoError(t, st.Exclusive(ctx, func(ctx context.Context) error {
		return st.Save(ctx, ports.SyncDelta{Meta: &meta})
	}))
	got, err := st.Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, meta, got)
}

func TestPathWithURICharacters(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "odd dir?x=1#frag%41", "state.db")
	st := newState(t, path)

	meta := ports.SyncMeta{Dir: "/sync"}
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Meta: &meta}))
	_, err := os.Stat(path)
	require.NoError(t, err, "the database is at the path as given")

	got, err := newState(t, path).Meta(ctx)
	require.NoError(t, err)
	assert.Equal(t, meta, got)
}

func TestSaveDuplicateRecordsKeepsTheLast(t *testing.T) {
	ctx := t.Context()
	st := newState(t, statePath(t))

	first := activity(1, "2026-11-18T10:00:00+03:00", "")
	last := activity(1, "2026-11-18T10:00:00+03:00", `"2026-11-18T11:00:00+03:00"`)
	delete(last.Fields, "tags/dev")
	for _, reset := range []bool{true, false} {
		require.NoError(t, st.Save(ctx, ports.SyncDelta{Reset: reset, Records: []journal.Record{first, last}}))

		records, err := st.AllRecords(ctx, journal.TypeActivity)
		require.NoError(t, err)
		assert.Equal(t, []journal.Record{last}, records)
		running, err := st.RunningActivities(ctx)
		require.NoError(t, err)
		assert.Empty(t, running)
	}
}

func TestSaveManyRecordsInBatches(t *testing.T) {
	ctx := t.Context()
	st := newState(t, statePath(t))

	// More rows than one batch holds, and a remainder.
	records := make([]journal.Record, 0, 333)
	for i := range 333 {
		records = append(records, activity(i, "2026-11-18T10:00:00+03:00", ""))
	}
	require.NoError(t, st.Save(ctx, ports.SyncDelta{Records: records}))

	got, err := st.AllRecords(ctx, journal.TypeActivity)
	require.NoError(t, err)
	assert.Equal(t, records, got)
	running, err := st.RunningActivities(ctx)
	require.NoError(t, err)
	assert.Len(t, running, len(records))
}

// The auto-stop marks outlive the process that wrote them, and a meta without them clears them.
func TestMetaKeepsAutoStops(t *testing.T) {
	ctx := t.Context()
	path := statePath(t)
	meta := fullMeta()
	start := time.Date(2026, 11, 18, 9, 0, 20, 0, time.UTC)
	meta.AutoStops = []ports.SyncAutoStop{{Start: start, End: start.Add(25 * time.Second), Project: "Work", Description: "a | b"}}
	require.NoError(t, newState(t, path).Save(ctx, ports.SyncDelta{Meta: &meta}))

	got, err := newState(t, path).Meta(ctx)
	require.NoError(t, err)
	require.Len(t, got.AutoStops, 1)
	assert.True(t, got.AutoStops[0].Start.Equal(start))
	assert.True(t, got.AutoStops[0].End.Equal(start.Add(25*time.Second)))
	assert.Equal(t, "a | b", got.AutoStops[0].Description)

	got.AutoStops = nil
	require.NoError(t, newState(t, path).Save(ctx, ports.SyncDelta{Meta: &got}))
	got, err = newState(t, path).Meta(ctx)
	require.NoError(t, err)
	assert.Empty(t, got.AutoStops)
}

// Exclusive waits for a lock another process holds no longer than its context allows.
func TestExclusiveStopsWaitingWithItsContext(t *testing.T) {
	path := statePath(t)
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	holder := newState(t, path)
	go func() {
		done <- holder.Exclusive(context.Background(), func(context.Context) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	ran := false
	err := newState(t, path).Exclusive(ctx, func(context.Context) error {
		ran = true
		return nil
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.False(t, ran)

	close(release)
	require.NoError(t, <-done)
	require.NoError(t, newState(t, path).Exclusive(t.Context(), func(context.Context) error { return nil }))
}
