package commands

import (
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// Columns of the day rows.
const (
	calendarProjectW  = 15
	calendarTagsW     = 18
	calendarDurationW = 7
	calendarBarMinW   = 4
)

// daySection is the part of the day a selected row is in.
type daySection int

const (
	sectionNone daySection = iota
	sectionReminders
	sectionTracked
)

// daySelection is the selected row of the day: reminders come first, then tracked time.
type daySelection struct {
	section daySection
	index   int
}

// dayActivities lists the selected day's activities by start time.
func (m *calendarModel) dayActivities() []models.Activity {
	report, ok := m.reportForDate(m.currentDate)
	if !ok || report == nil {
		return nil
	}
	activities := make([]models.Activity, len(report.Activities))
	copy(activities, report.Activities)
	sort.SliceStable(activities, func(i, j int) bool {
		return activities[i].StartTime.Before(activities[j].StartTime)
	})
	return activities
}

func (m *calendarModel) selectedReminder() (models.ReminderOccurrence, bool) {
	occurrences := m.dayReminders()
	if m.sel.section != sectionReminders || m.sel.index >= len(occurrences) {
		return models.ReminderOccurrence{}, false
	}
	return occurrences[m.sel.index], true
}

func (m *calendarModel) selectedActivity() (models.Activity, bool) {
	activities := m.dayActivities()
	if m.sel.section != sectionTracked || m.sel.index >= len(activities) {
		return models.Activity{}, false
	}
	return activities[m.sel.index], true
}

// handleSelectionKey moves the selection through the day's rows: j/k always, the arrows
// once a row is selected; tab jumps between reminders and tracked time.
func (m *calendarModel) handleSelectionKey(msg tea.KeyMsg) bool {
	if m.monthOpen {
		switch msg.String() {
		case "m", keyEsc:
			m.monthOpen = false
			return true
		}
		return false
	}
	switch msg.String() {
	case "j":
		m.moveSelection(1)
	case "k":
		m.moveSelection(-1)
	case keyDown, "up":
		if m.sel.section == sectionNone {
			return false
		}
		step := 1
		if msg.String() == "up" {
			step = -1
		}
		m.moveSelection(step)
	case keyTab:
		m.nextSection()
	case keyEsc:
		if m.sel.section == sectionNone {
			return false
		}
		m.sel = daySelection{}
	case "m":
		if !m.layout.dayOnly() {
			return false
		}
		m.monthOpen = true
	default:
		return false
	}
	m.rem.status = ""
	m.updateViewportContent()
	return true
}

// moveSelection selects the row step rows away; with nothing selected, j takes the first row and k the last.
func (m *calendarModel) moveSelection(step int) {
	reminders, activities := len(m.dayReminders()), len(m.dayActivities())
	total := reminders + activities
	if total == 0 {
		m.sel = daySelection{}
		return
	}

	row := -1
	switch m.sel.section {
	case sectionReminders:
		row = m.sel.index
	case sectionTracked:
		row = reminders + m.sel.index
	case sectionNone:
	}
	switch {
	case row < 0 && step > 0:
		row = 0
	case row < 0:
		row = total - 1
	default:
		row = min(max(row+step, 0), total-1)
	}

	if row < reminders {
		m.sel = daySelection{section: sectionReminders, index: row}
	} else {
		m.sel = daySelection{section: sectionTracked, index: row - reminders}
	}
}

// nextSection goes from nothing to the reminders, to the tracked time and back, skipping empty ones.
func (m *calendarModel) nextSection() {
	counts := map[daySection]int{
		sectionNone:      0,
		sectionReminders: len(m.dayReminders()),
		sectionTracked:   len(m.dayActivities()),
	}
	order := []daySection{sectionNone, sectionReminders, sectionTracked}
	at := int(m.sel.section)
	for range order {
		at = (at + 1) % len(order)
		if next := order[at]; next == sectionNone || counts[next] > 0 {
			m.sel = daySelection{section: next}
			return
		}
	}
}

// clampSelection keeps the selection on a row that still exists after the day changed.
func (m *calendarModel) clampSelection() {
	var count int
	switch m.sel.section {
	case sectionReminders:
		count = len(m.dayReminders())
	case sectionTracked:
		count = len(m.dayActivities())
	case sectionNone:
		return
	}
	if count == 0 {
		m.sel = daySelection{}
		return
	}
	m.sel.index = min(m.sel.index, count-1)
}

// updateViewportContent renders the day column: its title, reminders, tracked time and,
// with room for it, the time by project; the selected row is scrolled into view.
func (m *calendarModel) updateViewportContent() {
	m.clampSelection()
	width := m.viewport.Width
	activities := m.dayActivities()
	reminders := m.dayReminders()
	now := reminderNow()

	var total time.Duration
	for _, activity := range activities {
		total += activity.Duration()
	}
	done := 0
	for _, occurrence := range reminders {
		if occurrence.Status(now) == models.ReminderDone {
			done++
		}
	}

	lines := []string{m.renderDayTitle(total, done, len(reminders), width), ""}
	selectedLine := -1
	if len(reminders) > 0 {
		lines = append(lines, m.ui.renderSection(m.loc.Text("calendar.reminders"), strconv.Itoa(done)+"/"+strconv.Itoa(len(reminders))))
		rows := m.reminderRows()
		opts := reminderRowOpts{day: m.currentDate, notes: m.layout.inline}
		for i, occurrence := range reminders {
			selected := m.sel.section == sectionReminders && m.sel.index == i
			if selected {
				selectedLine = len(lines)
			}
			lines = append(lines, rows.row(occurrence, trackedDuration(occurrence, activities), now, selected, width, opts))
		}
		lines = append(lines, "")
	}

	if len(activities) == 0 {
		lines = append(lines, m.ui.muted.Render(m.loc.Text("calendar.no_events")))
	} else {
		lines = append(lines, m.ui.renderSection(m.loc.Text("calendar.tracked"), strconv.Itoa(len(activities)), m.formatTotal(total)))
		durationW := m.durationWidth(activities)
		for i, activity := range activities {
			selected := m.sel.section == sectionTracked && m.sel.index == i
			if selected {
				selectedLine = len(lines)
			}
			lines = append(lines, m.renderActivityRow(activity, selected, width, durationW))
		}
		if m.layout.extras {
			lines = append(lines, "")
			lines = append(lines, m.byProjectLines(activities, width)...)
		}
	}

	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.scrollToLine(selectedLine)
}

// scrollToLine scrolls the day just enough to show the line.
func (m *calendarModel) scrollToLine(line int) {
	switch {
	case line < 0:
	case line < m.viewport.YOffset:
		m.viewport.SetYOffset(line)
	case line >= m.viewport.YOffset+m.viewport.Height:
		m.viewport.SetYOffset(line - m.viewport.Height + 1)
	}
}

// renderDayTitle is the date with the day's totals at the right end, as many as fit.
func (m *calendarModel) renderDayTitle(total time.Duration, done, reminders, width int) string {
	date := formatLocalizedLongDate(m.loc, m.currentDate)
	var parts []string
	if total > 0 {
		parts = append(parts, m.loc.Format("calendar.day.tracked", m.formatTotal(total)))
	}
	if reminders > 0 {
		parts = append(parts, m.loc.Format("calendar.day.reminders_done", done, reminders))
	}
	for len(parts) > 0 && lipgloss.Width(date)+lipgloss.Width(strings.Join(parts, " · "))+2 > width {
		parts = parts[:len(parts)-1]
	}
	right := strings.Join(parts, " · ")
	if right == "" {
		return m.ui.bold.Render(cutText(date, width))
	}
	gap := width - lipgloss.Width(date) - lipgloss.Width(right)
	return m.ui.bold.Render(date) + strings.Repeat(" ", gap) + m.ui.muted.Render(right)
}

// reminderRows draws the day's reminders with the calendar's tag colors: their time, title
// and status; with room for them, how they repeat and their project, and in layout B their
// notes. Build it once per redraw, not per row.
func (m *calendarModel) reminderRows() reminderRows {
	rows := newReminderRows(m.ui, m.loc, m.timeFormat)
	rows.project = func(name string) lipgloss.Style { return m.tagColorStyle(m.ui.text, name, tagColorScopeCalendar) }
	rows.tags = func(tags []string) string { return m.renderTags(tags, false) }
	rows.next = &m.rem.next
	return rows
}

// renderActivityRow is one tracked activity: its time, project, description and duration;
// in layout B also its notes and tags. A running one is marked ● and ends "now".
func (m *calendarModel) renderActivityRow(activity models.Activity, selected bool, width, durationW int) string {
	running := activity.EndTime == nil
	glyph, durationStyle := "", m.ui.text
	if running {
		glyph, durationStyle = glyphRunning, m.ui.ok
	}

	description := rowCell{text: activity.Description, style: m.ui.text}
	if m.layout.inline && activity.Notes != "" {
		description.suffix, description.suffixStyle = " — "+oneLine(activity.Notes), m.ui.muted
	}
	project := rowCell{
		text:  activity.Project,
		style: m.tagColorStyle(m.ui.text, activity.Project, tagColorScopeCalendar),
		width: calendarProjectW,
	}
	spent := rowCell{text: m.activityDuration(activity), style: durationStyle, width: durationW, right: true}

	cells := make([]rowCell, 0, rowCellsCap)
	cells = append(cells, m.activityTimeCell(activity))
	if m.config.Calendar.AlignDurationLeft {
		spent.right = false
		cells = append(cells, spent, project, description)
	} else {
		cells = append(cells, project, description)
	}
	if m.layout.inline {
		cells = append(cells, rowCell{styled: m.renderTags(activity.Tags, selected), width: calendarTagsW})
	}
	if !m.config.Calendar.AlignDurationLeft {
		cells = append(cells, spent)
	}
	return m.ui.renderRow(glyph, m.ui.ok, cells, selected, width)
}

// activityTimeCell is "10:00–12:45", "13:30–now" for a running activity, or only the start
// on narrow terminals and when calendar.time_end_format puts the end after the duration.
func (m *calendarModel) activityTimeCell(activity models.Activity) rowCell {
	startFormat := m.config.Calendar.TimeStartFormat
	if startFormat == "" {
		startFormat = m.timeFormat.GetDisplayFormat()
	}
	start := activity.StartTime.Format(startFormat)
	sampleW := clockWidth(startFormat)

	if m.layout.dayOnly() || m.config.Calendar.TimeEndFormat != "" {
		style := m.ui.muted
		if activity.EndTime == nil {
			style = m.ui.ok
		}
		return rowCell{text: start, style: style, width: sampleW + 2}
	}

	cell := rowCell{text: start + "–", style: m.ui.muted, suffixStyle: m.ui.muted, width: 2*sampleW + 3}
	if activity.EndTime == nil {
		cell.suffix, cell.suffixStyle = m.loc.Text("calendar.now"), m.ui.ok
	} else {
		cell.suffix = activity.EndTime.Format(m.timeFormat.GetDisplayFormat())
	}
	return cell
}

// activityDuration is the time spent, in calendar.time_spent_format when set, followed by
// the end in calendar.time_end_format when that is set.
func (m *calendarModel) activityDuration(activity models.Activity) string {
	spent := m.formatSpent(activity.Duration())
	if format := m.config.Calendar.TimeEndFormat; format != "" && activity.EndTime != nil {
		spent += activity.EndTime.Format(format)
	}
	return spent
}

// durationWidth fits the duration column to the longest duration of the day.
func (m *calendarModel) durationWidth(activities []models.Activity) int {
	width := calendarDurationW
	for _, activity := range activities {
		width = max(width, lipgloss.Width(m.activityDuration(activity))+1)
	}
	return width
}

// formatSpent formats a duration in calendar.time_spent_format, or as "2h45m".
func (m *calendarModel) formatSpent(d time.Duration) string {
	if format := m.config.Calendar.TimeSpentFormat; format != "" {
		return timeutil.FormatDuration(d, format)
	}
	return formatHM(d)
}

// formatTotal formats a total in calendar.time_total_format, falling back to the time spent format.
func (m *calendarModel) formatTotal(d time.Duration) string {
	if format := m.config.Calendar.TimeTotalFormat; format != "" {
		return timeutil.FormatDuration(d.Round(time.Minute), format)
	}
	return m.formatSpent(d)
}

// renderTags is "#backend #api", each tag in its own color.
func (m *calendarModel) renderTags(tags []string, selected bool) string {
	on := func(style lipgloss.Style) lipgloss.Style {
		if selected {
			return withSelection(style, m.theme).Bold(true)
		}
		return style
	}
	parts := make([]string, len(tags))
	for i, tag := range tags {
		parts[i] = on(m.tagColorStyle(m.ui.tag, tag, tagColorScopeCalendar)).Render("#" + tag)
	}
	return strings.Join(parts, on(lipgloss.NewStyle()).Render(" "))
}

// byProjectLines is the day's time per project as bars.
func (m *calendarModel) byProjectLines(activities []models.Activity, width int) []string {
	byProject := map[string]time.Duration{}
	for _, activity := range activities {
		byProject[activity.Project] += activity.Duration()
	}
	projects := make([]insights.ProjectDuration, 0, len(byProject))
	for name, d := range byProject {
		projects = append(projects, insights.ProjectDuration{Name: name, Duration: d})
	}
	sortProjectDurations(projects)

	lines := []string{m.ui.renderSection(m.loc.Text("calendar.by_project"))}
	barW := max(width-rowGutterW-calendarProjectW-calendarDurationW-1, calendarBarMinW)
	for _, project := range projects {
		lines = append(lines, m.projectBarLine(project, projects[0].Duration, "   ", barW, tagColorScopeCalendar))
	}
	return lines
}

// projectBarLine is "Project Alpha  ▇▇▇▇▇▇  2h45m" with the bar scaled to longest.
func (m *calendarModel) projectBarLine(
	project insights.ProjectDuration,
	longest time.Duration,
	indent string,
	barW int,
	scope tagColorScope,
) string {
	return indent + m.ui.renderBarLine(barLine{
		label:      project.Name,
		labelStyle: m.tagColorStyle(m.ui.text, project.Name, scope),
		labelW:     calendarProjectW,
		length:     barLength(project.Duration, longest, barW),
		nonzero:    project.Duration > 0,
		barStyle:   m.tagBarStyle(m.ui.info, project.Name, scope),
		barW:       barW,
		value:      m.formatSpent(project.Duration),
		valueW:     calendarDurationW + 1,
	})
}

// sortProjectDurations orders projects by time, longest first, then by name.
func sortProjectDurations(projects []insights.ProjectDuration) {
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Duration != projects[j].Duration {
			return projects[i].Duration > projects[j].Duration
		}
		return projects[i].Name < projects[j].Name
	})
}

