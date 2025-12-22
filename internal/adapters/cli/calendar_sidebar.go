package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const barChar = "▏"

func (m *reportModel) renderSidebar() string {
	var b strings.Builder

	b.WriteString(m.renderProductivityStats())

	remaining := m.height - 2 - 7 // 7 lines for productivity stats

	if remaining >= 17 { // 17 lines for weekly activity
		b.WriteString(m.renderWeeklyActivity())
		remaining -= 17
	}

	if remaining >= 10 { // 10 lines for peak hours
		b.WriteString(m.renderPeakHours())
		remaining -= 10
	}

	if remaining >= 4 { // At least header + 1 project
		b.WriteString(m.renderTopProjects(remaining))
	}

	return m.styles.Sidebar.Render(b.String())
}

func (m *reportModel) renderProductivityStats() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Width(40).Render("Productivity") + "\n\n")

	var totalDuration time.Duration
	activeDays := 0
	maxDailyDuration := time.Duration(0)
	daysInMonth := time.Date(m.viewDate.Year(), m.viewDate.Month()+1, 0, 0, 0, 0, 0, time.Local).Day()

	currentStreak := 0
	longestStreak := 0

	for day := 1; day <= daysInMonth; day++ {
		dur := time.Duration(0)
		if r, ok := m.monthReports[day]; ok {
			dur = r.TotalDuration
		}

		if dur > 0 {
			activeDays++
			totalDuration += dur
			if dur > maxDailyDuration {
				maxDailyDuration = dur
			}
			currentStreak++
		} else {
			if currentStreak > longestStreak {
				longestStreak = currentStreak
			}
			currentStreak = 0
		}
	}
	if currentStreak > longestStreak {
		longestStreak = currentStreak
	}

	avgDuration := time.Duration(0)
	if activeDays > 0 {
		avgDuration = totalDuration / time.Duration(activeDays)
	}

	b.WriteString(fmt.Sprintf("Total:   %s\n", m.styles.Duration.Render(totalDuration.Round(time.Minute).String())))
	b.WriteString(fmt.Sprintf("Avg/Day: %s\n", m.styles.Duration.Render(avgDuration.Round(time.Minute).String())))
	b.WriteString(fmt.Sprintf("Max/Day: %s\n", m.styles.Duration.Render(maxDailyDuration.Round(time.Minute).String())))
	b.WriteString(fmt.Sprintf("Streak:  %d days\n\n", longestStreak))
	return b.String()
}

func (m *reportModel) renderWeeklyActivity() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Width(40).Render("Weekly Activity") + "\n\n")

	weekday := int(m.currentDate.Weekday())
	if weekday == 0 {
		weekday = 7
	}

	startOfWeek := m.currentDate.AddDate(0, 0, -weekday+1)
	startOfPrevWeek := startOfWeek.AddDate(0, 0, -7)

	maxDuration := time.Duration(0)
	var weeklyDurations []time.Duration
	var prevWeeklyDurations []time.Duration

	for i := range 7 {
		day := startOfWeek.AddDate(0, 0, i)
		dur := time.Duration(0)
		if day.Month() == m.viewDate.Month() {
			if r, ok := m.monthReports[day.Day()]; ok {
				dur = r.TotalDuration
			}
		}
		weeklyDurations = append(weeklyDurations, dur)
		if dur > maxDuration {
			maxDuration = dur
		}

		// Previous week
		prevDay := startOfPrevWeek.AddDate(0, 0, i)
		prevDur := time.Duration(0)
		if prevDay.Month() == m.viewDate.Month() {
			if r, ok := m.monthReports[prevDay.Day()]; ok {
				prevDur = r.TotalDuration
			}
		}
		prevWeeklyDurations = append(prevWeeklyDurations, prevDur)
		if prevDur > maxDuration {
			maxDuration = prevDur
		}
	}

	// Render Chart
	weekdays := []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}
	for i := range 7 {
		dur := weeklyDurations[i]
		prevDur := prevWeeklyDurations[i]
		label := weekdays[i]

		// Highlight current day
		day := startOfWeek.AddDate(0, 0, i)
		dayStyle := lipgloss.NewStyle().Foreground(m.theme.SubText)
		if day.Day() == m.currentDate.Day() && day.Month() == m.currentDate.Month() {
			dayStyle = dayStyle.Foreground(m.styles.Dot.GetForeground()).Bold(true)
		}

		bar := ""
		prevBar := ""
		if maxDuration > 0 {
			width := int((float64(dur) / float64(maxDuration)) * 25)
			if width > 0 {
				bar = strings.Repeat("█", width)
			} else if dur > 0 {
				bar = barChar
			}

			prevWidth := int((float64(prevDur) / float64(maxDuration)) * 25)
			if prevWidth > 0 {
				prevBar = strings.Repeat("▒", prevWidth)
			} else if prevDur > 0 {
				prevBar = barChar
			}
		}

		b.WriteString(fmt.Sprintf("%s %s\n", dayStyle.Width(2).Render(label), lipgloss.NewStyle().Foreground(m.theme.Primary).Render(bar)))
		b.WriteString(fmt.Sprintf("   %s\n", lipgloss.NewStyle().Foreground(m.theme.Faint).Render(prevBar)))
	}
	b.WriteString("\n")
	return b.String()
}

