package models

import (
	"encoding/json"
	"maps"
	"time"
)

// AllDayNotifyHour is the local hour an all-day reminder notifies at, before its lead time.
const AllDayNotifyHour = 9

// MaxNotifyLead is the longest lead time a reminder can notify ahead of its due time.
const MaxNotifyLead = 7 * 24 * time.Hour

// Reminder is something to do on a day, optionally at a time, once or repeatedly.
type Reminder struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Project string `json:"project,omitempty"`
	// Due is the first occurrence. Its time of day is ignored for all-day reminders.
	Due    time.Time `json:"due"`
	AllDay bool      `json:"all_day,omitempty"`
	Repeat Repeat    `json:"-"`
	// Notify is how long before an occurrence a notification goes out.
	Notify  time.Duration `json:"-"`
	Tags    []string      `json:"tags,omitempty"`
	Notes   string        `json:"notes,omitempty"`
	Created time.Time     `json:"created"`
	// States holds what happened to single occurrences, keyed by OccurrenceKey.
	States map[string]OccurrenceState `json:"states,omitempty"`
}

// OccurrenceState is what happened to one occurrence of a reminder.
type OccurrenceState struct {
	DoneAt       *time.Time `json:"done_at,omitempty"`
	SnoozedUntil *time.Time `json:"snoozed_until,omitempty"`
	// ActivityStart is the start of the activity tracked from this occurrence.
	ActivityStart *time.Time `json:"activity_start,omitempty"`
	// Skipped removes this single occurrence from a repeating reminder.
	Skipped bool `json:"skipped,omitempty"`
}

// IsZero reports whether nothing was recorded for the occurrence.
func (s OccurrenceState) IsZero() bool {
	return s.DoneAt == nil && s.SnoozedUntil == nil && s.ActivityStart == nil && !s.Skipped
}

// IsRepeating reports whether the reminder comes back after its first occurrence.
func (r Reminder) IsRepeating() bool {
	return r.Repeat.Freq != RepeatNone
}

// State returns the recorded state of the occurrence on day.
func (r Reminder) State(day time.Time) OccurrenceState {
	return r.States[OccurrenceKey(day)]
}

// WithState returns a copy of the reminder with the state of the occurrence on day replaced.
func (r Reminder) WithState(day time.Time, state OccurrenceState) Reminder {
	states := make(map[string]OccurrenceState, len(r.States)+1)
	maps.Copy(states, r.States)

	key := OccurrenceKey(day)
	if state.IsZero() {
		delete(states, key)
	} else {
		states[key] = state
	}
	if len(states) == 0 {
		states = nil
	}

	updated := r
	updated.States = states
	return updated
}

func (r Reminder) MarshalJSON() ([]byte, error) {
	type alias Reminder

	out := struct {
		alias

		Every  string `json:"every,omitempty"`
		Until  string `json:"until,omitempty"`
		Count  int    `json:"count,omitempty"`
		Notify string `json:"notify,omitempty"`
	}{
		alias: alias(r),
		Every: r.Repeat.Spec(),
		Count: r.Repeat.Count,
	}
	if r.Repeat.Until != nil {
		out.Until = r.Repeat.Until.Format(time.DateOnly)
	}
	if r.Notify > 0 {
		out.Notify = r.Notify.String()
	}
	return json.Marshal(out)
}

// OccurrenceKey identifies an occurrence by the day it falls on (YYYY-MM-DD).
func OccurrenceKey(day time.Time) string {
	return day.In(time.Local).Format(time.DateOnly)
}

// OccurrenceRef points at one occurrence of a reminder.
type OccurrenceRef struct {
	ID  int
	Day time.Time
}

// ReminderStatus is where an occurrence stands at a given moment.
type ReminderStatus string

const (
	ReminderPending ReminderStatus = "pending"
	ReminderSnoozed ReminderStatus = "snoozed"
	ReminderOverdue ReminderStatus = "overdue"
	ReminderMissed  ReminderStatus = "missed"
	ReminderDone    ReminderStatus = "done"
)

// ReminderOccurrence is one dated instance of a reminder.
type ReminderOccurrence struct {
	Reminder Reminder `json:"reminder"`
	// Due is when the occurrence is scheduled; the start of its day for all-day reminders.
	Due   time.Time       `json:"due"`
	State OccurrenceState `json:"state"`
	// Missed marks a past occurrence that was not done before a newer one came due.
	Missed bool `json:"missed,omitempty"`
}

