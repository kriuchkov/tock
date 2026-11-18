package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

func TestActivityUIDs(t *testing.T) {
	ctx := context.Background()
	repo := setupTestDB(t)
	defer repo.DB.Close()

	start := time.Date(2026, time.November, 18, 9, 0, 0, 0, time.Local)
	require.NoError(t, repo.Save(ctx, models.Activity{Project: "Ops", Description: "Deploy", StartTime: start}))
	first, err := repo.FindLast(ctx)
	require.NoError(t, err)
	assertUID(t, first.UID)

	require.NoError(t, repo.Save(ctx, models.Activity{Project: "Ops", Description: "Deploy v2", StartTime: start}))
	edited, err := repo.FindLast(ctx)
	require.NoError(t, err)
	assert.Equal(t, first.UID, edited.UID, "saving without a uid keeps the stored one")

	// Moving an activity saves it under its new start, then removes the old row.
	moved := *edited
	moved.StartTime = start.Add(time.Hour)
	require.NoError(t, repo.Save(ctx, moved))
	require.NoError(t, repo.Remove(ctx, *edited))
	all, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, first.UID, all[0].UID, "a moved activity keeps its uid")

	moved.UID = "019a9b3c-0000-7000-8000-0000000000a1"
	require.NoError(t, repo.Save(ctx, moved))
	named, err := repo.FindLast(ctx)
	require.NoError(t, err)
	assert.Equal(t, moved.UID, named.UID, "a uid the activity names replaces the stored one")

	require.NoError(t, repo.Save(ctx, models.Activity{Project: "Ops", Description: "Next", StartTime: start.Add(2 * time.Hour)}))
	next, err := repo.FindLast(ctx)
	require.NoError(t, err)
	assertUID(t, next.UID)
	assert.NotEqual(t, moved.UID, next.UID)
}

func TestReminderUIDs(t *testing.T) {
	ctx := context.Background()
	activities := setupTestDB(t)
	defer activities.DB.Close()
	repo, err := NewReminderRepository(ctx, activities.DB)
	require.NoError(t, err)

	due := time.Date(2026, time.November, 20, 10, 0, 0, 0, time.Local)
	saved, err := repo.Save(ctx, models.Reminder{Title: "Send invoice", Due: due})
	require.NoError(t, err)
	assertUID(t, saved.UID)

	edited := *saved
	edited.UID = ""
	edited.Title = "Send the invoice"
	again, err := repo.Save(ctx, edited)
	require.NoError(t, err)
	assert.Equal(t, saved.UID, again.UID, "saving without a uid keeps the stored one")

	edited.UID = "019a9b3c-0000-7000-8000-0000000000b1"
	again, err = repo.Save(ctx, edited)
	require.NoError(t, err)
	assert.Equal(t, edited.UID, again.UID, "a uid the reminder names replaces the stored one")

	other, err := repo.Save(ctx, models.Reminder{Title: "Other", Due: due})
	require.NoError(t, err)
	assertUID(t, other.UID)

	listed, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, edited.UID, listed[0].UID)
	assert.Equal(t, other.UID, listed[1].UID)
}

// TestUIDMigration opens a database made before uids: every row gets a uid once, and keeps it.
func TestUIDMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tock.db")

	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
	CREATE TABLE activities (
		id INTEGER PRIMARY KEY AUTOINCREMENT, description TEXT NOT NULL, project TEXT NOT NULL,
		start_time DATETIME NOT NULL UNIQUE, end_time DATETIME, notes TEXT, tags TEXT
	);
	CREATE TABLE reminders (
		id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, project TEXT NOT NULL DEFAULT '',
		due TEXT NOT NULL, all_day INTEGER NOT NULL DEFAULT 0, every TEXT NOT NULL DEFAULT '',
		"until" TEXT NOT NULL DEFAULT '', "count" INTEGER NOT NULL DEFAULT 0, notify TEXT NOT NULL DEFAULT '',
		tags TEXT NOT NULL DEFAULT '[]', notes TEXT NOT NULL DEFAULT '', created TEXT NOT NULL DEFAULT '',
		occurrences TEXT NOT NULL DEFAULT '{}'
	);
	INSERT INTO activities (description, project, start_time, end_time) VALUES
		('Deploy', 'Ops', '2026-11-18 08:00:00+00:00', '2026-11-18 09:00:00+00:00'),
		('Review', 'Ops', '2026-11-18 10:00:00+00:00', NULL);
	INSERT INTO reminders (title, due) VALUES ('First', '2026-11-16 09:00'), ('Second', '2026-11-17 09:00');
	`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	open := func() ([]string, []string) {
		repo, openErr := NewSQLiteActivityRepository(ctx, path)
		require.NoError(t, openErr)
		defer repo.DB.Close()
		reminders, openErr := NewReminderRepository(ctx, repo.DB)
		require.NoError(t, openErr)

		activities, findErr := repo.Find(ctx, models.ActivityFilter{})
		require.NoError(t, findErr)
		listed, listErr := reminders.List(ctx)
		require.NoError(t, listErr)

		var activityUIDs, reminderUIDs []string
		for _, activity := range activities {
			assertUID(t, activity.UID)
			activityUIDs = append(activityUIDs, activity.UID)
		}
		for _, reminder := range listed {
			assertUID(t, reminder.UID)
			reminderUIDs = append(reminderUIDs, reminder.UID)
		}
		return activityUIDs, reminderUIDs
	}

	activityUIDs, reminderUIDs := open()
	require.Len(t, activityUIDs, 2)
	require.Len(t, reminderUIDs, 2)
	assert.NotEqual(t, activityUIDs[0], activityUIDs[1])
	assert.NotEqual(t, reminderUIDs[0], reminderUIDs[1])

	activityAgain, reminderAgain := open()
	assert.Equal(t, activityUIDs, activityAgain, "opening again keeps the uids")
	assert.Equal(t, reminderUIDs, reminderAgain)
}

// TestSaveFillsAnEmptyUID covers rows an older tock wrote after the migration, with an empty uid.
func TestSaveFillsAnEmptyUID(t *testing.T) {
	ctx := context.Background()
	repo := setupTestDB(t)
	defer repo.DB.Close()
	reminders, err := NewReminderRepository(ctx, repo.DB)
	require.NoError(t, err)

	_, err = repo.DB.ExecContext(ctx, `
	INSERT INTO activities (description, project, start_time) VALUES ('Deploy', 'Ops', '2026-11-18 08:00:00+00:00');
	INSERT INTO reminders (title, due) VALUES ('First', '2026-11-16 09:00');
	`)
	require.NoError(t, err)

	activity, err := repo.FindLast(ctx)
	require.NoError(t, err)
	require.Empty(t, activity.UID)
	require.NoError(t, repo.Save(ctx, *activity))
	activity, err = repo.FindLast(ctx)
	require.NoError(t, err)
	assertUID(t, activity.UID)

	listed, err := reminders.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Empty(t, listed[0].UID)
	saved, err := reminders.Save(ctx, listed[0])
	require.NoError(t, err)
	assertUID(t, saved.UID)
	listed, err = reminders.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, saved.UID, listed[0].UID)
}

func assertUID(t *testing.T, uid string) {
	t.Helper()
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, uid)
}
