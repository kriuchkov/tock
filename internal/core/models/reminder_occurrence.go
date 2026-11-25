package models

import (
	"sort"
	"time"
)

// Occurrences returns the reminder's occurrences due in [from, to), oldest first, with their
// recorded state. Skipped occurrences are left out. A past occurrence that was not done before a
// newer one came due at now is marked missed, so only the latest one of a repeating reminder is
// ever overdue.
func (r Reminder) Occurrences(from, to, now time.Time) []ReminderOccurrence {
	latest := r.latestDue(now)

	var occurrences []ReminderOccurrence
	r.eachOccurrence(to, func(due time.Time) bool {
		if due.Before(from) {
			return true
		}
		state := r.State(due)
		if state.Skipped {
			return true
		}
		occurrences = append(occurrences, ReminderOccurrence{
			Reminder: r,
			Due:      due,
			State:    state,
			Missed:   state.DoneAt == nil && !latest.IsZero() && due.Before(latest),
		})
		return true
	})
	return occurrences
}

// FirstDue returns the due time of the first occurrence, which is later than Due when Due does not
// fall on the repeat rule; ok is false when the reminder never comes due.
func (r Reminder) FirstDue() (time.Time, bool) {
	var first time.Time
	found := false
	r.eachOccurrence(r.firstDay().AddDate(0, 0, maxOccurrenceScanDays), func(due time.Time) bool {
		first, found = due, true
		return false
	})
	return first, found
}

// OccurrenceOn returns the occurrence falling on day, if any.
func (r Reminder) OccurrenceOn(day, now time.Time) (ReminderOccurrence, bool) {
	occurrences := r.Occurrences(dayStart(day), nextDayStart(day), now)
	if len(occurrences) == 0 {
		return ReminderOccurrence{}, false
	}
	return occurrences[0], true
}

// NextAfter returns the first occurrence due after t that is not done.
func (r Reminder) NextAfter(t, now time.Time) (ReminderOccurrence, bool) {
	var next ReminderOccurrence
	found := false
	r.eachOccurrence(t.AddDate(0, 0, maxOccurrenceScanDays), func(due time.Time) bool {
		if !due.After(t) {
			return true
		}
		state := r.State(due)
		if state.Skipped || state.DoneAt != nil {
			return true
		}
		next = ReminderOccurrence{Reminder: r, Due: due, State: state}
		found = true
		return false
	})
	if found {
		next.Missed = next.Due.Before(r.latestDue(now))
	}
	return next, found
}

// Current returns the occurrence a command acts on when none is named: the overdue one if there
// is one, or the one from an earlier day still waiting on its snooze, else the first one from
// today on that is not done.
func (r Reminder) Current(now time.Time) (ReminderOccurrence, bool) {
	today := dayStart(now)
	tomorrow := nextDayStart(now)

	var upcoming *ReminderOccurrence
	for _, occurrence := range r.Occurrences(r.firstDay(), tomorrow, now) {
		earlier := occurrence.Due.Before(today)
		switch occurrence.Status(now) {
		case ReminderOverdue:
			return occurrence, true
		case ReminderSnoozed:
			if earlier && !occurrence.Missed {
				return occurrence, true
			}
			fallthrough
		case ReminderPending:
			if upcoming == nil && !earlier {
				candidate := occurrence
				upcoming = &candidate
			}
		case ReminderDone, ReminderMissed:
		}
	}
	if upcoming != nil {
		return *upcoming, true
	}
	return r.NextAfter(tomorrow.Add(-time.Nanosecond), now)
}

// Last returns the newest occurrence due at or before now with its state, done or not; for a
// finished reminder it is where the reminder ended.
func (r Reminder) Last(now time.Time) (ReminderOccurrence, bool) {
	occurrences := r.Occurrences(r.firstDay(), now.Add(time.Nanosecond), now)
	if len(occurrences) == 0 {
		return ReminderOccurrence{}, false
	}
	return occurrences[len(occurrences)-1], true
}

// latestDue returns the due time of the newest occurrence that is not skipped and became overdue
// at or before now (an all-day one at the end of its day), or the zero time when none has.
func (r Reminder) latestDue(now time.Time) time.Time {
	var latest time.Time
	r.eachOccurrence(nextDayStart(now), func(due time.Time) bool {
		if r.scheduledBy(due).After(now) {
			return false
		}
		if !r.State(due).Skipped {
			latest = due
		}
		return true
	})
	return latest
}

// SortOccurrences orders occurrences by due time; all-day ones lead their day, ties go by reminder ID.
func SortOccurrences(occurrences []ReminderOccurrence) {
	sort.SliceStable(occurrences, func(i, j int) bool {
		a, b := occurrences[i], occurrences[j]
		if !a.Due.Equal(b.Due) {
			return a.Due.Before(b.Due)
		}
		if a.Reminder.AllDay != b.Reminder.AllDay {
			return a.Reminder.AllDay
		}
		return a.Reminder.ID < b.Reminder.ID
	})
}