// Ref points at this occurrence.
func (o ReminderOccurrence) Ref() OccurrenceRef {
	return OccurrenceRef{ID: o.Reminder.ID, Day: o.Due}
}

// Key identifies the occurrence within its reminder.
func (o ReminderOccurrence) Key() string {
	return OccurrenceKey(o.Due)
}

// DueBy is the moment the occurrence becomes overdue: the end of its day for an all-day
// reminder, else its due time. A snooze can only push it later, never earlier.
func (o ReminderOccurrence) DueBy() time.Time {
	dueBy := o.Reminder.scheduledBy(o.Due)
	if until := o.State.SnoozedUntil; until != nil && until.After(dueBy) {
		return *until
	}
	return dueBy
}

// NotifyAt is when the scheduled notification goes out: the lead time before the due time, or
// before AllDayNotifyHour for an all-day reminder. A snooze adds its own notification on top.
func (o ReminderOccurrence) NotifyAt() time.Time {
	at := o.Due
	if o.Reminder.AllDay {
		day := dayStart(o.Due)
		at = time.Date(day.Year(), day.Month(), day.Day(), AllDayNotifyHour, 0, 0, 0, day.Location())
	}
	return at.Add(-o.Reminder.Notify)
}

// Status reports where the occurrence stands at now. A running snooze wins over missed, so
// snoozing an older occurrence shows until the snooze ends.
func (o ReminderOccurrence) Status(now time.Time) ReminderStatus {
	switch {
	case o.State.DoneAt != nil:
		return ReminderDone
	case o.State.SnoozedUntil != nil && now.Before(*o.State.SnoozedUntil):
		return ReminderSnoozed
	case o.Missed:
		return ReminderMissed
	case !now.Before(o.DueBy()):
		return ReminderOverdue
	}
	return ReminderPending
}

// PastDue reports whether the occurrence's scheduled time has passed at now, snoozed or not.
func (o ReminderOccurrence) PastDue(now time.Time) bool {
	return !now.Before(o.Reminder.scheduledBy(o.Due))
}

// scheduledBy is when an occurrence due at due becomes overdue, ignoring any snooze.
func (r Reminder) scheduledBy(due time.Time) time.Time {
	if r.AllDay {
		return dayStart(due).AddDate(0, 0, 1)
	}
	return due
}

// AddReminderRequest describes a new reminder.
type AddReminderRequest struct {
	Title   string
	Project string
	Due     time.Time
	AllDay  bool
	Repeat  Repeat
	Notify  time.Duration
	Tags    []string
	Notes   string
}

// UpdateReminderRequest describes a partial update of a reminder. Nil fields keep the current value.
type UpdateReminderRequest struct {
	Title   *string
	Project *string
	Due     *time.Time
	AllDay  *bool
	Repeat  *Repeat
	Notify  *time.Duration
	Tags    *[]string
	Notes   *string
}

// IsEmpty reports whether the request carries no change at all.
func (r UpdateReminderRequest) IsEmpty() bool {
	return r.Title == nil && r.Project == nil && r.Due == nil && r.AllDay == nil &&
		r.Repeat == nil && r.Notify == nil && r.Tags == nil && r.Notes == nil
}

// Apply returns a copy of reminder with the requested fields replaced.
func (r UpdateReminderRequest) Apply(reminder Reminder) Reminder {
	updated := reminder
	if r.Title != nil {
		updated.Title = *r.Title
	}
	if r.Project != nil {
		updated.Project = *r.Project
	}
	if r.Due != nil {
		updated.Due = *r.Due
	}
	if r.AllDay != nil {
		updated.AllDay = *r.AllDay
	}
	if r.Repeat != nil {
		updated.Repeat = *r.Repeat
	}
	if r.Notify != nil {
		updated.Notify = *r.Notify
	}
	if r.Tags != nil {
		updated.Tags = append([]string(nil), (*r.Tags)...)
	}
	if r.Notes != nil {
		updated.Notes = *r.Notes
	}
	return updated
}

// TrackReminderRequest starts tracking an occurrence as an activity.
type TrackReminderRequest struct {
	StartTime time.Time
	// Project overrides the reminder's project; required when the reminder has none.
	Project string
}
