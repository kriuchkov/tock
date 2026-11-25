package ports

import (
	"context"
	"time"

	"github.com/kriuchkov/tock/internal/core/models"
)

type ActivityResolver interface {
	Start(ctx context.Context, req models.StartActivityRequest) (*models.Activity, error)
	Stop(ctx context.Context, req models.StopActivityRequest) (*models.Activity, error)
	// Continue starts a recent activity again, stopping the running one like Start does.
	Continue(ctx context.Context, req models.ContinueActivityRequest) (*models.Activity, error)
	// StopAfterWorkingHours stops the running activity at the last end of a working day it ran past.
	// It returns the stopped activity, or nil when nothing had to stop.
	StopAfterWorkingHours(ctx context.Context, hours models.WorkingHours, now time.Time) (*models.Activity, error)
	Add(ctx context.Context, req models.AddActivityRequest) (*models.Activity, error)
	List(ctx context.Context, filter models.ActivityFilter) ([]models.Activity, error)
	GetReport(ctx context.Context, filter models.ActivityFilter) (*models.Report, error)
	GetRecent(ctx context.Context, limit int) ([]models.Activity, error)
	GetLast(ctx context.Context) (*models.Activity, error)
	// GetByKey returns the activity with a DATE-INDEX key such as 2026-11-18-02.
	GetByKey(ctx context.Context, key string) (*models.Activity, error)
	Update(ctx context.Context, original models.Activity, req models.UpdateActivityRequest) (*models.Activity, error)
	AddNote(ctx context.Context, activity models.Activity, note string) (*models.Activity, error)
	AddTags(ctx context.Context, activity models.Activity, tags []string) (*models.Activity, error)
	Remove(ctx context.Context, activity models.Activity) error
}

type ActivityRepository interface {
	Save(ctx context.Context, activity models.Activity) error
	FindLast(ctx context.Context) (*models.Activity, error)
	Find(ctx context.Context, filter models.ActivityFilter) ([]models.Activity, error)
	Remove(ctx context.Context, activity models.Activity) error
}

type NotesRepository interface {
	Save(ctx context.Context, activityID string, date time.Time, notes string, tags []string) error
	Get(ctx context.Context, activityID string, date time.Time) (string, []string, error)
	Delete(ctx context.Context, activityID string, date time.Time) error
}

// NotesLookup is implemented by a NotesRepository that keeps each activity's notes in a file of its
// own. Get returns the same for an activity without a notes file and for one whose notes were cleared,
// which tock does by saving empty notes; Lookup tells them apart, so sync never takes a notes file that
// is not there, or not there yet, for notes cleared on purpose.
type NotesLookup interface {
	// Lookup returns what Get returns, and whether the activity has a notes file.
	Lookup(ctx context.Context, activityID string, date time.Time) (notes string, tags []string, found bool, err error)
}

// ReminderResolver is the reminder API used by the delivery layer.
type ReminderResolver interface {
	Add(ctx context.Context, req models.AddReminderRequest) (*models.Reminder, error)
	Get(ctx context.Context, id int) (*models.Reminder, error)
	List(ctx context.Context) ([]models.Reminder, error)
	Update(ctx context.Context, id int, req models.UpdateReminderRequest) (*models.Reminder, error)
	Remove(ctx context.Context, id int) error
	// Occurrences returns the occurrences of all reminders due in [from, to), in due order.
	Occurrences(ctx context.Context, from, to, now time.Time) ([]models.ReminderOccurrence, error)
	// Overdue returns the occurrences past due and not done at now, in due order. It includes ones
	// snoozed past their due time, so they stay listed; callers counting alerts check Status.
	Overdue(ctx context.Context, now time.Time) ([]models.ReminderOccurrence, error)
	// Current returns the occurrence to act on when none is named: the overdue one, else the next one not done.
	Current(ctx context.Context, id int, now time.Time) (*models.ReminderOccurrence, error)
	SetDone(ctx context.Context, ref models.OccurrenceRef, done bool, at time.Time) (*models.ReminderOccurrence, error)
	// Snooze moves the occurrence's alert to until; a zero until clears the snooze.
	Snooze(ctx context.Context, ref models.OccurrenceRef, until time.Time) (*models.ReminderOccurrence, error)
	// Skip drops one occurrence of a repeating reminder; for a one-off reminder it removes the reminder.
	Skip(ctx context.Context, ref models.OccurrenceRef) error
	// Track starts an activity from the occurrence and marks it done.
	Track(ctx context.Context, ref models.OccurrenceRef, req models.TrackReminderRequest) (*models.Activity, error)
}

// ReminderRepository stores reminders.
type ReminderRepository interface {
	List(ctx context.Context) ([]models.Reminder, error)
	// Save inserts a reminder with ID 0 under a new ID, or replaces the one with its ID.
	Save(ctx context.Context, reminder models.Reminder) (*models.Reminder, error)
	Remove(ctx context.Context, id int) error
}
