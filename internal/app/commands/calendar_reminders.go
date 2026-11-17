package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"
)

const (
	// calendarUpcomingDays is how many days Upcoming covers, today included, unless
	// calendar.upcoming_days says otherwise; calendarUpcomingMaxDays is the most it takes.
	calendarUpcomingDays    = 7
	calendarUpcomingMaxDays = 31
	// calendarUpcomingMinLines is the least room Upcoming needs: its title and a line.
	calendarUpcomingMinLines = 2
	// Columns of Upcoming: the day ("thu 19") and today's times ("05:30 PM"), a space
	// narrower each below upcomingNarrowW; and the least room a title keeps before the
	// countdown is left out.
	upcomingDayW      = 8
	upcomingClockW    = 10
	upcomingNarrowW   = 36
	upcomingTitleMinW = 8
)

// calendarReminders is the calendar's reminder state: what is shown and the focused list.
type calendarReminders struct {
	svc    ports.ReminderResolver
	env    reminderFormEnv
	snooze time.Duration

	data reminderData
	next nextMemo

	form          *reminderForm
	pendingDelete *models.ReminderOccurrence
	status        string
	statusErr     bool
}

// reminderData is what the calendar loads with each month.
type reminderData struct {
	month    []models.ReminderOccurrence // the month on screen, with a margin
	upcoming []models.ReminderOccurrence // today and the next days
	overdue  []models.ReminderOccurrence
}

// withReminders turns on the reminder parts of the calendar.
func (m *calendarModel) withReminders(svc ports.ReminderResolver, snooze time.Duration) {
	m.rem.svc = svc
	m.rem.env = newReminderFormEnv(m.loc, m.timeFormat, m.theme)
	m.rem.snooze = snooze
}

func (m *calendarModel) remindersEnabled() bool {
	return m.rem.svc != nil
}

// upcomingDays is how many days Upcoming covers: calendar.upcoming_days, 7 when unset.
func (m *calendarModel) upcomingDays() int {
	if days := m.config.Calendar.UpcomingDays; days > 0 {
		return min(days, calendarUpcomingMaxDays)
	}
	return calendarUpcomingDays
}

// fetchReminders loads the reminders of [from, to) plus the upcoming and overdue ones.
func (m *calendarModel) fetchReminders(ctx context.Context, from, to time.Time) (reminderData, error) {
	if !m.remindersEnabled() {
		return reminderData{}, nil
	}
	now := reminderNow()

	month, err := m.rem.svc.Occurrences(ctx, from, to, now)
	if err != nil {
		return reminderData{}, errors.Wrap(err, "list reminders")
	}
	today, _ := timeutil.LocalDayBounds(now)
	upcoming, err := m.rem.svc.Occurrences(ctx, today, today.AddDate(0, 0, m.upcomingDays()), now)
	if err != nil {
		return reminderData{}, errors.Wrap(err, "list reminders")
	}
	overdue, err := m.rem.svc.Overdue(ctx, now)
	if err != nil {
		return reminderData{}, errors.Wrap(err, "list overdue reminders")
	}
	return reminderData{month: month, upcoming: upcoming, overdue: overdue}, nil
}

// dayReminders lists the reminders of the selected day; today also lists the overdue ones.
func (m *calendarModel) dayReminders() []models.ReminderOccurrence {
	var day []models.ReminderOccurrence
	for _, occurrence := range m.rem.data.month {
		if sameDay(occurrence.Due, m.currentDate) {
			day = append(day, occurrence)
		}
	}
	if sameDay(m.currentDate, reminderNow()) {
		return mergeOccurrences(m.rem.data.overdue, day)
	}
	return day
}

// calendarMark is the mark a day of the month grid gets for its reminders.
type calendarMark struct {
	glyph string
	style lipgloss.Style
}

// reminderMark marks a day with reminders: ◆ when one is overdue, • while some are to do,
// a faint • when all are done or missed.
func (m *calendarModel) reminderMark(date time.Time, now time.Time) (calendarMark, bool) {
	marked, pending := false, false
	for _, occurrence := range m.rem.data.month {
		if !sameDay(occurrence.Due, date) || !marksCalendarDay(occurrence.Reminder) {
			continue
		}
		marked = true
		switch occurrence.Status(now) {
		case models.ReminderOverdue:
			return calendarMark{glyph: glyphOverdue, style: m.ui.accent}, true
		case models.ReminderPending, models.ReminderSnoozed:
			pending = true
		case models.ReminderDone, models.ReminderMissed:
		}
	}
	switch {
	case !marked:
		return calendarMark{}, false
	case !pending:
		return calendarMark{glyph: glyphMark, style: m.ui.faint}, true
	}
	return calendarMark{glyph: glyphMark, style: m.ui.reminder}, true
}

func (m *calendarModel) reminderDeletePrompt() string {
	occurrence := m.rem.pendingDelete
	key := "remind.tui.delete_confirm"
	if occurrence.Reminder.IsRepeating() {
		key = "remind.tui.delete_confirm_repeating"
	}
	return m.loc.Format(key, occurrence.Reminder.ID, occurrence.Reminder.Title)
}

