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
	calendarUpcomingDays     = 7
	calendarUpcomingMinLines = 5
	calendarUpcomingMaxLines = 12
	calendarSidebarWidth     = 40
	calendarReminderGlyphW   = 2
	calendarReminderClockW   = 8
	calendarReminderRepeatW  = 16
	calendarReminderProjectW = 10
	calendarReminderStatusW  = 16
	calendarReminderTitleMin = 12
	calendarReminderTitleMax = 30
	// calendarReminderBaseW is what a reminder row takes besides its title, repeat and project:
	// glyph, clock, status and the gaps; calendarReminderMetaW is what repeat and project add.
	calendarReminderBaseW = calendarReminderGlyphW + calendarReminderClockW + 1 + 1 + calendarReminderStatusW
	calendarReminderMetaW = calendarReminderRepeatW + 1 + calendarReminderProjectW + 2
	tableSelectedFG       = lipgloss.Color("229")
	tableSelectedBG       = lipgloss.Color("57")
)

// calendarReminders is the calendar's reminder state: what is shown and the focused list.
type calendarReminders struct {
	svc    ports.ReminderResolver
	env    reminderFormEnv
	snooze time.Duration

	data reminderData

	focused       bool
	cursor        int
	form          *reminderForm
	pendingDelete *models.ReminderOccurrence
	status        string
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
	upcoming, err := m.rem.svc.Occurrences(ctx, today, today.AddDate(0, 0, calendarUpcomingDays), now)
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

// reminderMark is the dot a calendar day gets for its reminders, if any.
func (m *calendarModel) reminderMark(date time.Time, now time.Time) (lipgloss.Color, bool) {
	marked, pending := false, false
	for _, occurrence := range m.rem.data.month {
		if !sameDay(occurrence.Due, date) || !marksCalendarDay(occurrence.Reminder) {
			continue
		}
		marked = true
		switch occurrence.Status(now) {
		case models.ReminderOverdue:
			return m.theme.Secondary, true
		case models.ReminderPending, models.ReminderSnoozed:
			pending = true
		case models.ReminderDone, models.ReminderMissed:
		}
	}
	if !marked {
		return "", false
	}
	if !pending {
		return m.theme.Faint, true
	}
	return m.theme.Reminder, true
}

// renderDayCell draws a day number; a day with reminders gets a dot right after the number,
// in the space the centered number leaves.
func (m *calendarModel) renderDayCell(style lipgloss.Style, date, now time.Time) string {
	number := strconv.Itoa(date.Day())
	color, ok := m.reminderMark(date, now)
	if !m.remindersEnabled() || !ok {
		return style.Render(number)
	}

	inline := style.UnsetWidth().UnsetAlign()
	plain := inline.UnsetUnderline()
	rest := max(style.GetWidth()-1-len(number)-1, 0)
	return plain.Render(" ") + inline.Render(number) + plain.Foreground(color).Render("•") + plain.Render(strings.Repeat(" ", rest))
}

// calendarHelp is the help under the month, with the reminder keys and legend when they apply.
func (m *calendarModel) calendarHelp() string {
	faint := lipgloss.NewStyle().Foreground(m.styles.Weekday.GetForeground())
	if !m.remindersEnabled() {
		return faint.Render(m.loc.Format("calendar.help"))
	}
	legend := m.styles.Reminder.Render("•") + faint.Render(" "+m.loc.Text("calendar.legend.reminders"))
	return faint.Render(m.loc.Text("calendar.help.reminders")) + "\n\n" + legend
}

// writeReminderSection writes the selected day's reminders above its activities.
func (m *calendarModel) writeReminderSection(b *strings.Builder, activities []models.Activity) bool {
	if !m.remindersEnabled() {
		return false
	}
	occurrences := m.dayReminders()
	if len(occurrences) == 0 {
		m.rem.focused = false
		return false
	}
	m.rem.cursor = min(m.rem.cursor, len(occurrences)-1)
	now := reminderNow()

	done := 0
	for _, occurrence := range occurrences {
		if occurrence.Status(now) == models.ReminderDone {
			done++
		}
	}
	faint := lipgloss.NewStyle().Foreground(m.theme.SubText)
	b.WriteString(m.styles.Reminder.Bold(true).Render(m.loc.Text("calendar.reminders")) + "  " +
		faint.Render(m.loc.Format("calendar.reminders.done", done, len(occurrences))) + "\n\n")

	for i, occurrence := range occurrences {
		b.WriteString(m.renderReminderRow(occurrence, activities, now, m.rem.focused && i == m.rem.cursor) + "\n")
	}
	b.WriteString("\n")

	switch {
	case m.rem.pendingDelete != nil:
		m.writeWrapped(b, m.reminderDeletePromptStyle(), m.reminderDeletePrompt())
		b.WriteString("\n")
	case m.rem.focused:
		m.writeWrapped(b, faint, m.loc.Format("calendar.reminders.help", formatNotifyValue(m.rem.snooze)))
		if m.rem.status != "" {
			m.writeWrapped(b, m.styles.Duration, m.rem.status)
		}
		b.WriteString("\n")
	case m.rem.status != "":
		m.writeWrapped(b, m.styles.Duration, m.rem.status)
		b.WriteString("\n")
	}
	return true
}

// writeWrapped writes text word-wrapped to the details pane, one styled line at a time.
func (m *calendarModel) writeWrapped(b *strings.Builder, style lipgloss.Style, text string) {
	for _, line := range wrapText(text, m.viewport.Width-2) {
		b.WriteString(style.Render(line) + "\n")
	}
}

func (m *calendarModel) renderReminderRow(
	occurrence models.ReminderOccurrence,
	activities []models.Activity,
	now time.Time,
	selected bool,
) string {
	loc, tf := m.loc, m.timeFormat
	reminder := occurrence.Reminder
	status := occurrence.Status(now)

	// Narrow details drop the repeat and project columns before squeezing the title.
	showMeta := m.viewport.Width >= calendarReminderBaseW+calendarReminderMetaW+calendarReminderTitleMin
	titleWidth := m.viewport.Width - calendarReminderBaseW
	if showMeta {
		titleWidth -= calendarReminderMetaW
	}
	titleWidth = min(max(titleWidth, calendarReminderTitleMin), calendarReminderTitleMax)

	clock := formatReminderClock(loc, tf, occurrence.Due, reminder.AllDay)
	if !sameDay(occurrence.Due, m.currentDate) {
		// An overdue one from an earlier day shows its day: "Sun 15".
		clock = fmt.Sprintf("%s %d", localizedWeekdayAbbr(loc, occurrence.Due.Weekday()), occurrence.Due.Day())
	}
	repeat := ""
	if reminder.IsRepeating() {
		repeat = "↻ " + formatRepeat(loc, reminder)
	}

	glyphStyle, titleStyle, statusStyle := m.reminderRowStyles(status)
	if selected {
		glyphStyle = glyphStyle.Background(tableSelectedBG)
		titleStyle = titleStyle.Foreground(tableSelectedFG).Background(tableSelectedBG)
	}

	cells := []string{
		glyphStyle.Width(calendarReminderGlyphW).Render(reminderStatusGlyph(status)),
		titleStyle.Width(calendarReminderClockW + 1).Render(truncateText(clock, calendarReminderClockW)),
		titleStyle.Width(titleWidth + 1).Render(truncateText(reminder.Title, titleWidth)),
	}
	if showMeta {
		cells = append(cells,
			lipgloss.NewStyle().Foreground(m.theme.SubText).Width(calendarReminderRepeatW+1).
				Render(truncateText(repeat, calendarReminderRepeatW)),
			m.styles.Project.Width(calendarReminderProjectW+2).Render(truncateText(reminder.Project, calendarReminderProjectW)),
		)
	}
	statusText := formatReminderStatus(loc, tf, occurrence, now, trackedDuration(occurrence, activities))
	cells = append(cells, statusStyle.Render(truncateText(statusText, calendarReminderStatusW)))
	return strings.Join(cells, "")
}

// reminderRowStyles colors a reminder by its status: overdue red, done struck through.
func (m *calendarModel) reminderRowStyles(status models.ReminderStatus) (lipgloss.Style, lipgloss.Style, lipgloss.Style) {
	glyph := lipgloss.NewStyle().Foreground(m.theme.Reminder)
	title := lipgloss.NewStyle().Foreground(m.theme.Text)
	statusStyle := lipgloss.NewStyle().Foreground(m.theme.SubText)

	switch status {
	case models.ReminderOverdue:
		glyph = m.styles.Overdue
		statusStyle = m.styles.Overdue
	case models.ReminderDone:
		glyph = lipgloss.NewStyle().Foreground(m.theme.Tag)
		title = title.Foreground(m.theme.SubText).Strikethrough(true)
	case models.ReminderMissed:
		glyph = lipgloss.NewStyle().Foreground(m.theme.Faint)
		title = title.Foreground(m.theme.SubText)
	case models.ReminderSnoozed:
		statusStyle = m.styles.Duration
	case models.ReminderPending:
	}
	return glyph, title, statusStyle
}

func (m *calendarModel) reminderDeletePrompt() string {
	occurrence := m.rem.pendingDelete
	key := "remind.tui.delete_confirm"
	if occurrence.Reminder.IsRepeating() {
		key = "remind.tui.delete_confirm_repeating"
	}
	return m.loc.Format(key, occurrence.Reminder.ID, occurrence.Reminder.Title)
}

func (m *calendarModel) reminderDeletePromptStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203"))
}