func (m *reportModel) renderTopProjects(maxHeight int) string {
	var b strings.Builder

	// Top Projects
	b.WriteString(m.styles.Header.Width(40).Render("Top Projects") + "\n")

	projectDurations := make(map[string]time.Duration)
	for _, r := range m.monthReports {
		for p, pr := range r.ByProject {
			projectDurations[p] += pr.Duration
		}
	}

	type kv struct {
		Key   string
		Value time.Duration
	}
	var ss []kv
	for k, v := range projectDurations {
		ss = append(ss, kv{k, v})
	}
	sort.Slice(ss, func(i, j int) bool {
		return ss[i].Value > ss[j].Value
	})

	maxProjDuration := time.Duration(0)
	if len(ss) > 0 {
		maxProjDuration = ss[0].Value
	}

	maxProjects := min((maxHeight-1)/3, 5)

	for i, kv := range ss {
		if i >= maxProjects {
			break
		}

		bar := ""
		if maxProjDuration > 0 {
			width := int((float64(kv.Value) / float64(maxProjDuration)) * 20)
			if width > 0 {
				bar = strings.Repeat("█", width)
			} else if kv.Value > 0 {
				bar = "▏"
			}
		}

		b.WriteString(fmt.Sprintf("%s\n", m.styles.Project.Render(kv.Key)))
		b.WriteString(fmt.Sprintf("%s %s\n",
			lipgloss.NewStyle().Foreground(m.theme.Primary).Render(bar),
			m.styles.Duration.Render(kv.Value.Round(time.Minute).String())))
		b.WriteString("\n")
	}
	return b.String()
}

func (m *reportModel) renderPeakHours() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Width(40).Render("Peak Hours") + "\n\n")

	// Calculate hourly stats
	hourlyStats := make(map[int]time.Duration)
	for _, report := range m.monthReports {
		for _, act := range report.Activities {
			start := act.StartTime
			end := time.Now()
			if act.EndTime != nil {
				end = *act.EndTime
			}

			// Iterate through hours covered by activity
			current := start
			for current.Before(end) {
				// End of current hour
				nextHour := current.Truncate(time.Hour).Add(time.Hour)

				segmentEnd := end
				if nextHour.Before(end) {
					segmentEnd = nextHour
				}

				duration := segmentEnd.Sub(current)
				hour := current.Hour()
				hourlyStats[hour] += duration

				current = segmentEnd
			}
		}
	}

	// Find max duration for scaling
	var maxDuration time.Duration
	type hourStat struct {
		Hour     int
		Duration time.Duration
	}
	var stats []hourStat

	for h := range 24 {
		dur := hourlyStats[h]
		if dur > 0 {
			stats = append(stats, hourStat{h, dur})
			if dur > maxDuration {
				maxDuration = dur
			}
		}
	}

	// Sort by duration descending
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Duration > stats[j].Duration
	})

	// Take top 5
	count := 0
	for _, s := range stats {
		if count >= 5 {
			break
		}

		bar := ""
		if maxDuration > 0 {
			width := int((float64(s.Duration) / float64(maxDuration)) * 20)
			if width > 0 {
				bar = strings.Repeat("█", width)
			} else if s.Duration > 0 {
				bar = barChar
			}
		}

		timeStr := fmt.Sprintf("%02d:00", s.Hour)
		b.WriteString(fmt.Sprintf("%s %s\n",
			lipgloss.NewStyle().Foreground(m.theme.SubText).Width(5).Render(timeStr),
			lipgloss.NewStyle().Foreground(m.theme.Highlight).Render(bar),
		))
		count++
	}
	b.WriteString("\n")

	return b.String()
}
