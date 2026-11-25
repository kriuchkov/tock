package recorder_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	portsmocks "github.com/kriuchkov/tock/internal/core/ports/mocks"
	"github.com/kriuchkov/tock/internal/services/sync/recorder"
)

const (
	testUID      = "019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a10"
	reminderUID  = "019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a20"
	otherUID     = "019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a30"
	activityKey  = "100000"
	projectName  = "Backend"
	reminderName = "Standup"
)

// sinkKey marks the context the sink hands to write, to check that the repository gets it.
type sinkKey struct{}

// recorded is one write the sink took: what it did and whether its context carried the auto-stop mark.
type recorded struct {
	write    ports.SyncWrite
	autoStop bool
}

// newSink returns a sink that runs every write, like the sync service does, and keeps what it
// recorded. The context it passes to write carries sinkKey.
func newSink(t *testing.T) (*portsmocks.MockSyncChangeSink, *[]recorded) {
	t.Helper()
	sink := portsmocks.NewMockSyncChangeSink(t)
	var writes []recorded
	sink.EXPECT().Record(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, write func(context.Context) (ports.SyncWrite, error)) error {
			done, err := write(context.WithValue(ctx, sinkKey{}, true))
			if err != nil {
				return err
			}
			writes = append(writes, recorded{write: done, autoStop: ports.IsAutoStop(ctx)})
			return nil
		}).Maybe()
	return sink, &writes
}

// fromSink matches the context the sink passed to write.
func fromSink() any {
	return mock.MatchedBy(func(ctx context.Context) bool {
		marked, _ := ctx.Value(sinkKey{}).(bool)
		return marked
	})
}

func sampleActivity() models.Activity {
	start := time.Date(2026, time.November, 18, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	return models.Activity{
		Project:     projectName,
		Description: "API implementation",
		StartTime:   start,
		EndTime:     &end,
		Tags:        []string{"api"},
	}
}

func TestActivitySave(t *testing.T) {
	tests := []struct {
		name     string
		ctx      func(context.Context) context.Context
		autoStop bool
	}{
		{name: "a stop made on purpose", ctx: func(ctx context.Context) context.Context { return ctx }},
		{name: "an auto-stop keeps its mark for the sink", ctx: ports.WithAutoStop, autoStop: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := portsmocks.NewMockActivityRepository(t)
			sink, writes := newSink(t)
			activity := sampleActivity()
			repo.EXPECT().Save(fromSink(), activity).Return(nil).Once()

			require.NoError(t, recorder.NewActivityRepository(repo, sink).Save(tt.ctx(t.Context()), activity))

			require.Len(t, *writes, 1)
			got := (*writes)[0]
			assert.Equal(t, ports.SyncActivitySaved, got.write.Kind)
			assert.Equal(t, activity, got.write.Activity)
			assert.Equal(t, tt.autoStop, got.autoStop)
		})
	}
}

func TestActivitySaveKeepsItsOwnCopy(t *testing.T) {
	repo := portsmocks.NewMockActivityRepository(t)
	sink, writes := newSink(t)
	activity := sampleActivity()
	repo.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()

	require.NoError(t, recorder.NewActivityRepository(repo, sink).Save(t.Context(), activity))
	activity.Tags[0] = "changed"
	*activity.EndTime = activity.EndTime.Add(time.Hour)

	require.Len(t, *writes, 1)
	assert.Equal(t, []string{"api"}, (*writes)[0].write.Activity.Tags)
	assert.Equal(t, sampleActivity().EndTime, (*writes)[0].write.Activity.EndTime)
}

func TestActivitySaveFails(t *testing.T) {
	repo := portsmocks.NewMockActivityRepository(t)
	sink, writes := newSink(t)
	failure := errors.New("disk full")
	repo.EXPECT().Save(mock.Anything, mock.Anything).Return(failure).Once()

	err := recorder.NewActivityRepository(repo, sink).Save(t.Context(), sampleActivity())

	require.ErrorIs(t, err, failure)
	assert.Empty(t, *writes, "a failed save records nothing")
}

func TestActivitySinkFails(t *testing.T) {
	repo := portsmocks.NewMockActivityRepository(t)
	sink := portsmocks.NewMockSyncChangeSink(t)
	failure := errors.New("state locked")
	sink.EXPECT().Record(mock.Anything, mock.Anything).Return(failure).Times(2)
	activities := recorder.NewActivityRepository(repo, sink)

	require.ErrorIs(t, activities.Save(t.Context(), sampleActivity()), failure)
	require.ErrorIs(t, activities.Remove(t.Context(), sampleActivity()), failure)
}

func TestActivityRemove(t *testing.T) {
	repo := portsmocks.NewMockActivityRepository(t)
	sink, writes := newSink(t)
	activity := sampleActivity()
	activity.UID = testUID
	repo.EXPECT().Remove(fromSink(), activity).Return(nil).Once()

	require.NoError(t, recorder.NewActivityRepository(repo, sink).Remove(t.Context(), activity))

	require.Len(t, *writes, 1)
	assert.Equal(t, ports.SyncWrite{Kind: ports.SyncActivityRemoved, Activity: activity}, (*writes)[0].write)
}