// handleReminderKey handles the reminder form, the delete prompt, "a" and the keys that act
// on the selected reminder; handled is false for keys the calendar should take.
func (m *calendarModel) handleReminderKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if !m.remindersEnabled() {
		return nil, false
	}
	switch {
	case m.rem.form != nil:
		return m.updateReminderForm(msg), true
	case m.rem.pendingDelete != nil:
		return m.confirmReminderDelete(msg), true
	case m.monthOpen:
		return nil, false
	}

	if msg.String() == "a" {
		day := time.Date(m.currentDate.Year(), m.currentDate.Month(), m.currentDate.Day(), 0, 0, 0, 0, time.Local)
		m.rem.form = newReminderFormFromRequest(m.timeFormat, models.AddReminderRequest{Due: day, AllDay: true})
		return nil, true
	}
	selected, ok := m.selectedReminder()
	if !ok {
		return nil, false
	}
	return m.handleSelectedReminderKey(msg, selected)
}

func (m *calendarModel) handleSelectedReminderKey(msg tea.KeyMsg, selected models.ReminderOccurrence) (tea.Cmd, bool) {
	switch msg.String() {
	case " ", keySpace:
		return m.reminderAction(func(ctx context.Context) (string, error) {
			return m.toggleReminderDone(ctx, selected)
		}), true
	case "s":
		return m.reminderAction(func(ctx context.Context) (string, error) {
			activity, err := m.rem.svc.Track(ctx, selected.Ref(), models.TrackReminderRequest{StartTime: reminderNow()})
			if err != nil {
				return "", err
			}
			return m.loc.Format("remind.tui.started", activity.Project, activity.Description), nil
		}), true
	case "z":
		return m.reminderAction(func(ctx context.Context) (string, error) {
			until := reminderNow().Add(m.rem.snooze)
			if _, err := m.rem.svc.Snooze(ctx, selected.Ref(), until); err != nil {
				return "", err
			}
			return m.loc.Format("remind.tui.snoozed", selected.Reminder.Title, until.Format(m.timeFormat.GetDisplayFormat())), nil
		}), true
	case "e", keyEnter:
		m.rem.form = newReminderFormFromReminder(m.timeFormat, selected.Reminder)
	case "x", keyDel:
		m.rem.pendingDelete = &selected
	default:
		return nil, false
	}
	m.updateViewportContent()
	return nil, true
}

func (m *calendarModel) toggleReminderDone(ctx context.Context, occurrence models.ReminderOccurrence) (string, error) {
	done := occurrence.State.DoneAt == nil
	if _, err := m.rem.svc.SetDone(ctx, occurrence.Ref(), done, reminderNow()); err != nil {
		return "", err
	}
	if done {
		return m.loc.Format("remind.tui.done", occurrence.Reminder.Title), nil
	}
	return m.loc.Format("remind.tui.undone", occurrence.Reminder.Title), nil
}

// reminderAction runs a change and reloads the month, showing its outcome under the reminders.
func (m *calendarModel) reminderAction(action func(ctx context.Context) (string, error)) tea.Cmd {
	status, err := action(context.Background())
	m.rem.status, m.rem.statusErr = status, err != nil
	if err != nil {
		m.rem.status = reminderErrorText(m.loc, err)
	}
	m.updateViewportContent()
	return m.fetchMonthData
}

// confirmReminderDelete removes the reminder on "y", only this occurrence on "o"; other keys cancel.
func (m *calendarModel) confirmReminderDelete(msg tea.KeyMsg) tea.Cmd {
	occurrence := *m.rem.pendingDelete
	m.rem.pendingDelete = nil

	switch msg.String() {
	case "y", "Y":
		return m.reminderAction(func(ctx context.Context) (string, error) {
			if err := m.rem.svc.Remove(ctx, occurrence.Reminder.ID); err != nil {
				return "", err
			}
			return m.loc.Format("remind.tui.removed", occurrence.Reminder.Title), nil
		})
	case "o", "O":
		if occurrence.Reminder.IsRepeating() {
			return m.reminderAction(func(ctx context.Context) (string, error) {
				if err := m.rem.svc.Skip(ctx, occurrence.Ref()); err != nil {
					return "", err
				}
				when := formatReminderDue(m.loc, m.timeFormat, occurrence.Due, occurrence.Reminder.AllDay, reminderNow())
				return m.loc.Format("remind.tui.skipped", when, occurrence.Reminder.Title), nil
			})
		}
	}
	m.updateViewportContent()
	return nil
}

func (m *calendarModel) updateReminderForm(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyCtrlC {
		return tea.Quit
	}
	switch m.rem.form.update(msg) {
	case formCancel:
		m.rem.form = nil
	case formSave:
		saved, err := saveReminderForm(context.Background(), m.rem.svc, m.rem.form, m.timeFormat, reminderNow())
		if err != nil {
			m.rem.form.err = reminderErrorText(m.loc, err)
			return nil
		}
		m.rem.form = nil
		m.rem.status, m.rem.statusErr = m.loc.Format("remind.tui.saved", reminderLabel(*saved)), false
		m.updateViewportContent()
		return m.fetchMonthData
	case formContinue:
	}
	return nil
}

