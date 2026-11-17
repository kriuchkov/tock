package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/core/models"
)

const (
	barChar = "▏"
	// calendarDayCellW is a day of the month grid: the number right-aligned in three columns and its mark.
	calendarDayCellW    = 4
	calendarLegendW     = 13
	calendarStatLabelW  = 10
	calendarLastDays    = 7
	calendarTopProjects = 5
	calendarDayLabelW   = 6
)

// monthLines is the month grid: its name, the weekdays and a line per week.
func (m *calendarModel) monthLines() []string {
	now := time.Now()
	lines := []string{m.ui.renderSection(formatLocalizedMonthYear(m.loc, m.viewDate)), ""}

	weekdays := make([]string, 0, len(localizedWeekdayShortNames(m.loc)))
	for _, name := range localizedWeekdayShortNames(m.loc) {
		weekdays = append(weekdays, padText(name, calendarDayCellW-1, true))
	}
	lines = append(lines, m.ui.muted.Render(strings.Join(weekdays, " ")))

	first := time.Date(m.viewDate.Year(), m.viewDate.Month(), 1, 0, 0, 0, 0, time.Local)
	column := (int(first.Weekday()) + 6) % 7 // Monday first
	var week strings.Builder
	week.WriteString(strings.Repeat(" ", column*calendarDayCellW))
	for date := first; date.Month() == first.Month(); date = date.AddDate(0, 0, 1) {
		week.WriteString(m.renderDayCell(date, now))
		if column++; column == 7 {
			lines = append(lines, week.String())
			week.Reset()
			column = 0
		}
	}
	if column > 0 {
		lines = append(lines, week.String())
	}
	return lines
}

// renderDayCell is a day of the month grid: muted without tracked time, bold with it, the accent today,
// on the selection color when selected, followed by its reminder mark.
func (m *calendarModel) renderDayCell(date, now time.Time) string {
	style := m.ui.muted
	if report, ok := m.monthReports[date.Day()]; ok && report.TotalDuration > 0 {
		style = m.ui.bold
	}
	if sameDay(date, now) {
		style = m.ui.accentBold
	}
	mark, markStyle := " ", lipgloss.NewStyle()
	if dayMark, ok := m.reminderMark(date, now); ok {
		mark, markStyle = dayMark.glyph, dayMark.style
	}
	if sameDay(date, m.currentDate) {
		style = withSelection(style, m.theme).Bold(true)
		markStyle = withSelection(markStyle, m.theme)
	}
	return style.Render(fmt.Sprintf("%3d", date.Day())) + markStyle.Render(mark)
}

// legendLines explains the grid with days of this month: a tracked one, today, and the reminder marks.
func (m *calendarModel) legendLines() []string {
	now := time.Now()
	sample := func(day int, style lipgloss.Style, label string) string {
		return padText(style.Render(fmt.Sprintf("%2d", day))+m.ui.muted.Render(" "+label), calendarLegendW, false)
	}
	mark := func(glyph string, style lipgloss.Style, label string) string {
		return style.Render(glyph) + m.ui.muted.Render(" "+label)
	}

	var tracked, today, reminder, overdue string
	for day := 1; day <= 31; day++ {
		if report, ok := m.monthReports[day]; ok && report.TotalDuration > 0 {
			tracked = sample(day, m.ui.bold, m.loc.Text("calendar.legend.tracked"))
			break
		}
	}
	if now.Year() == m.viewDate.Year() && now.Month() == m.viewDate.Month() {
		today = sample(now.Day(), m.ui.accentBold, m.loc.Text("calendar.legend.today"))
	}
	if m.remindersEnabled() {
		reminder = mark(glyphMark, m.ui.reminder, m.loc.Text("calendar.legend.reminder"))
		overdue = mark(glyphOverdue, m.ui.accent, m.loc.Text("calendar.legend.overdue"))
	}

	var lines []string
	for _, pair := range [][2]string{{tracked, reminder}, {today, overdue}} {
		if pair[0] == "" && pair[1] == "" {
			continue
		}
		lines = append(lines, strings.TrimRight(padText(pair[0], calendarLegendW, false)+pair[1], " "))
	}
	return lines
}

func (m *calendarModel) monthStats() insights.ProductivityStats {
	daysInMonth := time.Date(m.viewDate.Year(), m.viewDate.Month()+1, 0, 0, 0, 0, 0, time.Local).Day()
	return insights.ComputeProductivityStats(m.monthReports, daysInMonth)
}

