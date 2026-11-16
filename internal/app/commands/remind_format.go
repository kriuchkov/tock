package commands

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

const (
	hoursPerDay = 24
	// countdownHorizon is how far ahead a pending occurrence shows "in 3h05m".
	countdownHorizon = hoursPerDay * time.Hour
)

// reminderLabel is "Project | Title", or the title alone when there is no project.
func reminderLabel(reminder models.Reminder) string {
	if reminder.Project == "" {
		return reminder.Title
	}
	return reminder.Project + " | " + reminder.Title
}

func localizedWeekdayAbbr(loc *localization.Localizer, weekday time.Weekday) string {
	return loc.Text("date.weekday_abbr." + strings.ToLower(weekday.String()[:3]))
}

// formatReminderDay formats a day as "Mon 16 Nov", adding the year when it is not now's.
func formatReminderDay(loc *localization.Localizer, day, now time.Time) string {
	text := fmt.Sprintf("%s %d %s", localizedWeekdayAbbr(loc, day.Weekday()), day.Day(), localizedMonthShortName(loc, day.Month()))
	if day.Year() != now.Year() {
		text += " " + strconv.Itoa(day.Year())
	}
	return text
}

// formatReminderDate formats a date as "31 Dec 2026".
func formatReminderDate(loc *localization.Localizer, day time.Time) string {
	return fmt.Sprintf("%d %s %d", day.Day(), localizedMonthShortName(loc, day.Month()), day.Year())
}

// formatReminderClock is the occurrence's time of day, or "all day".
func formatReminderClock(loc *localization.Localizer, tf *timeutil.Formatter, due time.Time, allDay bool) string {
	if allDay {
		return loc.Text("remind.all_day")
	}
	return due.Format(tf.GetDisplayFormat())
}

// formatReminderDue formats an occurrence for a table: "Mon 16 Nov 17:30", or "Mon 16 Nov" when all day.
func formatReminderDue(loc *localization.Localizer, tf *timeutil.Formatter, due time.Time, allDay bool, now time.Time) string {
	day := formatReminderDay(loc, due, now)
	if allDay {
		return day
	}
	return day + " " + due.Format(tf.GetDisplayFormat())
}

// formatReminderWhen formats an occurrence as "Mon 16 Nov 17:30", or "Mon 16 Nov (all day)".
func formatReminderWhen(loc *localization.Localizer, tf *timeutil.Formatter, due time.Time, allDay bool, now time.Time) string {
	day := formatReminderDay(loc, due, now)
	if allDay {
		return loc.Format("remind.when_all_day", day)
	}
	return day + " " + due.Format(tf.GetDisplayFormat())
}

// formatRepeat describes how a reminder repeats, e.g. "weekly on Mon" or "monthly on last Fri";
// empty for a one-off reminder.
func formatRepeat(loc *localization.Localizer, reminder models.Reminder) string {
	rep := reminder.Repeat
	interval := max(rep.Interval, 1)

	switch rep.Freq {
	case models.RepeatNone:
		return ""
	case models.RepeatDaily:
		if interval > 1 {
			return loc.Format("remind.repeat.every_days", interval)
		}
		return loc.Text("remind.repeat.daily")
	case models.RepeatWeekdays:
		return loc.Text("remind.repeat.weekdays")
	case models.RepeatWeekly:
		days := make([]string, 0, len(reminder.WeeklyDays()))
		for _, wd := range reminder.WeeklyDays() {
			days = append(days, localizedWeekdayAbbr(loc, wd))
		}
		return formatRepeatInterval(loc, "weekly", interval, strings.Join(days, ", "))
	case models.RepeatMonthly:
		return formatRepeatInterval(loc, "monthly", interval, formatMonthlyOn(loc, reminder))
	case models.RepeatYearly:
		first := reminder.Due.In(time.Local)
		on := fmt.Sprintf("%d %s", first.Day(), localizedMonthShortName(loc, first.Month()))
		return formatRepeatInterval(loc, "yearly", interval, on)
	}
	return rep.Spec()
}

func formatRepeatInterval(loc *localization.Localizer, freq string, interval int, on string) string {
	if interval > 1 {
		return loc.Format("remind.repeat.every_"+freq+"_on", interval, on)
	}
	return loc.Format("remind.repeat."+freq+"_on", on)
}

func formatMonthlyOn(loc *localization.Localizer, reminder models.Reminder) string {
	rep := reminder.Repeat
	switch {
	case rep.Week == models.WeekLast:
		return loc.Text("remind.repeat.ordinal.last") + " " + localizedWeekdayAbbr(loc, rep.Weekday)
	case rep.Week > 0:
		return loc.Text("remind.repeat.ordinal."+strconv.Itoa(rep.Week)) + " " + localizedWeekdayAbbr(loc, rep.Weekday)
	case rep.MonthDay == models.MonthDayLast:
		return loc.Text("remind.repeat.last_day")
	case rep.MonthDay > 0:
		return strconv.Itoa(rep.MonthDay)
	}
	return strconv.Itoa(reminder.Due.In(time.Local).Day())
}

// formatRepeatBounds is " until 31 Dec 2026" or ", 5 times" when the repeat ends; else empty.
func formatRepeatBounds(loc *localization.Localizer, rep models.Repeat) string {
	switch {
	case rep.Until != nil:
		return " " + loc.Format("remind.repeat.until", formatReminderDate(loc, *rep.Until))
	case rep.Count > 0:
		return loc.Format("remind.repeat.count", rep.Count)
	}
	return ""
}

