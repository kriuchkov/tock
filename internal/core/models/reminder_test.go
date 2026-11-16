package models_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

func standup() models.Reminder {
	return models.Reminder{
		ID:     4,
		Title:  "Standup",
		Due:    at(16, 9, 30),
		Repeat: models.Repeat{Freq: models.RepeatDaily},
	}
}

func TestOccurrenceStatus(t *testing.T) {
	due := at(16, 13, 0)
	snoozedUntil := due.Add(30 * time.Minute)

	tests := []struct {
		name       string
		occurrence models.ReminderOccurrence
		now        time.Time
		want       models.ReminderStatus
	}{
		{name: "before due", now: due.Add(-time.Minute), want: models.ReminderPending},
		{name: "at due", now: due, want: models.ReminderOverdue},
		{
			name:       "done",
			occurrence: models.ReminderOccurrence{State: models.OccurrenceState{DoneAt: &due}},
			now:        due.Add(time.Hour),
			want:       models.ReminderDone,
		},
		{
			name:       "snoozed",
			occurrence: models.ReminderOccurrence{State: models.OccurrenceState{SnoozedUntil: &snoozedUntil}},
			now:        due.Add(10 * time.Minute),
			want:       models.ReminderSnoozed,
		},
		{
			name:       "snooze over",
			occurrence: models.ReminderOccurrence{State: models.OccurrenceState{SnoozedUntil: &snoozedUntil}},
			now:        snoozedUntil,
			want:       models.ReminderOverdue,
		},
		{name: "missed", occurrence: models.ReminderOccurrence{Missed: true}, now: due.Add(time.Hour), want: models.ReminderMissed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			occurrence := tt.occurrence
			occurrence.Due = due
			assert.Equal(t, tt.want, occurrence.Status(tt.now))
		})
	}
}

func TestAllDayOccurrenceIsDueUntilTheEndOfItsDay(t *testing.T) {
	reminder := models.Reminder{Due: day(2026, time.November, 16), AllDay: true, Notify: 30 * time.Minute}
	occurrence := models.ReminderOccurrence{Reminder: reminder, Due: reminder.Due}

	assert.Equal(t, models.ReminderPending, occurrence.Status(at(16, 23, 59)))
	assert.Equal(t, models.ReminderOverdue, occurrence.Status(day(2026, time.November, 17)))
	assert.Equal(t, at(16, 8, 30), occurrence.NotifyAt())
}

func TestSnoozeOnlyPushesTheDueTimeLater(t *testing.T) {
	due := at(16, 17, 0)
	early := at(16, 10, 10)
	occurrence := models.ReminderOccurrence{
		Reminder: models.Reminder{Due: due},
		Due:      due,
		State:    models.OccurrenceState{SnoozedUntil: &early},
	}

	assert.Equal(t, models.ReminderSnoozed, occurrence.Status(at(16, 10, 0)))
	assert.Equal(t, models.ReminderPending, occurrence.Status(at(16, 10, 15)), "a snooze before the due time does not make it overdue")
	assert.Equal(t, due, occurrence.DueBy())
	assert.Equal(t, models.ReminderOverdue, occurrence.Status(due))
}

func TestCurrentKeepsAnEarlierOccurrenceWaitingOnItsSnooze(t *testing.T) {
	until := at(18, 9, 0)
	weekly := models.Reminder{Due: at(16, 10, 0), Repeat: models.Repeat{Freq: models.RepeatWeekly}}.
		WithState(at(16, 10, 0), models.OccurrenceState{SnoozedUntil: &until})

	current, ok := weekly.Current(at(17, 8, 0))
	require.True(t, ok)
	assert.Equal(t, "2026-11-16", current.Key())
	assert.Equal(t, models.ReminderSnoozed, current.Status(at(17, 8, 0)))

	current, ok = weekly.Current(at(18, 9, 0))
	require.True(t, ok)
	assert.Equal(t, "2026-11-16", current.Key())
	assert.Equal(t, models.ReminderOverdue, current.Status(at(18, 9, 0)))
}

func TestAllDayOccurrenceIsOverdueTheDayAfter(t *testing.T) {
	daily := models.Reminder{Due: day(2026, time.November, 16), AllDay: true, Repeat: models.Repeat{Freq: models.RepeatDaily}}
	now := at(18, 10, 0)

	occurrences := daily.Occurrences(day(2026, time.November, 16), day(2026, time.November, 19), now)
	require.Len(t, occurrences, 3)
	assert.Equal(t, models.ReminderMissed, occurrences[0].Status(now))
	assert.Equal(t, models.ReminderOverdue, occurrences[1].Status(now))
	assert.Equal(t, models.ReminderPending, occurrences[2].Status(now))

	current, ok := daily.Current(now)
	require.True(t, ok)
	assert.Equal(t, "2026-11-17", current.Key())
}

func TestLastIsWhereAFinishedReminderEnded(t *testing.T) {
	doneAt := at(10, 9, 5)
	oneOff := models.Reminder{Due: at(10, 9, 0)}.WithState(at(10, 9, 0), models.OccurrenceState{DoneAt: &doneAt})
	now := at(16, 9, 0)

	_, ok := oneOff.Current(now)
	require.False(t, ok)
	last, ok := oneOff.Last(now)
	require.True(t, ok)
	assert.Equal(t, models.ReminderDone, last.Status(now))
}

func TestOnlyTheLatestPastOccurrenceIsOverdue(t *testing.T) {
	reminder := standup()
	now := at(18, 10, 0)

	occurrences := reminder.Occurrences(day(2026, time.November, 16), day(2026, time.November, 20), now)
	require.Len(t, occurrences, 4)

	statuses := make([]models.ReminderStatus, 0, len(occurrences))
	for _, occurrence := range occurrences {
		statuses = append(statuses, occurrence.Status(now))
	}
	assert.Equal(t, []models.ReminderStatus{
		models.ReminderMissed, models.ReminderMissed, models.ReminderOverdue, models.ReminderPending,
	}, statuses)
}

