package commands

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// Columns of a reminder row.
const (
	reminderClockW   = 10 // "all day", "13:00", "Sat 14", "03:04 PM"
	reminderTitleW   = 18 // the title when the notes have a column
	reminderTitleMin = 12
	reminderRepeatW  = 20
	reminderProjectW = 15
	reminderNotifyW  = 13
	reminderStatusW  = 16
	reminderNotesMin = 12
)

// reminderRows draws reminder occurrences as rows and focus lines, for the calendar and the
// reminders tab alike.
type reminderRows struct {
	ui  chromeStyles
	loc *localization.Localizer
	tf  *timeutil.Formatter
	// project styles a project name; the calendar gives it its tag colors.
	project func(name string) lipgloss.Style
	// tags draws tags; the calendar gives them their tag colors.
	tags func(tags []string) string
	// next remembers the next occurrence the focus box shows; nil looks it up each time.
	next *nextMemo
}

// nextMemo remembers the next occurrence of the reminder in the focus box: finding it scans
// the reminder's days from its first one, too slow to do on every redraw. A screen forgets it
// when it reloads its reminders.
type nextMemo struct {
	id   int
	due  time.Time
	next models.ReminderOccurrence
	ok   bool
	set  bool
}

// after is the occurrence of a repeating reminder after occurrence that is not done or skipped.
func (n *nextMemo) after(occurrence models.ReminderOccurrence, now time.Time) (models.ReminderOccurrence, bool) {
	reminder := occurrence.Reminder
	if n == nil {
		return reminder.NextAfter(occurrence.Due, now)
	}
	if !n.set || n.id != reminder.ID || !n.due.Equal(occurrence.Due) {
		n.next, n.ok = reminder.NextAfter(occurrence.Due, now)
		n.id, n.due, n.set = reminder.ID, occurrence.Due, true
	}
	return n.next, n.ok
}

func newReminderRows(ui chromeStyles, loc *localization.Localizer, tf *timeutil.Formatter) reminderRows {
	return reminderRows{
		ui:      ui,
		loc:     loc,
		tf:      tf,
		project: func(string) lipgloss.Style { return ui.text },
		tags: func(tags []string) string {
			return ui.tag.Render("#" + strings.Join(tags, " #"))
		},
	}
}

// reminderRowOpts are the columns a screen gives a row beyond time, title and status.
type reminderRowOpts struct {
	// day is the day the row is listed under: occurrences due on it show their time,
	// others their day, "Sun 15".
	day time.Time
	// notes and notify ask for those columns; they show only when the row is wide enough.
	notes  bool
	notify bool
}

// styles are the glyph and the styles of a row's glyph, title and status for a status:
// ◆ in the accent when overdue, ✓ and muted text when done.
func (r reminderRows) styles(status models.ReminderStatus) (string, lipgloss.Style, lipgloss.Style, lipgloss.Style) {
	switch status {
	case models.ReminderOverdue:
		return glyphOverdue, r.ui.accent, r.ui.text, r.ui.accent
	case models.ReminderDone:
		return reminderStatusGlyph(status), r.ui.ok, r.ui.muted, r.ui.muted
	case models.ReminderMissed:
		return reminderStatusGlyph(status), r.ui.faint, r.ui.muted, r.ui.muted
	case models.ReminderSnoozed:
		return reminderStatusGlyph(status), r.ui.accent, r.ui.text, r.ui.accent
	case models.ReminderPending:
	}
	return reminderStatusGlyph(status), r.ui.reminder, r.ui.text, r.ui.muted
}

func (r reminderRows) projectStyle(name string, status models.ReminderStatus) lipgloss.Style {
	if status == models.ReminderDone || status == models.ReminderMissed {
		return r.ui.muted
	}
	return r.project(name)
}

