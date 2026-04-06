package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/kriuchkov/tock/internal/app/insights"
)

const barChar = "▏"

// formatDurationCompact formats a duration as "Xh Ym" for compact display.
func formatDurationCompact(d time.Duration) string {
	d = d.Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 && m > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	} else if h > 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

func (m *calendarModel) renderSidebar() string {
	var b strings.Builder

	b.WriteString(m.renderProductivityStats())

	// Base: 7 lines for productivity stats, +3 if weekly target is configured
	productivityLines := 7
	if m.config.WeeklyTarget > 0 {
		productivityLines += 3
	}
	remaining := m.height - 2 - productivityLines

	if remaining >= 17 { // 17 lines for weekly activity
		b.WriteString(m.renderWeeklyActivity())
		remaining -= 17
	}

	if remaining >= 4 { // At least header + 1 project
		b.WriteString(m.renderTopProjects(remaining))
	}

	return m.styles.Sidebar.Render(b.String())
}

func (m *calendarModel) renderProductivityStats() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Width(40).Render(m.loc.Text("calendar.sidebar.productivity")) + "\n\n")
	daysInMonth := time.Date(m.viewDate.Year(), m.viewDate.Month()+1, 0, 0, 0, 0, 0, time.Local).Day()
	stats := insights.ComputeProductivityStats(m.monthReports, daysInMonth)
	weekly := insights.BuildWeeklyActivityData(m.dailyReports, m.currentDate)

	fmt.Fprintf(&b, m.loc.Text("calendar.sidebar.total"), m.styles.Duration.Render(stats.TotalDuration.Round(time.Minute).String()))
	fmt.Fprintf(&b, m.loc.Text("calendar.sidebar.avg_day"), m.styles.Duration.Render(stats.AvgDuration.Round(time.Minute).String()))
	fmt.Fprintf(&b, m.loc.Text("calendar.sidebar.max_day"), m.styles.Duration.Render(stats.MaxDailyDuration.Round(time.Minute).String()))
	fmt.Fprintf(&b, m.loc.Text("calendar.sidebar.streak"), stats.LongestStreak)

	// Weekly target progress (only if configured)
	if m.config.WeeklyTarget > 0 {
		b.WriteString("\n")

		weekStr := formatDurationCompact(weekly.CurrentWeekTotal)
		targetStr := formatDurationCompact(m.config.WeeklyTarget)
		fmt.Fprintf(&b, m.loc.Text("calendar.sidebar.week"), m.styles.Duration.Render(weekStr), targetStr)

		percent := float64(weekly.CurrentWeekTotal) / float64(m.config.WeeklyTarget) * 100
		barPercent := min(percent, 100)

		barWidth := m.styles.Sidebar.GetWidth() - 9
		filledWidth := int(barPercent / 100 * float64(barWidth))
		emptyWidth := barWidth - filledWidth

		bar := fmt.Sprintf("[%s%s] %.0f%%",
			lipgloss.NewStyle().Foreground(m.theme.Primary).Render(strings.Repeat("█", filledWidth)),
			strings.Repeat("░", emptyWidth),
			percent,
		)
		b.WriteString(bar + "\n")
	}

	b.WriteString("\n")
	return b.String()
}

func (m *calendarModel) renderWeeklyActivity() string {
	var b strings.Builder
	weekly := insights.BuildWeeklyActivityData(m.dailyReports, m.currentDate)

	b.WriteString(m.styles.Header.Width(40).Render(m.loc.Text("calendar.sidebar.weekly_activity")) + "\n\n")

	// Render Chart
	for i := range 7 {
		day := weekly.StartOfWeek.AddDate(0, 0, i)
		dur := weekly.CurrentWeekDurations[i]
		prevDur := weekly.PreviousWeekDurations[i]
		label := localizedWeekdayShort(m.loc, day.Weekday())

		// Highlight current day
		dayStyle := lipgloss.NewStyle().Foreground(m.theme.SubText)
		if day.Day() == m.currentDate.Day() && day.Month() == m.currentDate.Month() {
			dayStyle = dayStyle.Foreground(m.styles.Dot.GetForeground()).Bold(true)
		}

		bar := ""
		prevBar := ""
		if weekly.MaxDuration > 0 {
			width := int((float64(dur) / float64(weekly.MaxDuration)) * 25)
			if width > 0 {
				bar = strings.Repeat("█", width)
			} else if dur > 0 {
				bar = barChar
			}

			prevWidth := int((float64(prevDur) / float64(weekly.MaxDuration)) * 25)
			if prevWidth > 0 {
				prevBar = strings.Repeat("▒", prevWidth)
			} else if prevDur > 0 {
				prevBar = barChar
			}
		}

		fmt.Fprintf(&b, "%s %s\n", dayStyle.Width(2).Render(label), lipgloss.NewStyle().Foreground(m.theme.Primary).Render(bar))
		fmt.Fprintf(&b, "   %s\n", lipgloss.NewStyle().Foreground(m.theme.Faint).Render(prevBar))
	}
	b.WriteString("\n")
	return b.String()
}

func (m *calendarModel) renderTopProjects(maxHeight int) string {
	var b strings.Builder

	// Top Projects
	b.WriteString(m.styles.Header.Width(40).Render(m.loc.Text("calendar.sidebar.top_projects")) + "\n")

	projects := insights.AggregateProjectDurations(m.monthReports)

	maxProjDuration := time.Duration(0)
	if len(projects) > 0 {
		maxProjDuration = projects[0].Duration
	}

	maxProjects := min((maxHeight-1)/3, 5)

	for i, project := range projects {
		if i >= maxProjects {
			break
		}

		bar := ""
		if maxProjDuration > 0 {
			width := int((float64(project.Duration) / float64(maxProjDuration)) * 20)
			if width > 0 {
				bar = strings.Repeat("█", width)
			} else if project.Duration > 0 {
				bar = "▏"
			}
		}

		fmt.Fprintf(&b, "%s\n", m.styles.Project.Render(project.Name))
		fmt.Fprintf(&b, "%s %s\n",
			lipgloss.NewStyle().Foreground(m.theme.Primary).Render(bar),
			m.styles.Duration.Render(project.Duration.Round(time.Minute).String()))
		b.WriteString("\n")
	}
	return b.String()
}