// focusLines are the details of the selected row for the focus box.
func (m *calendarModel) focusLines() []string {
	label := m.ui.mutedBold.Render(m.loc.Text("calendar.focus") + "  ")
	if occurrence, ok := m.selectedReminder(); ok {
		return m.reminderRows().focus(label, occurrence, reminderNow())
	}
	if activity, ok := m.selectedActivity(); ok {
		return m.activityFocus(label, activity)
	}
	return []string{label + m.ui.muted.Render(m.loc.Text("calendar.focus.hint"))}
}

func (m *calendarModel) activityFocus(label string, activity models.Activity) []string {
	end := m.loc.Text("calendar.now")
	if activity.EndTime != nil {
		end = activity.EndTime.Format(m.timeFormat.GetDisplayFormat())
	}
	span := activity.StartTime.Format(m.timeFormat.GetDisplayFormat()) + "–" + end
	head := label + m.ui.accentBold.Render(activity.Project) +
		m.ui.muted.Render(" · "+span+" · "+m.formatSpent(activity.Duration()))
	if len(activity.Tags) > 0 {
		head += m.ui.muted.Render(" · ") + m.renderTags(activity.Tags, false)
	}
	body := m.ui.text.Render(activity.Description)
	if activity.Notes != "" {
		body += m.ui.muted.Render(" — " + oneLine(activity.Notes))
	}
	return []string{head, body}
}

// oneLine puts multi-line notes on one line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
