package syncbackend_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3" // register the sqlite3 database driver
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/sqlite"
	"github.com/kriuchkov/tock/internal/adapters/syncbackend"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// openSQLite returns the repositories of a new sqlite backend and the path of its database.
func openSQLite(t *testing.T) (*sqlite.ActivityRepository, *sqlite.ReminderRepository, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tock.db")
	activities, err := sqlite.NewSQLiteActivityRepository(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = activities.DB.Close() })
	reminders, err := sqlite.NewReminderRepository(t.Context(), activities.DB)
	require.NoError(t, err)
	return activities, reminders, path
}

func second(at time.Time) ports.SyncSpan {
	return ports.SyncSpan{From: at.UTC(), To: at.UTC().Add(time.Second)}
}

func TestSQLiteChangeLog(t *testing.T) {
	activities, reminders, path := openSQLite(t)
	backend := syncbackend.NewSQLite(activities.DB, path)
	ctx := t.Context()
	zone := time.FixedZone("UTC+3", 3*60*60)
	start := time.Date(2026, time.November, 18, 10, 0, 0, 0, zone)

	empty := token(t, backend)
	assert.Equal(t, ports.SyncToken{"log": "0"}, empty, "the first token makes the log")
	assert.True(t, changed(t, backend, empty, empty).IsEmpty())

	activity := models.Activity{Project: "Backend", Description: "API", StartTime: start}
	require.NoError(t, activities.Save(ctx, activity))
	saved := token(t, backend)
	assert.Equal(t, ports.SyncScope{Activities: []ports.SyncSpan{second(start)}}, changed(t, backend, empty, saved))

	// Moving an activity saves it at its new start and removes the old row: both starts are compared.
	moved := activity
	moved.StartTime = start.Add(2 * time.Hour)
	require.NoError(t, activities.Save(ctx, moved))
	require.NoError(t, activities.Remove(ctx, activity))
	assert.Equal(t, ports.SyncScope{Activities: []ports.SyncSpan{second(start), second(moved.StartTime)}},
		changed(t, backend, saved, token(t, backend)))

	before := token(t, backend)
	_, err := reminders.Save(ctx, models.Reminder{Title: "Standup", Due: start})
	require.NoError(t, err)
	assert.Equal(t, ports.SyncScope{Reminders: true}, changed(t, backend, before, token(t, backend)))

	// Notes live on the activity row, so saving them logs its start.
	before = token(t, backend)
	require.NoError(t, sqlite.NewNotesRepository(activities.DB).Save(ctx, moved.ID(), moved.StartTime, "notes", []string{"api"}))
	assert.Equal(t, ports.SyncScope{Activities: []ports.SyncSpan{second(moved.StartTime)}}, changed(t, backend, before, token(t, backend)))
}

func TestSQLiteChangesMadeOutsideTock(t *testing.T) {
	activities, _, path := openSQLite(t)
	backend := syncbackend.NewSQLite(activities.DB, path)
	ctx := t.Context()
	before := token(t, backend)

	// Rows written by hand with sqlite3, in formats tock does not write itself.
	_, err := activities.DB.ExecContext(ctx, `INSERT INTO activities (description, project, start_time) VALUES
		('a', 'Ops', '2026-11-18T08:30:00Z'), ('b', 'Ops', '2026-11-18 09:15:00.250')`)
	require.NoError(t, err)
	assert.Equal(t, ports.SyncScope{Activities: []ports.SyncSpan{
		second(time.Date(2026, time.November, 18, 8, 30, 0, 0, time.UTC)),
		second(time.Date(2026, time.November, 18, 9, 15, 0, 0, time.UTC)),
	}}, changed(t, backend, before, token(t, backend)))

	before = token(t, backend)
	_, err = activities.DB.ExecContext(ctx, `INSERT INTO activities (description, project, start_time) VALUES ('c', 'Ops', 'yesterday')`)
	require.NoError(t, err)
	assert.Equal(t, ports.SyncScope{AllActivities: true}, changed(t, backend, before, token(t, backend)),
		"a start that does not parse covers every activity")
}

func TestSQLiteTokensItCannotFollow(t *testing.T) {
	activities, _, path := openSQLite(t)
	backend := syncbackend.NewSQLite(activities.DB, path)
	ctx := t.Context()
	require.Equal(t, ports.SyncToken{"log": "0"}, token(t, backend), "the triggers log the changes after the first token")
	for i := range 3 {
		require.NoError(t, activities.Save(ctx, models.Activity{
			Project: "Ops", Description: "x", StartTime: time.Date(2026, time.November, 18, 9, i, 0, 0, time.UTC),
		}))
	}
	now := token(t, backend)
	all := ports.SyncScope{AllActivities: true, Reminders: true}

	assert.Equal(t, all, changed(t, backend, nil, now), "an empty since covers everything")
	assert.Equal(t, all, changed(t, backend, ports.SyncToken{"activities": "1:1"}, now), "a token of another backend")
	assert.Equal(t, all, changed(t, backend, ports.SyncToken{"log": "99"}, now), "the log went back")

	_, err := activities.DB.ExecContext(ctx, `DELETE FROM sync_changes WHERE seq <= 2`)
	require.NoError(t, err)
	assert.Equal(t, all, changed(t, backend, ports.SyncToken{"log": "1"}, now), "changes after since were removed")
	assert.Equal(t, ports.SyncScope{Activities: []ports.SyncSpan{second(time.Date(2026, time.November, 18, 9, 2, 0, 0, time.UTC))}},
		changed(t, backend, ports.SyncToken{"log": "2"}, now), "the changes after since are all kept")
}

