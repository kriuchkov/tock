package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// reminderNotifyGrace is how late a notification still goes out, e.g. after the Mac wakes up;
// older ones are left to the overdue badge and lists.
const reminderNotifyGrace = 15 * time.Minute

// reminderNotification is one banner.
type reminderNotification struct {
	Title    string
	Subtitle string
	Body     string
}

// reminderNotifier decides which notifications are due. It remembers what it sent while the
// moment is within the grace window, so each one goes out once.
type reminderNotifier struct {
	loc  *localization.Localizer
	tf   *timeutil.Formatter
	sent map[string]time.Time
}

func newReminderNotifier(loc *localization.Localizer, tf *timeutil.Formatter) *reminderNotifier {
	return &reminderNotifier{loc: loc, tf: tf, sent: make(map[string]time.Time)}
}

// notifyMoment is when a banner for an occurrence goes out; snooze marks the one a snooze adds.
type notifyMoment struct {
	at     time.Time
	snooze bool
}

// notifyMoments are the banners of an occurrence in time order: the lead time before a timed one
// and its due time, the morning for an all-day one, plus the end of a snooze.
func notifyMoments(occurrence models.ReminderOccurrence) []notifyMoment {
	var moments []notifyMoment
	switch {
	case occurrence.Reminder.AllDay:
		moments = []notifyMoment{{at: occurrence.NotifyAt()}}
	case occurrence.Reminder.Notify > 0:
		moments = []notifyMoment{{at: occurrence.NotifyAt()}, {at: occurrence.Due}}
	default:
		moments = []notifyMoment{{at: occurrence.Due}}
	}
	if until := occurrence.State.SnoozedUntil; until != nil {
		moments = append(moments, notifyMoment{at: *until, snooze: true})
		sort.SliceStable(moments, func(i, j int) bool { return moments[i].at.Before(moments[j].at) })
	}
	return moments
}

// due returns the notifications that came due by now and were not sent yet. When several
// moments of one occurrence are due at once only the latest goes out. A missed occurrence only
// notifies when it was snoozed.
func (n *reminderNotifier) due(occurrences []models.ReminderOccurrence, now time.Time) []reminderNotification {
	for key, at := range n.sent {
		if now.Sub(at) >= reminderNotifyGrace {
			delete(n.sent, key)
		}
	}

	var out []reminderNotification
	for _, occurrence := range occurrences {
		if occurrence.State.DoneAt != nil || (occurrence.Missed && occurrence.State.SnoozedUntil == nil) {
			continue
		}

		var latest *notifyMoment
		for _, moment := range notifyMoments(occurrence) {
			if moment.at.After(now) || now.Sub(moment.at) >= reminderNotifyGrace {
				continue
			}
			key := fmt.Sprintf("%d@%s@%d", occurrence.Reminder.ID, occurrence.Key(), moment.at.Unix())
			if _, sent := n.sent[key]; sent {
				continue
			}
			n.sent[key] = moment.at
			current := moment
			latest = &current
		}
		if latest != nil {
			out = append(out, n.notification(occurrence, *latest, now))
		}
	}
	return out
}

// notification builds the banner: the title, when it is due, then time, project and repeat.
func (n *reminderNotifier) notification(occurrence models.ReminderOccurrence, moment notifyMoment, now time.Time) reminderNotification {
	reminder := occurrence.Reminder

	subtitle := n.loc.Text("remind.notify.due_now")
	switch {
	case reminder.AllDay && !moment.snooze:
		subtitle = n.loc.Text("remind.status.today")
	case !reminder.AllDay && moment.at.Before(occurrence.Due):
		subtitle = n.loc.Format("remind.status.in", formatSpan(occurrence.Due.Sub(now)))
	}

	parts := []string{formatReminderClock(n.loc, n.tf, occurrence.Due, reminder.AllDay)}
	if reminder.Project != "" {
		parts = append(parts, reminder.Project)
	}
	if repeat := formatRepeat(n.loc, reminder); repeat != "" {
		parts = append(parts, n.loc.Format("remind.repeats", repeat))
	}
	return reminderNotification{Title: reminder.Title, Subtitle: subtitle, Body: strings.Join(parts, " · ")}
}
