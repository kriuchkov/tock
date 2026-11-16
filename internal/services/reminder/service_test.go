package reminder_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/reminders"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	portsmocks "github.com/kriuchkov/tock/internal/core/ports/mocks"
	"github.com/kriuchkov/tock/internal/services/reminder"
)

// at is a time in November 2026.
func at(day, hour, minute int) time.Time {
	return time.Date(2026, time.November, day, hour, minute, 0, 0, time.Local)
}

func newService(t *testing.T, activities ports.ActivityResolver) ports.ReminderResolver {
	t.Helper()
	repo := reminders.NewRepository(filepath.Join(t.TempDir(), "reminders.yaml"))
	return reminder.NewService(repo, activities)
}

func addStandup(t *testing.T, svc ports.ReminderResolver) *models.Reminder {
	t.Helper()
	added, err := svc.Add(context.Background(), models.AddReminderRequest{
		Title:   "  Standup ",
		Project: "Work",
		Due:     time.Date(2026, time.November, 16, 9, 30, 42, 0, time.Local),
		Repeat:  models.Repeat{Freq: models.RepeatWeekdays},
		Tags:    []string{"team"},
	})
	require.NoError(t, err)
	return added
}

func TestAddNormalizesTheReminder(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, nil)

	added := addStandup(t, svc)
	assert.Equal(t, 1, added.ID)
	assert.Equal(t, "Standup", added.Title)
	assert.Equal(t, at(16, 9, 30), added.Due, "seconds are dropped")

	allDay, err := svc.Add(ctx, models.AddReminderRequest{Title: "Pay rent", Due: at(18, 15, 0), AllDay: true,
		Repeat: models.Repeat{Freq: models.RepeatMonthly, Week: models.WeekLast, Weekday: time.Wednesday}})
	require.NoError(t, err)
	assert.Equal(t, 2, allDay.ID)
	assert.Equal(t, at(25, 0, 0), allDay.Due, "moved to the first day the rule falls on, at the start of it")

	got, err := svc.Get(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, allDay.Due, got.Due)
}

func TestAddRejectsInvalidReminders(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, nil)
	until := at(20, 0, 0)

	_, err := svc.Add(ctx, models.AddReminderRequest{Title: " ", Due: at(16, 9, 0)})
	require.ErrorIs(t, err, coreErrors.ErrReminderTitleRequired)

	_, err = svc.Add(ctx, models.AddReminderRequest{Title: "No date"})
	require.ErrorIs(t, err, coreErrors.ErrReminderDueRequired)

	_, err = svc.Add(ctx, models.AddReminderRequest{Title: "Never", Due: at(18, 9, 0),
		Repeat: models.Repeat{Freq: models.RepeatMonthly, Week: models.WeekLast, Weekday: time.Wednesday, Until: &until}})
	require.ErrorIs(t, err, coreErrors.ErrReminderNeverDue)

	_, err = svc.Add(ctx, models.AddReminderRequest{Title: "Too early", Due: at(18, 9, 0), Notify: 8 * 24 * time.Hour})
	require.Error(t, err)

	reminders, err := svc.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, reminders)
}

func TestUpdateKeepsUntouchedFields(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, nil)
	added := addStandup(t, svc)

	title := "Daily standup"
	updated, err := svc.Update(ctx, added.ID, models.UpdateReminderRequest{Title: &title})
	require.NoError(t, err)
	assert.Equal(t, "Daily standup", updated.Title)
	assert.Equal(t, added.Due, updated.Due)
	assert.Equal(t, added.Repeat, updated.Repeat)
	assert.Equal(t, []string{"team"}, updated.Tags)

	_, err = svc.Update(ctx, 99, models.UpdateReminderRequest{Title: &title})
	require.ErrorIs(t, err, coreErrors.ErrReminderNotFound)
}

func TestSetDoneAndSnooze(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, nil)
	added := addStandup(t, svc)
	ref := models.OccurrenceRef{ID: added.ID, Day: at(17, 0, 0)}
	now := at(17, 9, 40)

	snoozed, err := svc.Snooze(ctx, ref, now.Add(10*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, models.ReminderSnoozed, snoozed.Status(now))

	done, err := svc.SetDone(ctx, ref, true, now)
	require.NoError(t, err)
	assert.Equal(t, models.ReminderDone, done.Status(now))
	assert.Nil(t, done.State.SnoozedUntil, "marking done ends the snooze")

	undone, err := svc.SetDone(ctx, ref, false, now)
	require.NoError(t, err)
	assert.Nil(t, undone.State.DoneAt)

	stored, err := svc.Get(ctx, added.ID)
	require.NoError(t, err)
	assert.Empty(t, stored.States, "an occurrence with nothing recorded is not stored")

	_, err = svc.SetDone(ctx, models.OccurrenceRef{ID: added.ID, Day: at(21, 0, 0)}, true, now)
	require.ErrorIs(t, err, coreErrors.ErrNoReminderOccurrence, "Saturday has no weekday occurrence")
}