func TestActivityRemoveFails(t *testing.T) {
	repo := portsmocks.NewMockActivityRepository(t)
	sink, writes := newSink(t)
	repo.EXPECT().Remove(mock.Anything, mock.Anything).Return(coreErrors.ErrActivityNotFound).Once()

	err := recorder.NewActivityRepository(repo, sink).Remove(t.Context(), sampleActivity())

	require.ErrorIs(t, err, coreErrors.ErrActivityNotFound)
	assert.Empty(t, *writes)
}

func TestActivityReadsPassThrough(t *testing.T) {
	repo := portsmocks.NewMockActivityRepository(t)
	sink := portsmocks.NewMockSyncChangeSink(t) // no Record expected
	activity := sampleActivity()
	running := true
	filter := models.ActivityFilter{IsRunning: &running}
	repo.EXPECT().FindLast(mock.Anything).Return(&activity, nil).Once()
	repo.EXPECT().Find(mock.Anything, filter).Return([]models.Activity{activity}, nil).Once()
	activities := recorder.NewActivityRepository(repo, sink)

	last, err := activities.FindLast(t.Context())
	require.NoError(t, err)
	assert.Equal(t, &activity, last)

	found, err := activities.Find(t.Context(), filter)
	require.NoError(t, err)
	assert.Equal(t, []models.Activity{activity}, found)
}

func TestNotesSave(t *testing.T) {
	repo := portsmocks.NewMockNotesRepository(t)
	sink, writes := newSink(t)
	date := sampleActivity().StartTime
	tags := []string{"api", "review"}
	repo.EXPECT().Save(fromSink(), activityKey, date, "Paired on it", tags).Return(nil).Once()

	require.NoError(t, recorder.NewNotesRepository(repo, sink).Save(t.Context(), activityKey, date, "Paired on it", tags))
	tags[0] = "changed"

	require.Len(t, *writes, 1)
	assert.Equal(t, ports.SyncWrite{
		Kind:       ports.SyncNotesSaved,
		Activity:   models.Activity{StartTime: date, Notes: "Paired on it", Tags: []string{"api", "review"}},
		ActivityID: activityKey,
	}, (*writes)[0].write)
}

func TestNotesDelete(t *testing.T) {
	repo := portsmocks.NewMockNotesRepository(t)
	sink, writes := newSink(t)
	date := sampleActivity().StartTime
	repo.EXPECT().Delete(fromSink(), activityKey, date).Return(nil).Once()

	require.NoError(t, recorder.NewNotesRepository(repo, sink).Delete(t.Context(), activityKey, date))

	require.Len(t, *writes, 1)
	assert.Equal(t, ports.SyncWrite{
		Kind:       ports.SyncNotesDeleted,
		Activity:   models.Activity{StartTime: date},
		ActivityID: activityKey,
	}, (*writes)[0].write)
}

func TestNotesWriteFails(t *testing.T) {
	repo := portsmocks.NewMockNotesRepository(t)
	sink, writes := newSink(t)
	failure := errors.New("read-only file system")
	date := sampleActivity().StartTime
	repo.EXPECT().Save(mock.Anything, activityKey, date, "x", []string(nil)).Return(failure).Once()
	repo.EXPECT().Delete(mock.Anything, activityKey, date).Return(failure).Once()
	notes := recorder.NewNotesRepository(repo, sink)

	require.ErrorIs(t, notes.Save(t.Context(), activityKey, date, "x", nil), failure)
	require.ErrorIs(t, notes.Delete(t.Context(), activityKey, date), failure)
	assert.Empty(t, *writes)
}

func TestNotesGetPassesThrough(t *testing.T) {
	repo := portsmocks.NewMockNotesRepository(t)
	sink := portsmocks.NewMockSyncChangeSink(t) // no Record expected
	date := sampleActivity().StartTime
	repo.EXPECT().Get(mock.Anything, activityKey, date).Return("Paired on it", []string{"api"}, nil).Once()

	text, tags, err := recorder.NewNotesRepository(repo, sink).Get(t.Context(), activityKey, date)

	require.NoError(t, err)
	assert.Equal(t, "Paired on it", text)
	assert.Equal(t, []string{"api"}, tags)
}

func sampleReminder() models.Reminder {
	done := time.Date(2026, time.November, 18, 9, 5, 0, 0, time.UTC)
	return models.Reminder{
		Title:   reminderName,
		Project: projectName,
		Due:     time.Date(2026, time.November, 18, 9, 0, 0, 0, time.UTC),
		Repeat:  models.Repeat{Freq: models.RepeatWeekly, Weekdays: []time.Weekday{time.Monday}},
		Tags:    []string{"team"},
		States:  map[string]models.OccurrenceState{"2026-11-18": {DoneAt: &done}},
	}
}

