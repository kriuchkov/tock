package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
)

func TestReminderRepositoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	activities, err := NewSQLiteActivityRepository(ctx, filepath.Join(t.TempDir(), "tock.db"))
	require.NoError(t, err)
	defer activities.DB.Close()

	repo, err := NewReminderRepository(ctx, activities.DB)
	require.NoError(t, err)

	until := time.Date(2026, time.December, 31, 0, 0, 0, 0, time.Local)
	doneAt := time.Date(2026, time.November, 16, 9, 35, 0, 0, time.Local)
	reminder := models.Reminder{
		Title:   "Send invoice",
		Project: "Admin",
		Due:     time.Date(2026, time.November, 16, 9, 0, 0, 0, time.Local),
		Repeat:  models.Repeat{Freq: models.RepeatMonthly, Week: models.WeekLast, Weekday: time.Friday, Until: &until},
		Notify:  time.Hour,
		Tags:    []string{"money"},
		Created: time.Date(2026, time.November, 1, 8, 0, 0, 0, time.UTC),
		States:  map[string]models.OccurrenceState{"2026-11-16": {DoneAt: &doneAt}},
	}

	saved, err := repo.Save(ctx, reminder)
	require.NoError(t, err)
	assert.Equal(t, 1, saved.ID)

	_, err = repo.Save(ctx, models.Reminder{Title: "Second", Due: reminder.Due, AllDay: true})
	require.NoError(t, err)

	saved.Title = "Send the invoice"
	_, err = repo.Save(ctx, *saved)
	require.NoError(t, err)

	listed, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 2)

	got := listed[0]
	assert.Equal(t, "Send the invoice", got.Title)
	assert.Equal(t, "Admin", got.Project)
	assert.True(t, reminder.Due.Equal(got.Due))
	assert.Equal(t, "monthly:last-fri", got.Repeat.Spec())
	require.NotNil(t, got.Repeat.Until)
	assert.True(t, until.Equal(*got.Repeat.Until))
	assert.Equal(t, time.Hour, got.Notify)
	assert.Equal(t, []string{"money"}, got.Tags)
	assert.True(t, reminder.Created.Equal(got.Created))
	require.NotNil(t, got.States["2026-11-16"].DoneAt)
	assert.True(t, doneAt.Equal(*got.States["2026-11-16"].DoneAt))
	assert.True(t, listed[1].AllDay)
	assert.Empty(t, listed[1].Tags)

	require.NoError(t, repo.Remove(ctx, 2))
	require.ErrorIs(t, repo.Remove(ctx, 2), coreErrors.ErrReminderNotFound)

	third, err := repo.Save(ctx, models.Reminder{Title: "Third", Due: reminder.Due})
	require.NoError(t, err)
	assert.Equal(t, 3, third.ID, "AUTOINCREMENT does not reuse removed IDs")
}