// trackedEntries is "1 entry" or "4 entries".
func (m *calendarModel) trackedEntries(n int) string {
	if n == 1 {
		return m.loc.Text("calendar.tracked.entry")
	}
	return m.loc.Format("calendar.tracked.entries", n)
}

// truncateText cuts s to width runes, ending it with "…" when cut.
func truncateText(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

// handleReminderKey handles the keys of the reminder form, the delete prompt and the
// focused reminder list; handled is false for keys the calendar should take.
func (m *calendarModel) handleReminderKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if !m.remindersEnabled() {
		return nil, false
	}
	switch {
	case m.rem.form != nil:
		return m.updateReminderForm(msg), true
	case m.rem.pendingDelete != nil:
		return m.confirmReminderDelete(msg), true
	}

	switch msg.String() {
	case "tab":
		m.rem.focused = !m.rem.focused && len(m.dayReminders()) > 0
		m.rem.status = ""
		m.updateViewportContent()
		return nil, true
	case "a":
		day := time.Date(m.currentDate.Year(), m.currentDate.Month(), m.currentDate.Day(), 0, 0, 0, 0, time.Local)
		m.rem.form = newReminderFormFromRequest(m.timeFormat, models.AddReminderRequest{Due: day, AllDay: true})
		return nil, true
	}
	if !m.rem.focused {
		return nil, false
	}
	return m.handleFocusedReminderKey(msg)
}