func TestSkippedOccurrencesAreLeftOut(t *testing.T) {
	reminder := standup().WithState(day(2026, time.November, 17), models.OccurrenceState{Skipped: true})
	now := at(18, 10, 0)

	assert.Equal(
		t,
		[]string{"2026-11-16", "2026-11-18"},
		occurrenceDays(reminder, day(2026, time.November, 16), day(2026, time.November, 19)),
	)
	_, ok := reminder.OccurrenceOn(day(2026, time.November, 17), now)
	assert.False(t, ok)

	// Skipping the latest one leaves the one before it as the latest, so it is overdue, not missed.
	reminder = reminder.WithState(day(2026, time.November, 18), models.OccurrenceState{Skipped: true})
	current, ok := reminder.Current(now)
	require.True(t, ok)
	assert.Equal(t, "2026-11-16", current.Key())
	assert.Equal(t, models.ReminderOverdue, current.Status(now))
}

func TestCurrent(t *testing.T) {
	reminder := standup()

	t.Run("overdue first", func(t *testing.T) {
		now := at(17, 10, 0)
		current, ok := reminder.Current(now)
		require.True(t, ok)
		assert.Equal(t, at(17, 9, 30), current.Due)
	})

	t.Run("today's pending one once yesterday's is done", func(t *testing.T) {
		now := at(17, 9, 0)
		doneAt := at(16, 9, 35)
		current, ok := reminder.WithState(doneAt, models.OccurrenceState{DoneAt: &doneAt}).Current(now)
		require.True(t, ok)
		assert.Equal(t, at(17, 9, 30), current.Due)
	})

	t.Run("next one when today's is done", func(t *testing.T) {
		now := at(17, 10, 0)
		done := reminder.WithState(now, models.OccurrenceState{DoneAt: &now})
		current, ok := done.Current(now)
		require.True(t, ok)
		assert.Equal(t, at(18, 9, 30), current.Due)
	})

	t.Run("a finished one-off has none", func(t *testing.T) {
		oneOff := models.Reminder{Due: at(16, 9, 0)}
		doneAt := oneOff.Due
		oneOff = oneOff.WithState(oneOff.Due, models.OccurrenceState{DoneAt: &doneAt})
		_, ok := oneOff.Current(at(17, 9, 0))
		assert.False(t, ok)
	})
}

func TestNextAfterSkipsDoneOccurrences(t *testing.T) {
	reminder := standup()
	doneAt := at(18, 9, 31)
	reminder = reminder.WithState(doneAt, models.OccurrenceState{DoneAt: &doneAt})

	next, ok := reminder.NextAfter(at(17, 10, 0), at(17, 10, 0))
	require.True(t, ok)
	assert.Equal(t, at(19, 9, 30), next.Due)
}

func TestWithStateDropsEmptyStates(t *testing.T) {
	reminder := standup()
	doneAt := at(16, 9, 31)

	done := reminder.WithState(doneAt, models.OccurrenceState{DoneAt: &doneAt})
	require.Len(t, done.States, 1)
	assert.Nil(t, reminder.States, "the original is left untouched")

	cleared := done.WithState(doneAt, models.OccurrenceState{})
	assert.Nil(t, cleared.States)
}

func TestSortOccurrences(t *testing.T) {
	allDay := models.ReminderOccurrence{Reminder: models.Reminder{ID: 9, AllDay: true}, Due: day(2026, time.November, 16)}
	early := models.ReminderOccurrence{Reminder: models.Reminder{ID: 2}, Due: at(16, 9, 0)}
	sameTimeLowID := models.ReminderOccurrence{Reminder: models.Reminder{ID: 1}, Due: at(16, 9, 0)}
	midnight := models.ReminderOccurrence{Reminder: models.Reminder{ID: 3}, Due: day(2026, time.November, 16)}

	occurrences := []models.ReminderOccurrence{early, midnight, sameTimeLowID, allDay}
	models.SortOccurrences(occurrences)
	assert.Equal(t, []models.ReminderOccurrence{allDay, midnight, sameTimeLowID, early}, occurrences)
}

func TestReminderMarshalJSONSpellsOutTheRepeat(t *testing.T) {
	until := day(2026, time.December, 31)
	reminder := models.Reminder{
		ID:     1,
		Title:  "Team sync",
		Due:    at(16, 17, 30),
		Repeat: models.Repeat{Freq: models.RepeatWeekly, Weekdays: []time.Weekday{time.Monday}, Until: &until, Count: 5},
		Notify: 10 * time.Minute,
	}

	raw, err := json.Marshal(reminder)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, "weekly:mon", decoded["every"])
	assert.Equal(t, "2026-12-31", decoded["until"])
	assert.InDelta(t, 5, decoded["count"], 0)
	assert.Equal(t, "10m0s", decoded["notify"])
	assert.Equal(t, "Team sync", decoded["title"])
}

func TestUpdateReminderRequestApply(t *testing.T) {
	original := standup()
	req := models.UpdateReminderRequest{Title: new("Daily standup"), Notify: new(5 * time.Minute)}
	require.False(t, req.IsEmpty())
	assert.True(t, models.UpdateReminderRequest{}.IsEmpty())

	updated := req.Apply(original)
	assert.Equal(t, "Daily standup", updated.Title)
	assert.Equal(t, 5*time.Minute, updated.Notify)
	assert.Equal(t, original.Due, updated.Due)
	assert.Equal(t, original.Repeat, updated.Repeat)
}