// row is a reminder in width columns: its time, title, repeat, project, notification and notes
// as far as they fit, and its status at the right end.
func (r reminderRows) row(
	occurrence models.ReminderOccurrence,
	tracked time.Duration,
	now time.Time,
	selected bool,
	width int,
	opts reminderRowOpts,
) string {
	reminder := occurrence.Reminder
	status := occurrence.Status(now)
	glyph, glyphStyle, titleStyle, statusStyle := r.styles(status)

	clock := formatReminderClock(r.loc, r.tf, occurrence.Due, reminder.AllDay)
	if !sameDay(occurrence.Due, opts.day) {
		clock = localizedWeekdayAbbr(r.loc, occurrence.Due.Weekday()) + " " + strconv.Itoa(occurrence.Due.Day())
	}

	fixed := rowGutterW + reminderClockW + reminderRepeatW + reminderProjectW + reminderStatusW
	meta := width >= fixed+reminderTitleMin
	// The notification column comes with the notes, when both fit.
	notesMin := fixed + reminderTitleW + reminderNotesMin
	if opts.notify {
		notesMin += reminderNotifyW
	}
	notes := opts.notes && meta && width >= notesMin
	notify := opts.notify && notes

	title := rowCell{text: reminder.Title, style: titleStyle}
	if notes {
		title.width = reminderTitleW
	}
	cells := make([]rowCell, 0, rowCellsCap)
	cells = append(cells, rowCell{text: clock, style: titleStyle, width: reminderClockW}, title)
	if meta {
		repeat := ""
		if reminder.IsRepeating() {
			repeat = glyphRepeat + " " + formatRepeat(r.loc, reminder)
		}
		cells = append(cells,
			rowCell{text: repeat, style: r.ui.muted, width: reminderRepeatW},
			rowCell{text: reminder.Project, style: r.projectStyle(reminder.Project, status), width: reminderProjectW},
		)
	}
	if notify {
		before := ""
		if reminder.Notify > 0 {
			before = r.loc.Format("remind.notify_before", formatNotifyValue(reminder.Notify))
		}
		cells = append(cells, rowCell{text: before, style: r.ui.muted, width: reminderNotifyW})
	}
	if notes {
		cells = append(cells, rowCell{text: oneLine(reminder.Notes), style: r.ui.muted})
	}
	statusText := formatReminderStatus(r.loc, r.tf, occurrence, now, tracked)
	cells = append(cells, rowCell{text: statusText, style: statusStyle, width: reminderStatusW, right: true})
	return r.ui.renderRow(glyph, glyphStyle, cells, selected, width)
}

// focus is the focus box of a selected reminder: what it is and when, then its notes, or its
// status without notes.
func (r reminderRows) focus(label string, occurrence models.ReminderOccurrence, now time.Time) []string {
	reminder := occurrence.Reminder
	parts := []string{formatReminderWhen(r.loc, r.tf, occurrence.Due, reminder.AllDay, now)}
	if reminder.IsRepeating() {
		parts = append(parts, glyphRepeat+" "+formatRepeat(r.loc, reminder)+formatRepeatBounds(r.loc, reminder.Repeat))
	}
	if reminder.Project != "" {
		parts = append(parts, reminder.Project)
	}
	if reminder.Notify > 0 {
		parts = append(parts, r.loc.Format("remind.notify_before", formatNotifyValue(reminder.Notify)))
	}
	if reminder.IsRepeating() {
		if next, ok := r.next.after(occurrence, now); ok {
			parts = append(parts, r.loc.Format("remind.tui.focus.next",
				formatReminderDue(r.loc, r.tf, next.Due, reminder.AllDay, now)))
		}
	}
	id := " #" + strconv.Itoa(reminder.ID)
	head := label + r.ui.accentBold.Render(reminder.Title) + r.ui.muted.Render(id+" · "+strings.Join(parts, " · "))
	if len(reminder.Tags) > 0 {
		head += r.ui.muted.Render(" · ") + r.tags(reminder.Tags)
	}
	body := r.ui.muted.Render(formatReminderStatus(r.loc, r.tf, occurrence, now, 0))
	if reminder.Notes != "" {
		body = r.ui.text.Render(oneLine(reminder.Notes))
	}
	return []string{head, body}
}