func TestReminderSave(t *testing.T) {
	repo := portsmocks.NewMockReminderRepository(t)
	sink, writes := newSink(t)
	reminder := sampleReminder()
	stored := sampleReminder()
	stored.ID = 3
	stored.UID = reminderUID
	repo.EXPECT().Save(fromSink(), reminder).Return(&stored, nil).Once()

	saved, err := recorder.NewReminderRepository(repo, sink).Save(t.Context(), reminder)

	require.NoError(t, err)
	assert.Same(t, &stored, saved, "the caller gets what the repository returned")
	require.Len(t, *writes, 1)
	got := (*writes)[0].write
	assert.Equal(t, ports.SyncReminderSaved, got.Kind)
	assert.Equal(t, stored, got.Reminder, "the sink gets the ID and the UID the repository gave")

	// What the sink keeps does not change with the caller's copy.
	stored.Tags[0] = "changed"
	*stored.States["2026-11-18"].DoneAt = time.Time{}
	stored.Repeat.Weekdays[0] = time.Friday
	assert.Equal(t, []string{"team"}, got.Reminder.Tags)
	assert.Equal(t, *sampleReminder().States["2026-11-18"].DoneAt, *got.Reminder.States["2026-11-18"].DoneAt)
	assert.Equal(t, []time.Weekday{time.Monday}, got.Reminder.Repeat.Weekdays)
}

func TestReminderSaveFails(t *testing.T) {
	repo := portsmocks.NewMockReminderRepository(t)
	sink, writes := newSink(t)
	failure := errors.New("invalid reminder")
	repo.EXPECT().Save(mock.Anything, mock.Anything).Return(nil, failure).Once()

	saved, err := recorder.NewReminderRepository(repo, sink).Save(t.Context(), sampleReminder())

	require.ErrorIs(t, err, failure)
	assert.Nil(t, saved)
	assert.Empty(t, *writes)
}

func TestReminderRemove(t *testing.T) {
	repo := portsmocks.NewMockReminderRepository(t)
	sink, writes := newSink(t)
	target := sampleReminder()
	target.ID = 3
	target.UID = reminderUID
	other := models.Reminder{ID: 4, UID: otherUID, Title: "Review"}
	listed := repo.EXPECT().List(fromSink()).Return([]models.Reminder{other, target}, nil).Once()
	repo.EXPECT().Remove(fromSink(), 3).Return(nil).Once().NotBefore(listed)

	require.NoError(t, recorder.NewReminderRepository(repo, sink).Remove(t.Context(), 3))

	require.Len(t, *writes, 1)
	assert.Equal(t, ports.SyncWrite{Kind: ports.SyncReminderRemoved, Reminder: target}, (*writes)[0].write)
}

func TestReminderRemoveUnlisted(t *testing.T) {
	repo := portsmocks.NewMockReminderRepository(t)
	sink, writes := newSink(t)
	repo.EXPECT().List(mock.Anything).Return(nil, nil).Once()
	repo.EXPECT().Remove(mock.Anything, 7).Return(nil).Once()

	require.NoError(t, recorder.NewReminderRepository(repo, sink).Remove(t.Context(), 7))

	require.Len(t, *writes, 1)
	assert.Equal(t, ports.SyncWrite{Kind: ports.SyncReminderRemoved, Reminder: models.Reminder{ID: 7}}, (*writes)[0].write)
}

func TestReminderRemoveFails(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		repo := portsmocks.NewMockReminderRepository(t)
		sink, writes := newSink(t)
		failure := errors.New("broken yaml")
		repo.EXPECT().List(mock.Anything).Return(nil, failure).Once()

		err := recorder.NewReminderRepository(repo, sink).Remove(t.Context(), 3)

		require.ErrorIs(t, err, failure)
		assert.Empty(t, *writes, "nothing is removed when the list fails")
	})
	t.Run("remove", func(t *testing.T) {
		repo := portsmocks.NewMockReminderRepository(t)
		sink, writes := newSink(t)
		repo.EXPECT().List(mock.Anything).Return(nil, nil).Once()
		repo.EXPECT().Remove(mock.Anything, 3).Return(coreErrors.ErrReminderNotFound).Once()

		err := recorder.NewReminderRepository(repo, sink).Remove(t.Context(), 3)

		require.ErrorIs(t, err, coreErrors.ErrReminderNotFound)
		assert.Empty(t, *writes)
	})
}

func TestReminderListPassesThrough(t *testing.T) {
	repo := portsmocks.NewMockReminderRepository(t)
	sink := portsmocks.NewMockSyncChangeSink(t) // no Record expected
	listed := []models.Reminder{{ID: 1, UID: reminderUID, Title: reminderName}}
	repo.EXPECT().List(mock.Anything).Return(listed, nil).Once()

	got, err := recorder.NewReminderRepository(repo, sink).List(t.Context())

	require.NoError(t, err)
	assert.Equal(t, listed, got)
}
