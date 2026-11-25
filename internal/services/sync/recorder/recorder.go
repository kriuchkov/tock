// Package recorder wraps the backend's repositories so every save and removal made through tock (the
// CLI, the TUI, the MCP server, the menu bar icon) reaches the sync journal (docs/sync.md, Changes
// made through tock). Each write runs inside ports.SyncChangeSink.Record, which takes the backend's
// change token around it and remembers what it did; reads go straight to the repository.
//
// The recorder only says what was written. The sink finds the record's uid (in the backend, or by
// start time in the sync state for backends that keep no uids), journals only the fields that
// differ from the merged state, and leaves out an auto-stop (ports.WithAutoStop, passed on with the
// context) of an activity this device does not own, while a stop made on purpose is journaled.
//
// The runtime wraps the repositories only while sync is on; the sync service itself writes through
// the unwrapped ones, since what it writes are other devices' changes.
package recorder

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// activities records the saves and removals of an ActivityRepository.
type activities struct {
	repo ports.ActivityRepository
	sink ports.SyncChangeSink
}

// NewActivityRepository returns repo with every Save and Remove recorded through sink, as
// SyncActivitySaved and SyncActivityRemoved writes of the activity it got. The context of a Save is
// passed on, so sink sees the auto-stop mark of ports.WithAutoStop.
func NewActivityRepository(repo ports.ActivityRepository, sink ports.SyncChangeSink) ports.ActivityRepository {
	return &activities{repo: repo, sink: sink}
}

func (a *activities) Save(ctx context.Context, activity models.Activity) error {
	return a.sink.Record(ctx, func(writeCtx context.Context) (ports.SyncWrite, error) {
		if err := a.repo.Save(writeCtx, activity); err != nil {
			return ports.SyncWrite{}, err
		}
		return ports.SyncWrite{Kind: ports.SyncActivitySaved, Activity: cloneActivity(activity)}, nil
	})
}

func (a *activities) Remove(ctx context.Context, activity models.Activity) error {
	return a.sink.Record(ctx, func(writeCtx context.Context) (ports.SyncWrite, error) {
		if err := a.repo.Remove(writeCtx, activity); err != nil {
			return ports.SyncWrite{}, err
		}
		return ports.SyncWrite{Kind: ports.SyncActivityRemoved, Activity: cloneActivity(activity)}, nil
	})
}

func (a *activities) FindLast(ctx context.Context) (*models.Activity, error) {
	return a.repo.FindLast(ctx)
}

func (a *activities) Find(ctx context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
	return a.repo.Find(ctx, filter)
}

// notes records the saves and deletions of a NotesRepository.
type notes struct {
	repo ports.NotesRepository
	sink ports.SyncChangeSink
}

// NewNotesRepository returns repo with every Save and Delete recorded through sink, as
// SyncNotesSaved and SyncNotesDeleted writes: Activity.StartTime is the date, Notes and Tags what was
// saved, ActivityID the key.
func NewNotesRepository(repo ports.NotesRepository, sink ports.SyncChangeSink) ports.NotesRepository {
	return &notes{repo: repo, sink: sink}
}

func (n *notes) Save(ctx context.Context, activityID string, date time.Time, text string, tags []string) error {
	return n.sink.Record(ctx, func(writeCtx context.Context) (ports.SyncWrite, error) {
		if err := n.repo.Save(writeCtx, activityID, date, text, tags); err != nil {
			return ports.SyncWrite{}, err
		}
		return ports.SyncWrite{
			Kind:       ports.SyncNotesSaved,
			Activity:   models.Activity{StartTime: date, Notes: text, Tags: slices.Clone(tags)},
			ActivityID: activityID,
		}, nil
	})
}

func (n *notes) Delete(ctx context.Context, activityID string, date time.Time) error {
	return n.sink.Record(ctx, func(writeCtx context.Context) (ports.SyncWrite, error) {
		if err := n.repo.Delete(writeCtx, activityID, date); err != nil {
			return ports.SyncWrite{}, err
		}
		return ports.SyncWrite{Kind: ports.SyncNotesDeleted, Activity: models.Activity{StartTime: date}, ActivityID: activityID}, nil
	})
}

func (n *notes) Get(ctx context.Context, activityID string, date time.Time) (string, []string, error) {
	return n.repo.Get(ctx, activityID, date)
}

// reminders records the saves and removals of a ReminderRepository.
type reminders struct {
	repo ports.ReminderRepository
	sink ports.SyncChangeSink
}

// NewReminderRepository returns repo with every Save and Remove recorded through sink: a
// SyncReminderSaved write of the reminder Save returned, with its ID and UID, and a
// SyncReminderRemoved write of the reminder as List gave it before Remove.
func NewReminderRepository(repo ports.ReminderRepository, sink ports.SyncChangeSink) ports.ReminderRepository {
	return &reminders{repo: repo, sink: sink}
}

func (r *reminders) List(ctx context.Context) ([]models.Reminder, error) {
	return r.repo.List(ctx)
}

func (r *reminders) Save(ctx context.Context, reminder models.Reminder) (*models.Reminder, error) {
	var saved *models.Reminder
	err := r.sink.Record(ctx, func(writeCtx context.Context) (ports.SyncWrite, error) {
		result, err := r.repo.Save(writeCtx, reminder)
		if err != nil {
			return ports.SyncWrite{}, err
		}
		saved = result
		written := reminder
		if result != nil {
			written = *result
		}
		return ports.SyncWrite{Kind: ports.SyncReminderSaved, Reminder: cloneReminder(written)}, nil
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// Remove lists the reminders first, under the same Record, so the sink gets the uid of the reminder
// it removes. A reminder List does not hold is recorded with its ID only.
func (r *reminders) Remove(ctx context.Context, id int) error {
	return r.sink.Record(ctx, func(writeCtx context.Context) (ports.SyncWrite, error) {
		list, err := r.repo.List(writeCtx)
		if err != nil {
			return ports.SyncWrite{}, errors.Wrap(err, "list reminders before removing one")
		}
		removed := models.Reminder{ID: id}
		if i := slices.IndexFunc(list, func(reminder models.Reminder) bool { return reminder.ID == id }); i >= 0 {
			removed = cloneReminder(list[i])
		}

		if err = r.repo.Remove(writeCtx, id); err != nil {
			return ports.SyncWrite{}, err
		}
		return ports.SyncWrite{Kind: ports.SyncReminderRemoved, Reminder: removed}, nil
	})
}

// cloneActivity copies an activity so the write the sink keeps until the next sync does not share
// memory with the caller's.
func cloneActivity(activity models.Activity) models.Activity {
	activity.Tags = slices.Clone(activity.Tags)
	activity.EndTime = cloneTime(activity.EndTime)
	return activity
}

// cloneReminder copies a reminder like cloneActivity.
func cloneReminder(reminder models.Reminder) models.Reminder {
	reminder.Tags = slices.Clone(reminder.Tags)
	reminder.Repeat.Weekdays = slices.Clone(reminder.Repeat.Weekdays)
	reminder.Repeat.Until = cloneTime(reminder.Repeat.Until)
	if reminder.States != nil {
		states := maps.Clone(reminder.States)
		for key, state := range states {
			state.DoneAt = cloneTime(state.DoneAt)
			state.SnoozedUntil = cloneTime(state.SnoozedUntil)
			state.ActivityStart = cloneTime(state.ActivityStart)
			states[key] = state
		}
		reminder.States = states
	}
	return reminder
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	copied := *t
	return &copied
}