// upcomingLines lists the reminders still to do in the next days in at most maxLines lines of
// width columns: today's one per line with its time and the time left, each later day on one
// line with its reminders' titles. The selected day is left out: its reminders are in the day.
func (m *calendarModel) upcomingLines(width, maxLines int) []string {
	if !m.remindersEnabled() || maxLines < calendarUpcomingMinLines {
		return nil
	}
	now := reminderNow()
	var items []models.ReminderOccurrence
	for _, occurrence := range m.rem.data.upcoming {
		status := occurrence.Status(now)
		if (status == models.ReminderPending || status == models.ReminderSnoozed) && !sameDay(occurrence.Due, m.currentDate) {
			items = append(items, occurrence)
		}
	}
	if len(items) == 0 {
		return nil
	}

	lines := []string{m.ui.renderSection(m.loc.Text("calendar.upcoming"), strconv.Itoa(len(items)))}
	for start := 0; start < len(items) && len(lines) < maxLines; {
		end := start + 1
		for end < len(items) && sameDay(items[end].Due, items[start].Due) {
			end++
		}
		if sameDay(items[start].Due, now) {
			lines = append(lines, m.upcomingToday(items[start:end], now, width)...)
		} else {
			lines = append(lines, m.upcomingDay(items[start:end], now, width))
		}
		start = end
	}
	return lines[:min(len(lines), maxLines)]
}

// upcomingToday is a line per reminder of today: "today   05:30 PM  Team sync ↻   in 2h13m",
// the nearest countdown in the text color.
func (m *calendarModel) upcomingToday(day []models.ReminderOccurrence, now time.Time, width int) []string {
	lines := make([]string, 0, len(day))
	nearest := true
	for i, occurrence := range day {
		label := ""
		if i == 0 {
			label = m.upcomingDayLabel(occurrence.Due, now)
		}
		when, whenStyle := "", m.ui.muted
		if !occurrence.Reminder.AllDay {
			when = m.loc.Format("remind.status.in", formatSpan(occurrence.DueBy().Sub(now)))
			if nearest {
				whenStyle, nearest = m.ui.text, false
			}
		}
		clock := formatReminderClock(m.loc, m.timeFormat, occurrence.Due, occurrence.Reminder.AllDay)
		lines = append(lines, m.upcomingTodayRow([2]string{label, clock}, occurrence.Reminder, when, whenStyle, width))
	}
	return lines
}

// upcomingTodayRow is the day and the time (lead), the title and the countdown at the right
// end, which is left out when it would leave the title too little room.
func (m *calendarModel) upcomingTodayRow(
	lead [2]string,
	reminder models.Reminder,
	when string,
	whenStyle lipgloss.Style,
	width int,
) string {
	repeat := ""
	if reminder.IsRepeating() {
		repeat = " " + glyphRepeat
	}
	dayW, clockW := upcomingColumns(width)
	room := width - dayW - clockW - lipgloss.Width(repeat)
	if room-lipgloss.Width(when)-1 < upcomingTitleMinW {
		when = ""
	}
	whenW := lipgloss.Width(when)
	if whenW > 0 {
		whenW++
	}
	title := cutText(reminder.Title, max(room-whenW, 1))
	gap := strings.Repeat(" ", max(room-lipgloss.Width(title)-lipgloss.Width(when), 0))
	if when == "" {
		gap = ""
	}
	prefix := padText(cutText(lead[0], dayW-1), dayW, false) + padText(lead[1], clockW, false)
	return m.ui.muted.Render(prefix) + m.ui.text.Render(title) + m.ui.muted.Render(repeat) + gap + whenStyle.Render(when)
}

// upcomingDay is a later day on one line: "thu 19  Coding practice, Standup".
func (m *calendarModel) upcomingDay(day []models.ReminderOccurrence, now time.Time, width int) string {
	titles := make([]string, 0, len(day))
	for _, occurrence := range day {
		titles = append(titles, occurrence.Reminder.Title)
	}
	dayW, _ := upcomingColumns(width)
	label := padText(cutText(m.upcomingDayLabel(day[0].Due, now), dayW-1), dayW, false)
	return m.ui.muted.Render(label) + m.ui.text.Render(cutText(strings.Join(titles, ", "), width-dayW))
}

// upcomingColumns are the widths of the day and time columns of Upcoming in width columns.
func upcomingColumns(width int) (int, int) {
	if width < upcomingNarrowW {
		return upcomingDayW - 1, upcomingClockW - 1
	}
	return upcomingDayW, upcomingClockW
}

// upcomingDayLabel is "today", "tmrw" or "thu 19".
func (m *calendarModel) upcomingDayLabel(day, now time.Time) string {
	switch {
	case sameDay(day, now):
		return strings.ToLower(m.loc.Text("calendar.upcoming.today"))
	case sameDay(day, now.AddDate(0, 0, 1)):
		return m.loc.Text("calendar.upcoming.tmrw")
	}
	return strings.ToLower(fmt.Sprintf("%s %d", localizedWeekdayAbbr(m.loc, day.Weekday()), day.Day()))
}