// monthStatsLines are the month totals under the grid in layout A, with the week target when set.
func (m *calendarModel) monthStatsLines(width int) []string {
	stats := m.monthStats()
	row := func(label, value string) string {
		return m.ui.muted.Render(padText(m.loc.Text(label), calendarStatLabelW, false)) + m.ui.text.Render(value)
	}
	lines := []string{
		m.ui.renderSection(m.loc.Text("calendar.month"), formatHM(stats.TotalDuration)),
		row("calendar.month.avg_day", formatHM(stats.AvgDuration)),
		row("calendar.month.max_day", formatHM(stats.MaxDailyDuration)),
		row("calendar.month.streak", m.loc.Format("calendar.month.streak_days", stats.LongestStreak)),
	}

	target := m.config.WeeklyTarget
	if target <= 0 {
		return lines
	}
	week := insights.BuildWeeklyActivityData(m.dailyReports, m.currentDate).CurrentWeekTotal
	percent := float64(week) / float64(target) * 100
	progress := row("calendar.month.week", formatHM(week)+" / "+formatHM(target))
	lines = append(lines, progress+m.ui.muted.Render(fmt.Sprintf("  %.0f%%", percent)))

	barW := max(width-1, calendarBarMinW)
	filled := barLength(min(week, target), target, barW)
	lines = append(lines, m.ui.info.Render(strings.Repeat(glyphBar, filled))+m.ui.faint.Render(strings.Repeat(glyphTrack, barW-filled)))
	return lines
}

// lastDaysLines are bars of the seven days up to the selected one, split by project.
func (m *calendarModel) lastDaysLines(width int) []string {
	type dayTotal struct {
		date     time.Time
		total    time.Duration
		projects []insights.ProjectDuration
	}
	days := make([]dayTotal, calendarLastDays)
	var total, longest time.Duration
	for i := range days {
		date := m.currentDate.AddDate(0, 0, i-calendarLastDays+1)
		days[i].date = date
		if report, ok := m.reportForDate(date); ok && report != nil {
			days[i].total = report.TotalDuration
			days[i].projects = reportProjects(report)
		}
		total += days[i].total
		longest = max(longest, days[i].total)
	}

	lines := []string{m.ui.renderSection(m.loc.Text("calendar.last_days"), formatHM(total))}
	barW := max(width-calendarDayLabelW-2-calendarDurationW-1, calendarBarMinW)
	for _, day := range days {
		labelStyle := m.ui.muted
		if sameDay(day.date, m.currentDate) {
			labelStyle = m.ui.accentBold
		}
		label := fmt.Sprintf("%s %2d", localizedWeekdayShort(m.loc, day.date.Weekday()), day.date.Day())
		bar := m.renderWeekBar(day.total, longest, day.projects, barW)
		value := m.ui.muted.Render(padText("—", calendarDurationW+1, true))
		if day.total > 0 {
			value = m.ui.text.Render(padText(formatHM(day.total), calendarDurationW+1, true))
		}
		lines = append(lines, labelStyle.Render(padText(label, calendarDayLabelW, false))+"  "+bar+
			strings.Repeat(" ", max(barW-lipgloss.Width(bar), 0))+value)
	}
	return lines
}

// reportProjects is a report's time per project, longest first.
func reportProjects(report *models.Report) []insights.ProjectDuration {
	projects := make([]insights.ProjectDuration, 0, len(report.ByProject))
	for name, project := range report.ByProject {
		projects = append(projects, insights.ProjectDuration{Name: name, Duration: project.Duration})
	}
	sortProjectDurations(projects)
	return projects
}

// renderWeekBar renders a segmented bar for one day in up to width columns. Each project
// gets a share of it in the project's tag color; projects without one share the info color.
func (m *calendarModel) renderWeekBar(dur, maxDur time.Duration, projects []insights.ProjectDuration, width int) string {
	if dur <= 0 {
		return ""
	}
	length := barLength(dur, maxDur, width)
	if length == 0 {
		return m.ui.info.Render(barChar)
	}
	if len(projects) == 0 {
		return m.ui.info.Render(strings.Repeat(glyphBar, length))
	}

	var segments strings.Builder
	remaining := length
	for i, project := range projects {
		w := remaining
		if i < len(projects)-1 {
			w = int(float64(project.Duration) / float64(dur) * float64(length))
		}
		if w <= 0 {
			continue
		}
		remaining -= w
		segments.WriteString(m.tagBarStyle(m.ui.info, project.Name, tagColorScopeWeekly).Render(strings.Repeat(glyphBar, w)))
	}
	return segments.String()
}

// topProjectLines are the month's longest projects as bars.
func (m *calendarModel) topProjectLines(width int) []string {
	projects := insights.AggregateProjectDurations(m.monthReports)
	month := strings.ToUpper(localizedMonthName(m.loc, m.viewDate.Month()))
	lines := []string{m.ui.renderSection(m.loc.Text("calendar.top_projects"), month)}
	if len(projects) == 0 {
		return lines
	}
	barW := max(width-calendarProjectW-calendarDurationW-1, calendarBarMinW)
	for _, project := range projects[:min(len(projects), calendarTopProjects)] {
		lines = append(lines, m.projectBarLine(project, projects[0].Duration, "", barW, tagColorScopeTopProject))
	}
	return lines
}