// formatReminderSchedule is "Mon 16 Nov 17:30, repeats weekly on Mo until 31 Dec 2026".
func formatReminderSchedule(loc *localization.Localizer, tf *timeutil.Formatter, reminder models.Reminder, now time.Time) string {
	text := formatReminderWhen(loc, tf, reminder.Due, reminder.AllDay, now)
	if repeat := formatRepeat(loc, reminder); repeat != "" {
		text += ", " + loc.Format("remind.repeats", repeat) + formatRepeatBounds(loc, reminder.Repeat)
	}
	return text
}

// formatReminderSummary is a sentence for the form preview: "Repeats weekly on Mo at 17:30 until 31 Dec 2026".
func formatReminderSummary(loc *localization.Localizer, tf *timeutil.Formatter, reminder models.Reminder, now time.Time) string {
	repeat := formatRepeat(loc, reminder)
	if repeat == "" {
		return loc.Format("remind.summary.once", formatReminderWhen(loc, tf, reminder.Due, reminder.AllDay, now))
	}

	text := loc.Format("remind.summary.repeats", repeat)
	if !reminder.AllDay {
		text += " " + loc.Format("remind.summary.at", reminder.Due.Format(tf.GetDisplayFormat()))
	}
	return text + formatRepeatBounds(loc, reminder.Repeat)
}

// reminderStatusGlyph is the one-character status marker used in lists.
func reminderStatusGlyph(status models.ReminderStatus) string {
	switch status {
	case models.ReminderDone:
		return "✓"
	case models.ReminderOverdue:
		return "!"
	case models.ReminderSnoozed:
		return "z"
	case models.ReminderMissed:
		return "–"
	case models.ReminderPending:
	}
	return "○"
}

// formatReminderStatus describes where an occurrence stands: "tracked 15m", "overdue 1h25m",
// "in 3h05m", "today". tracked is the duration of the activity tracked from it, if known.
func formatReminderStatus(
	loc *localization.Localizer,
	tf *timeutil.Formatter,
	occurrence models.ReminderOccurrence,
	now time.Time,
	tracked time.Duration,
) string {
	switch occurrence.Status(now) {
	case models.ReminderDone:
		if occurrence.State.ActivityStart == nil {
			return loc.Text("remind.status.done")
		}
		if tracked > 0 {
			return loc.Format("remind.status.tracked_for", formatSpan(tracked))
		}
		return loc.Text("remind.status.tracked")
	case models.ReminderMissed:
		return loc.Text("remind.status.missed")
	case models.ReminderSnoozed:
		return loc.Format("remind.status.snoozed", occurrence.State.SnoozedUntil.Format(tf.GetDisplayFormat()))
	case models.ReminderOverdue:
		late := now.Sub(occurrence.DueBy())
		if late < time.Minute {
			return loc.Text("remind.status.due_now")
		}
		return loc.Format("remind.status.overdue", formatSpan(late))
	case models.ReminderPending:
	}

	if occurrence.Reminder.AllDay {
		if sameDay(occurrence.Due, now) {
			return loc.Text("remind.status.today")
		}
		return ""
	}
	if ahead := occurrence.Due.Sub(now); ahead < countdownHorizon {
		return loc.Format("remind.status.in", formatSpan(ahead))
	}
	return ""
}

// formatReminderStatusLine is the status with its glyph ("! overdue 1h25m"); a pending
// occurrence shows the plain countdown.
func formatReminderStatusLine(
	loc *localization.Localizer,
	tf *timeutil.Formatter,
	occurrence models.ReminderOccurrence,
	now time.Time,
	tracked time.Duration,
) string {
	status := occurrence.Status(now)
	text := formatReminderStatus(loc, tf, occurrence, now, tracked)
	if status == models.ReminderPending || text == "" {
		return text
	}
	return reminderStatusGlyph(status) + " " + text
}

// formatSpan formats a duration compactly: "2d", "3h05m", "45m".
func formatSpan(d time.Duration) string {
	d = max(d.Round(time.Minute), 0)
	if days := int(d / (hoursPerDay * time.Hour)); days >= 1 {
		return fmt.Sprintf("%dd", days)
	}
	hours := int(d / time.Hour)
	minutes := int(d/time.Minute) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// trackedDuration finds the activity tracked from the occurrence among activities.
func trackedDuration(occurrence models.ReminderOccurrence, activities []models.Activity) time.Duration {
	if occurrence.State.ActivityStart == nil {
		return 0
	}
	// Backends keep start times to the minute or second, so the stored start is matched by minute.
	start := occurrence.State.ActivityStart.Truncate(time.Minute)
	for _, activity := range activities {
		if activity.StartTime.Truncate(time.Minute).Equal(start) {
			return activity.Duration()
		}
	}
	return 0
}

// marksCalendarDay reports whether the reminder dots calendar days: daily and weekday repeats
// do not, or every day would be marked.
func marksCalendarDay(reminder models.Reminder) bool {
	switch reminder.Repeat.Freq {
	case models.RepeatDaily:
		return reminder.Repeat.Interval > 1
	case models.RepeatWeekdays:
		return false
	case models.RepeatNone, models.RepeatWeekly, models.RepeatMonthly, models.RepeatYearly:
	}
	return true
}