func TestSkip(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, nil)
	added := addStandup(t, svc)

	require.NoError(t, svc.Skip(ctx, models.OccurrenceRef{ID: added.ID, Day: at(17, 0, 0)}))
	occurrences, err := svc.Occurrences(ctx, at(16, 0, 0), at(19, 0, 0), at(16, 8, 0))
	require.NoError(t, err)
	require.Len(t, occurrences, 2)
	assert.Equal(t, "2026-11-16", occurrences[0].Key())
	assert.Equal(t, "2026-11-18", occurrences[1].Key())

	oneOff, err := svc.Add(ctx, models.AddReminderRequest{Title: "Dentist", Due: at(17, 15, 0)})
	require.NoError(t, err)
	require.NoError(t, svc.Skip(ctx, models.OccurrenceRef{ID: oneOff.ID, Day: oneOff.Due}))
	_, err = svc.Get(ctx, oneOff.ID)
	require.ErrorIs(t, err, coreErrors.ErrReminderNotFound, "skipping a one-off removes it")
}

func TestOverdue(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, nil)
	addStandup(t, svc)
	_, err := svc.Add(ctx, models.AddReminderRequest{Title: "Later", Due: at(18, 18, 0)})
	require.NoError(t, err)

	overdue, err := svc.Overdue(ctx, at(18, 12, 0))
	require.NoError(t, err)
	require.Len(t, overdue, 1)
	assert.Equal(t, "Standup", overdue[0].Reminder.Title)
	assert.Equal(t, at(18, 9, 30), overdue[0].Due, "only the latest missed occurrence is overdue")
}

func TestOverdueKeepsAnOccurrenceSnoozedPastItsDueTime(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, nil)
	call, err := svc.Add(ctx, models.AddReminderRequest{Title: "Call", Due: at(16, 10, 0)})
	require.NoError(t, err)
	_, err = svc.Snooze(ctx, models.OccurrenceRef{ID: call.ID, Day: at(16, 0, 0)}, at(18, 9, 0))
	require.NoError(t, err)

	overdue, err := svc.Overdue(ctx, at(17, 8, 0))
	require.NoError(t, err)
	require.Len(t, overdue, 1, "still listed while snoozed")
	assert.Equal(t, models.ReminderSnoozed, overdue[0].Status(at(17, 8, 0)))

	current, err := svc.Current(ctx, call.ID, at(17, 8, 0))
	require.NoError(t, err)
	assert.Equal(t, at(16, 10, 0), current.Due)
}

func TestRemove(t *testing.T) {
	ctx := context.Background()
	svc := newService(t, nil)
	added := addStandup(t, svc)

	require.NoError(t, svc.Remove(ctx, added.ID))
	require.ErrorIs(t, svc.Remove(ctx, added.ID), coreErrors.ErrReminderNotFound)

	next, err := svc.Add(ctx, models.AddReminderRequest{Title: "Next", Due: at(18, 9, 0)})
	require.NoError(t, err)
	assert.Equal(t, 2, next.ID, "IDs are not reused")
}

func TestTrackStartsAnActivityAndMarksTheOccurrenceDone(t *testing.T) {
	ctx := context.Background()
	activities := portsmocks.NewMockActivityResolver(t)
	svc := newService(t, activities)
	added := addStandup(t, svc)
	start := at(17, 9, 32)

	activities.EXPECT().Start(mock.Anything, models.StartActivityRequest{
		Description: "Standup", Project: "Work", StartTime: start, Tags: []string{"team"},
	}).Return(&models.Activity{Description: "Standup", Project: "Work", StartTime: start}, nil)

	ref := models.OccurrenceRef{ID: added.ID, Day: start}
	activity, err := svc.Track(ctx, ref, models.TrackReminderRequest{StartTime: start})
	require.NoError(t, err)
	assert.Equal(t, "Work", activity.Project)

	stored, err := svc.Get(ctx, added.ID)
	require.NoError(t, err)
	state := stored.State(start)
	require.NotNil(t, state.DoneAt)
	require.NotNil(t, state.ActivityStart)
	assert.True(t, start.Equal(*state.ActivityStart), "read back from the store, so compare instants")
}

func TestTrackNeedsAProject(t *testing.T) {
	ctx := context.Background()
	activities := portsmocks.NewMockActivityResolver(t)
	svc := newService(t, activities)
	added, err := svc.Add(ctx, models.AddReminderRequest{Title: "Renew domain", Due: at(16, 0, 0), AllDay: true})
	require.NoError(t, err)

	_, err = svc.Track(ctx, models.OccurrenceRef{ID: added.ID, Day: added.Due}, models.TrackReminderRequest{StartTime: at(16, 12, 0)})
	require.ErrorIs(t, err, coreErrors.ErrProjectRequired)

	activities.EXPECT().Start(mock.Anything, mock.MatchedBy(func(req models.StartActivityRequest) bool {
		return req.Project == "Home"
	})).Return(&models.Activity{Project: "Home", Description: "Renew domain"}, nil)
	_, err = svc.Track(
		ctx,
		models.OccurrenceRef{ID: added.ID, Day: added.Due},
		models.TrackReminderRequest{StartTime: at(16, 12, 0), Project: "Home"},
	)
	require.NoError(t, err)
}
