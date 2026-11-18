package models

import (
	"strconv"
	"time"
)

// OverdueOnly drops the occurrences Overdue returns because they are snoozed past their due time:
// they stay listed, but are not counted or shown as overdue.
func OverdueOnly(occurrences []ReminderOccurrence, now time.Time) []ReminderOccurrence {
	var overdue []ReminderOccurrence
	for _, occurrence := range occurrences {
		if occurrence.Status(now) == ReminderOverdue {
			overdue = append(overdue, occurrence)
		}
	}
	return overdue
}

// MergeOccurrences appends upcoming to overdue, dropping the ones already listed as overdue.
func MergeOccurrences(overdue, upcoming []ReminderOccurrence) []ReminderOccurrence {
	seen := make(map[string]bool, len(overdue))
	merged := make([]ReminderOccurrence, 0, len(overdue)+len(upcoming))
	for _, occurrence := range overdue {
		seen[occurrence.ID()] = true
		merged = append(merged, occurrence)
	}
	for _, occurrence := range upcoming {
		if !seen[occurrence.ID()] {
			merged = append(merged, occurrence)
		}
	}
	return merged
}

// ID identifies the occurrence among all reminders, as "<reminder id>@<YYYY-MM-DD>".
func (o ReminderOccurrence) ID() string {
	return strconv.Itoa(o.Reminder.ID) + "@" + o.Key()
}
