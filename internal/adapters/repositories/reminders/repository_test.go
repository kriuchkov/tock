package reminders_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/reminders"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
)

func sampleReminder() models.Reminder {
	until := time.Date(2026, time.December, 31, 0, 0, 0, 0, time.Local)
	doneAt := time.Date(2026, time.November, 16, 17, 35, 0, 0, time.Local)
	return models.Reminder{
		Title:   "Team sync",
		Project: "Work",
		Due:     time.Date(2026, time.November, 16, 17, 30, 0, 0, time.Local),
		Repeat: models.Repeat{
			Freq: models.RepeatWeekly, Interval: 2, Weekdays: []time.Weekday{time.Monday, time.Thursday},
			Until: &until, Count: 8,
		},
		Notify:  10 * time.Minute,
		Tags:    []string{"team", "weekly"},
		Notes:   "bring the agenda",
		Created: time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC),
		States: map[string]models.OccurrenceState{
			"2026-11-16": {DoneAt: &doneAt, ActivityStart: &doneAt},
			"2026-11-19": {Skipped: true},
		},
	}
}

func TestRepositoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), ".tock", "reminders.yaml")
	repo := reminders.NewRepository(path)

	empty, err := repo.List(ctx)
	require.NoError(t, err, "a missing file is an empty list")
	assert.Empty(t, empty)

	saved, err := repo.Save(ctx, sampleReminder())
	require.NoError(t, err)
	assert.Equal(t, 1, saved.ID)

	allDay, err := repo.Save(
		ctx,
		models.Reminder{Title: "Renew domain", Due: time.Date(2026, time.November, 16, 0, 0, 0, 0, time.Local), AllDay: true},
	)
	require.NoError(t, err)
	assert.Equal(t, 2, allDay.ID)

	listed, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assertSameReminder(t, *saved, listed[0])
	assert.True(t, listed[1].AllDay)
	assert.Equal(t, allDay.Due, listed[1].Due)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "due: 2026-11-16 17:30", "due times are kept as wall-clock time")
	assert.Contains(t, string(raw), "every: 2w:mon,thu")
}

func TestRepositoryUpdatesInPlaceAndNeverReusesIDs(t *testing.T) {
	ctx := context.Background()
	repo := reminders.NewRepository(filepath.Join(t.TempDir(), "reminders.yaml"))

	first, err := repo.Save(ctx, sampleReminder())
	require.NoError(t, err)
	second, err := repo.Save(ctx, sampleReminder())
	require.NoError(t, err)

	first.Title = "Renamed"
	_, err = repo.Save(ctx, *first)
	require.NoError(t, err)

	require.NoError(t, repo.Remove(ctx, second.ID))
	require.ErrorIs(t, repo.Remove(ctx, second.ID), coreErrors.ErrReminderNotFound)

	third, err := repo.Save(ctx, sampleReminder())
	require.NoError(t, err)
	assert.Equal(t, 3, third.ID)

	listed, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, "Renamed", listed[0].Title)
	assert.Equal(t, 3, listed[1].ID)
}

func TestRepositoryReportsACorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reminders.yaml")
	require.NoError(t, os.WriteFile(path, []byte("reminders: [{id: 1, title: x, due: tomorrow}]\n"), 0o600))

	_, err := reminders.NewRepository(path).List(context.Background())
	assert.Error(t, err)
}

func TestRepositoryKeepsUIDs(t *testing.T) {
	ctx := context.Background()
	repo := reminders.NewRepository(filepath.Join(t.TempDir(), "reminders.yaml"))

	saved, err := repo.Save(ctx, sampleReminder())
	require.NoError(t, err)
	assertUID(t, saved.UID)

	edited := *saved
	edited.UID = ""
	edited.Title = "Renamed"
	again, err := repo.Save(ctx, edited)
	require.NoError(t, err)
	assert.Equal(t, saved.UID, again.UID, "saving without a uid keeps the stored one")

	edited.UID = "019a9b3c-0000-7000-8000-0000000000b1"
	again, err = repo.Save(ctx, edited)
	require.NoError(t, err)
	assert.Equal(t, edited.UID, again.UID, "a uid the reminder names replaces the stored one")

	listed, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, edited.UID, listed[0].UID)
}

func TestRepositoryGivesOldRemindersUIDsOnWrite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reminders.yaml")
	old := "next_id: 3\nreminders:\n" +
		"  - id: 1\n    title: First\n    due: 2026-11-16 09:00\n" +
		"  - id: 2\n    title: Second\n    due: 2026-11-17 09:00\n"
	require.NoError(t, os.WriteFile(path, []byte(old), 0o600))
	repo := reminders.NewRepository(path)

	listed, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Empty(t, listed[0].UID, "reading does not write the file")

	require.NoError(t, repo.Remove(ctx, 2))
	listed, err = repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assertUID(t, listed[0].UID)

	added, err := repo.Save(ctx, models.Reminder{Title: "Third", Due: listed[0].Due})
	require.NoError(t, err)
	assertUID(t, added.UID)
	assert.NotEqual(t, listed[0].UID, added.UID)

	again, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, listed[0].UID, again[0].UID, "a uid never changes once written")
}

// assertUID checks that a change naming uid is valid by docs/sync.md.
func assertUID(t *testing.T, uid string) {
	t.Helper()
	line := `{"v":1,"hlc":"0001794985200000.000000","dev":"7f3a9c2e5b1d4a60","type":"activity","uid":"` + uid +
		`","set":{"project":"Ops"}}`
	_, err := journal.ParseChange([]byte(line))
	assert.NoError(t, err, "uid %q", uid)
}

func assertSameReminder(t *testing.T, want, got models.Reminder) {
	t.Helper()
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.UID, got.UID)
	assert.Equal(t, want.Title, got.Title)
	assert.Equal(t, want.Project, got.Project)
	assert.True(t, want.Due.Equal(got.Due), "due %s != %s", want.Due, got.Due)
	assert.Equal(t, want.Repeat.Spec(), got.Repeat.Spec())
	assert.Equal(t, want.Repeat.Count, got.Repeat.Count)
	require.NotNil(t, got.Repeat.Until)
	assert.True(t, want.Repeat.Until.Equal(*got.Repeat.Until))
	assert.Equal(t, want.Notify, got.Notify)
	assert.Equal(t, want.Tags, got.Tags)
	assert.Equal(t, want.Notes, got.Notes)
	assert.True(t, want.Created.Equal(got.Created))

	require.Len(t, got.States, len(want.States))
	for key, state := range want.States {
		gotState := got.States[key]
		assert.Equal(t, state.Skipped, gotState.Skipped, key)
		if state.DoneAt != nil {
			require.NotNil(t, gotState.DoneAt, key)
			assert.True(t, state.DoneAt.Equal(*gotState.DoneAt), key)
			require.NotNil(t, gotState.ActivityStart, key)
		}
	}
}