func (m *calendarModel) handleFocusedReminderKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	occurrences := m.dayReminders()
	if len(occurrences) == 0 {
		m.rem.focused = false
		return nil, false
	}
	selected := occurrences[min(m.rem.cursor, len(occurrences)-1)]

	switch msg.String() {
	case keyEsc:
		m.rem.focused = false
	case "up", "k":
		m.rem.cursor = max(m.rem.cursor-1, 0)
	case keyDown, "j":
		m.rem.cursor = min(m.rem.cursor+1, len(occurrences)-1)
	case " ", "space":
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
	if err != nil {
		m.rem.status = reminderErrorText(m.loc, err)
	} else {
		m.rem.status = status
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
		m.rem.status = m.loc.Format("remind.tui.saved", reminderLabel(*saved))
		m.updateViewportContent()
		return m.fetchMonthData
	case formContinue:
	}
	return nil
}

// renderUpcoming is the sidebar block with the overdue count and what comes next, in at most
// maxLines lines; empty when there is nothing to show.
func (m *calendarModel) renderUpcoming(maxLines int) (string, int) {
	if !m.remindersEnabled() || maxLines < calendarUpcomingMinLines {
		return "", 0
	}
	now := reminderNow()
	faint := lipgloss.NewStyle().Foreground(m.theme.SubText)

	lines := make([]string, 0, maxLines)
	if overdue := overdueOnly(m.rem.data.overdue, now); len(overdue) > 0 {
		text := m.loc.Format("calendar.upcoming.overdue", len(overdue), overdue[0].Reminder.Title)
		lines = append(
			lines,
			m.styles.Overdue.Render("! ")+m.styles.Overdue.UnsetBold().Render(truncateText(text, calendarSidebarWidth-2)),
			"",
		)
	}

	var day time.Time
	first := true
	for _, occurrence := range m.rem.data.upcoming {
		status := occurrence.Status(now)
		if status != models.ReminderPending && status != models.ReminderSnoozed {
			continue
		}
		if !sameDay(occurrence.Due, day) {
			if len(lines)+2 > maxLines-2 {
				break
			}
			day = occurrence.Due
			lines = append(lines, lipgloss.NewStyle().Bold(true).Render(m.upcomingDayLabel(day, now)))
		} else if len(lines)+1 > maxLines-2 {
			break
		}
		lines = append(lines, m.renderUpcomingItem(occurrence, now, first, faint))
		first = false
	}
	if len(lines) == 0 {
		return "", 0
	}

	header := m.styles.Header.Width(calendarSidebarWidth).Render(m.loc.Text("calendar.sidebar.upcoming"))
	block := header + "\n\n" + strings.Join(lines, "\n") + "\n\n"
	return block, len(lines) + 3
}

func (m *calendarModel) upcomingDayLabel(day, now time.Time) string {
	switch {
	case sameDay(day, now):
		return m.loc.Text("calendar.upcoming.today")
	case sameDay(day, now.AddDate(0, 0, 1)):
		return m.loc.Text("calendar.upcoming.tomorrow")
	}
	return fmt.Sprintf("%s %d", localizedWeekdayAbbr(m.loc, day.Weekday()), day.Day())
}

func (m *calendarModel) renderUpcomingItem(occurrence models.ReminderOccurrence, now time.Time, next bool, faint lipgloss.Style) string {
	reminder := occurrence.Reminder
	clock := formatReminderClock(m.loc, m.timeFormat, occurrence.Due, reminder.AllDay)
	title := reminder.Title
	if reminder.IsRepeating() {
		title += " ↻"
	}

	suffix := ""
	if next {
		suffix = formatReminderStatus(m.loc, m.timeFormat, occurrence, now, 0)
	}
	titleWidth := calendarSidebarWidth - calendarReminderClockW - 2 - len([]rune(suffix))
	line := faint.Width(calendarReminderClockW).Render(clock) + " " + truncateText(title, titleWidth)
	if suffix != "" {
		line += " " + m.styles.Duration.Render(suffix)
	}
	return line
}