func TestSQLitePrunesTheLog(t *testing.T) {
	activities, _, path := openSQLite(t)
	ctx := t.Context()
	require.NotEmpty(t, token(t, syncbackend.NewSQLite(activities.DB, path)))

	_, err := activities.DB.ExecContext(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 20001)
		INSERT INTO sync_changes (kind, start) SELECT 'activity', i FROM n`)
	require.NoError(t, err)

	// The next process prunes the log when it first takes a token.
	backend := syncbackend.NewSQLite(activities.DB, path)
	now := token(t, backend)
	assert.Equal(t, ports.SyncToken{"log": "20001"}, now)
	assert.Equal(t, 10000, countRows(t, activities.DB))

	assert.Equal(t, ports.SyncScope{AllActivities: true, Reminders: true}, changed(t, backend, ports.SyncToken{"log": "5"}, now))
	assert.Equal(t, ports.SyncScope{AllActivities: true}, changed(t, backend, ports.SyncToken{"log": "15000"}, now),
		"more changed starts than a compare reads one by one")
	assert.Equal(t, ports.SyncScope{Activities: []ports.SyncSpan{{From: time.Unix(19999, 0).UTC(), To: time.Unix(20002, 0).UTC()}}},
		changed(t, backend, ports.SyncToken{"log": "19998"}, now), "neighbouring seconds make one span")
}

func TestSQLiteLogIsMadeOnce(t *testing.T) {
	activities, _, path := openSQLite(t)
	ctx := t.Context()
	first := token(t, syncbackend.NewSQLite(activities.DB, path))
	again := token(t, syncbackend.NewSQLite(activities.DB, path))
	assert.Equal(t, first, again)

	require.NoError(t, activities.Save(ctx, models.Activity{Project: "Ops", Description: "x", StartTime: time.Now()}))
	assert.Equal(t, 1, countRows(t, activities.DB), "a second probe adds no second set of triggers")
}

func TestSQLiteWithoutDatabase(t *testing.T) {
	backend := syncbackend.NewSQLite(nil, "tock.db")

	_, err := backend.Token(t.Context())

	require.Error(t, err)
}

func countRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sync_changes`).Scan(&count))
	return count
}

func queryCount(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), query).Scan(&n))
	return n
}

// With sync off the change log goes: no trigger logs the writes, and the next sync compares everything.
func TestSQLiteLogStopsWhileSyncIsOff(t *testing.T) {
	activities, _, path := openSQLite(t)
	ctx := t.Context()
	db := activities.DB
	start := time.Date(2026, time.November, 18, 10, 0, 0, 0, time.UTC)

	// A database that never synced gets nothing.
	require.NoError(t, syncbackend.DisableSQLite(ctx, db))
	assert.Zero(t, queryCount(t, db, `SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'sync_%'`))

	backend := syncbackend.NewSQLite(db, path)
	require.NoError(t, activities.Save(ctx, models.Activity{Project: "Backend", Description: "API", StartTime: start}))
	saved := token(t, backend)

	require.NoError(t, syncbackend.DisableSQLite(ctx, db))
	for i := range 5 {
		require.NoError(
			t,
			activities.Save(ctx, models.Activity{Project: "Off", Description: "x", StartTime: start.Add(time.Duration(i+1) * time.Hour)}),
		)
	}
	assert.Zero(t, queryCount(t, db, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'sync_%'`))
	assert.Equal(t, 1, queryCount(t, db, `SELECT COUNT(*) FROM sync_changes`), "only the gap is left")
	require.NoError(t, syncbackend.DisableSQLite(ctx, db), "again: nothing to do")
	assert.Equal(t, 1, queryCount(t, db, `SELECT COUNT(*) FROM sync_changes`))

	// Sync on again, in a new process: the triggers are made anew and the gap covers everything.
	again := syncbackend.NewSQLite(db, path)
	now := token(t, again)
	assert.Equal(t, ports.SyncScope{AllActivities: true, Reminders: true}, changed(t, again, saved, now))
	later := start.Add(10 * time.Hour)
	require.NoError(t, activities.Save(ctx, models.Activity{Project: "On", Description: "y", StartTime: later}))
	assert.Equal(t, ports.SyncScope{Activities: []ports.SyncSpan{second(later)}}, changed(t, again, now, token(t, again)))
}
