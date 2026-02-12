package cli

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func NewCalendarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "Show interactive calendar view",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service := getService(cmd)
			cfg := getConfig(cmd)
			tf := getTimeFormatter(cmd)
			m := initialReportModel(service, cfg, tf)
			p := tea.NewProgram(&m)
			if _, err := p.Run(); err != nil {
				return errors.Wrap(err, "run program")
			}
			return nil
		},
	}
	return cmd
}

type reportModel struct {
	service      ports.ActivityResolver
	config       *config.Config
	timeFormat   *timeutil.Formatter // time display format (12/24 hour)
	currentDate  time.Time           // The date currently selected
	viewDate     time.Time           // The month currently being viewed
	monthReports map[int]*dto.Report // Cache for daily reports in the month (day -> report)
	viewport     viewport.Model
	ready        bool
	width        int
	height       int
	err          error
	styles       Styles
	theme        Theme
}

func initialReportModel(service ports.ActivityResolver, cfg *config.Config, tf *timeutil.Formatter) reportModel {
	now := time.Now()
	theme := GetTheme(cfg.Theme)
	return reportModel{
		service:      service,
		config:       cfg,
		timeFormat:   tf,
		currentDate:  now,
		viewDate:     now,
		monthReports: make(map[int]*dto.Report),
		styles:       InitStyles(theme),
		theme:        theme,
	}
}

func (m *reportModel) Init() tea.Cmd {
	return m.fetchMonthData
}

func (m *reportModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		var handled bool
		if cmd, handled = m.handleKeyMsg(msg); handled {
			return m, cmd
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		var detailsWidth int

		//nolint:gocritic // nested ifs for clarity
		if msg.Width >= 120 {
			detailsWidth = msg.Width - 33 - 44 - 4 // Calendar(33) + Sidebar(44) + DetailsOverhead(4)
		} else if msg.Width >= 70 {
			detailsWidth = msg.Width - 33 - 4 // Calendar(33) + DetailsOverhead(4)
		} else {
			detailsWidth = msg.Width - 4 // DetailsOverhead(4)
		}

		if !m.ready {
			m.viewport = viewport.New(detailsWidth, msg.Height-5)
			m.ready = true
		} else {
			m.viewport.Width = detailsWidth
			m.viewport.Height = msg.Height - 5
		}
		m.updateViewportContent()

	case monthDataMsg:
		m.monthReports = msg.reports
		m.updateViewportContent()

	case errMsg:
		m.err = msg.err
	}

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *reportModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}
	if !m.ready {
		return "Initializing..."
	}

	detailsView := m.renderDetails()

	if m.width >= 120 {
		calendarView := m.renderCalendar()
		sidebarView := m.renderSidebar()
		return lipgloss.JoinHorizontal(lipgloss.Top, calendarView, detailsView, sidebarView)
	} else if m.width >= 70 {
		calendarView := m.renderCalendar()
		return lipgloss.JoinHorizontal(lipgloss.Top, calendarView, detailsView)
	}

	return detailsView
}

func (m *reportModel) renderCalendar() string {
	var b strings.Builder
	now := time.Now()

	// Month Header
	header := fmt.Sprintf("%s %d", m.viewDate.Month(), m.viewDate.Year())
	b.WriteString(m.styles.Header.Render(header) + "\n\n")

	// Weekday headers
	weekdays := []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}
	for _, w := range weekdays {
		b.WriteString(m.styles.Weekday.Render(w))
	}
	b.WriteString("\n")

	// Calendar grid
	firstDay := time.Date(m.viewDate.Year(), m.viewDate.Month(), 1, 0, 0, 0, 0, time.Local)
	weekday := int(firstDay.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	weekday-- // Mon=0

	// Padding
	for range weekday {
		b.WriteString("    ")
	}

	daysInMonth := time.Date(m.viewDate.Year(), m.viewDate.Month()+1, 0, 0, 0, 0, 0, time.Local).Day()
	for day := 1; day <= daysInMonth; day++ {
		date := time.Date(m.viewDate.Year(), m.viewDate.Month(), day, 0, 0, 0, 0, time.Local)

		isToday := date.Year() == now.Year() && date.Month() == now.Month() && date.Day() == now.Day()
		isSelected := date.Year() == m.currentDate.Year() && date.Month() == m.currentDate.Month() && date.Day() == m.currentDate.Day()
		hasActivity := false
		if report, ok := m.monthReports[day]; ok && report.TotalDuration > 0 {
			hasActivity = true
		}

		str := strconv.Itoa(day)
		var cellStyle lipgloss.Style

		switch {
		case isToday:
			cellStyle = m.styles.Today
		case isSelected:
			cellStyle = m.styles.Selected
		default:
			cellStyle = m.styles.Day
			if hasActivity {
				cellStyle = cellStyle.Foreground(m.styles.Dot.GetForeground()).Bold(true)
			} else {
				cellStyle = cellStyle.Foreground(m.styles.Weekday.GetForeground())
			}
		}

		if hasActivity && (isToday || isSelected) {
			cellStyle = cellStyle.Underline(true)
		}

		b.WriteString(cellStyle.Render(str))

		weekday++
		if weekday > 6 {
			weekday = 0
			b.WriteString("\n")
		}
	}
	b.WriteString("\n\n")
	b.WriteString(
		lipgloss.NewStyle().
			Foreground(m.styles.Weekday.GetForeground()).
			Render("Use arrows to navigate:\n - 'j'/'k' to scroll details\n - 'n'/'p' for next/prev month\n - 'q' to quit"),
	)

	return m.styles.Wrapper.Render(b.String())
}

func (m *reportModel) renderDetails() string {
	var detailsWidth int

	//nolint:gocritic // nested ifs for clarity
	if m.width >= 120 {
		detailsWidth = m.width - 33 - 44 - 4
	} else if m.width >= 70 {
		detailsWidth = m.width - 33 - 4
	} else {
		detailsWidth = m.width - 4
	}

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Faint).
		Padding(0, 1).
		Width(detailsWidth).
		Height(m.height - 2).
		Render(m.viewport.View())
}

//nolint:funlen //it's more readable to keep the content generation in one place for now
func (m *reportModel) updateViewportContent() {
	day := m.currentDate.Day()
	report, ok := m.monthReports[day]

	var b strings.Builder

	// Header
	dateStr := m.currentDate.Format("Monday, 02 January 2006")
	b.WriteString(m.styles.DetailsHeader.Render(dateStr) + "\n\n")

	hasEvents := ok && report != nil && report.TotalDuration > 0

	if !hasEvents {
		b.WriteString(lipgloss.NewStyle().Foreground(m.styles.Weekday.GetForeground()).Render("No events"))
		m.viewport.SetContent(b.String())
		return
	}
	// Flatten and sort activities
	activities := make([]models.Activity, len(report.Activities))
	copy(activities, report.Activities)

	sort.Slice(activities, func(i, j int) bool {
		return activities[i].StartTime.Before(activities[j].StartTime)
	})

	for i, act := range activities {
		isLast := i == len(activities)-1

		startFormat := m.config.Calendar.TimeStartFormat
		if startFormat == "" {
			startFormat = m.timeFormat.GetDisplayFormat()
		}
		start := act.StartTime.Format(startFormat)

		// Timeline styles
		dot := "●"
		line := "│"

		// Colors
		dotStyle := m.styles.Dot
		lineStyle := m.styles.Line

		// Content
		durStr := timeutil.FormatDuration(act.Duration(), m.config.Calendar.TimeSpentFormat)
		if act.EndTime != nil {
			if m.config.Calendar.TimeEndFormat != "" {
				durStr += act.EndTime.Format(m.config.Calendar.TimeEndFormat)
			} else {
				durStr += fmt.Sprintf(" • %s", act.EndTime.Format(m.timeFormat.GetDisplayFormat()))
			}
		}

		// Row 1: Time | Dot | Project
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
			m.styles.Time.Width(9).Align(lipgloss.Right).Render(start),
			"  ",
			dotStyle.Render(dot),
			"  ",
			m.styles.Project.Render(act.Project),
		) + "\n")

		// Row 2:      | Line | Description
		if act.Description != "" {
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
				lipgloss.NewStyle().Width(9).Render(""),
				"  ",
				lineStyle.Render(line),
				"  ",
				m.styles.Desc.Render(act.Description),
			) + "\n")
		}

		// Row 3:      | Line | Duration
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(9).Render(""),
			"  ",
			lineStyle.Render(line),
			"  ",
			m.styles.Duration.Render(durStr),
		) + "\n")

		// Spacer
		if !isLast {
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
				lipgloss.NewStyle().Width(9).Render(""),
				"  ",
				lineStyle.Render(line),
			) + "\n")
		} else {
			b.WriteString("\n")
		}
	}

	totalFormat := m.config.Calendar.TimeTotalFormat
	totalDurStr := timeutil.FormatDuration(report.TotalDuration.Round(time.Minute), totalFormat)

	if m.config.Calendar.AlignDurationLeft {
		b.WriteString(lipgloss.NewStyle().
			Foreground(m.styles.Weekday.GetForeground()).
			Render(fmt.Sprintf("%s Total", totalDurStr)))
	} else {
		b.WriteString(lipgloss.NewStyle().
			Foreground(m.styles.Weekday.GetForeground()).
			Render(fmt.Sprintf("Total: %s", totalDurStr)))
	}
	b.WriteString("\n")

	// Project breakdown
	type pStat struct {
		Name     string
		Duration time.Duration
	}
	var stats []pStat
	for name, pr := range report.ByProject {
		stats = append(stats, pStat{Name: name, Duration: pr.Duration})
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Duration > stats[j].Duration
	})

	for _, s := range stats {
		b.WriteString(m.formatProjectStat(s.Name, s.Duration))
	}

	m.viewport.SetContent(b.String())
}

func (m *reportModel) formatProjectStat(name string, duration time.Duration) string {
	dur := timeutil.FormatDuration(duration, m.config.Calendar.TimeSpentFormat)
	if m.config.Calendar.AlignDurationLeft {
		return fmt.Sprintf("%s %s\n",
			m.styles.Duration.Render(dur),
			m.styles.Project.Render(name),
		)
	}
	return fmt.Sprintf("- %s: %s\n",
		m.styles.Project.Render(name),
		m.styles.Duration.Render(dur),
	)
}

// Messages and Commands

type monthDataMsg struct {
	reports map[int]*dto.Report
}

type errMsg struct{ err error }

func (m *reportModel) fetchMonthData() tea.Msg {
	// Calculate start and end of the month
	year, month, _ := m.viewDate.Date()
	startOfMonth := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	endOfMonth := startOfMonth.AddDate(0, 1, 0)

	filter := dto.ActivityFilter{
		FromDate: &startOfMonth,
		ToDate:   &endOfMonth,
	}

	// Get report for the whole month
	// Note: The service.GetReport aggregates by project.
	// We need to aggregate by DAY for the calendar view.
	// The current GetReport returns a single Report struct for the whole period.
	// It contains a list of Activities. We can process these activities here to group by day.

	report, err := m.service.GetReport(context.Background(), filter)
	if err != nil {
		return errMsg{errors.Wrap(err, "get report")}
	}

	// Group by day
	dailyReports := make(map[int]*dto.Report)

	for _, act := range report.Activities {
		day := act.StartTime.Day()

		// Handle activities spanning days? For now, assign to start day.
		// Also check if activity is within the month (it should be due to filter)
		// if act.StartTime.Month() != month {
		// 	continue
		// }

		if _, ok := dailyReports[day]; !ok {
			dailyReports[day] = &dto.Report{
				Activities: []models.Activity{},
				ByProject:  make(map[string]dto.ProjectReport),
			}
		}

		dr := dailyReports[day]
		dr.Activities = append(dr.Activities, act)
		dur := act.Duration()
		dr.TotalDuration += dur

		// Update project report for the day
		pr, ok := dr.ByProject[act.Project]
		if !ok {
			pr = dto.ProjectReport{
				ProjectName: act.Project,
				Activities:  []models.Activity{},
			}
		}
		pr.Duration += dur
		pr.Activities = append(pr.Activities, act)
		dr.ByProject[act.Project] = pr
	}

	return monthDataMsg{reports: dailyReports}
}

// getWeeklyDuration calculates the total duration for the current week (Monday to Sunday)
// based on the selected date. Fetches directly from service to handle cross-month weeks.
func (m *reportModel) getWeeklyDuration() (time.Duration, error) {
	// Find Monday of the current week (based on selected date)
	weekday := m.currentDate.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	daysFromMonday := int(weekday) - 1
	monday := time.Date(
		m.currentDate.Year(), m.currentDate.Month(), m.currentDate.Day(),
		0, 0, 0, 0, time.Local,
	).AddDate(0, 0, -daysFromMonday)

	// End of week is Sunday, or today if the week isn't complete
	sunday := monday.AddDate(0, 0, 6)
	today := time.Now()
	endDate := sunday
	if today.Before(sunday) {
		endDate = today
	}
	endDate = time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 23, 59, 59, 0, time.Local)

	report, err := m.service.GetReport(context.Background(), dto.ActivityFilter{
		FromDate: &monday,
		ToDate:   &endDate,
	})
	if err != nil {
		return 0, err
	}

	return report.TotalDuration, nil
}

func (m *reportModel) handleKeyMsg(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		return tea.Quit, true
	case "left", "h":
		m.currentDate = m.currentDate.AddDate(0, 0, -1)
		if m.currentDate.Month() != m.viewDate.Month() {
			m.viewDate = m.currentDate
			return m.fetchMonthData, true
		}
		m.updateViewportContent()
		return nil, true
	case "right", "l":
		m.currentDate = m.currentDate.AddDate(0, 0, 1)
		if m.currentDate.Month() != m.viewDate.Month() {
			m.viewDate = m.currentDate
			return m.fetchMonthData, true
		}
		m.updateViewportContent()
		return nil, true
	case "up":
		m.currentDate = m.currentDate.AddDate(0, 0, -7)
		if m.currentDate.Month() != m.viewDate.Month() {
			m.viewDate = m.currentDate
			return m.fetchMonthData, true
		}
		m.updateViewportContent()
		return nil, true
	case "down":
		m.currentDate = m.currentDate.AddDate(0, 0, 7)
		if m.currentDate.Month() != m.viewDate.Month() {
			m.viewDate = m.currentDate
			return m.fetchMonthData, true
		}
		m.updateViewportContent()
		return nil, true
	case "j":
		m.viewport.LineDown(1) //nolint:staticcheck //it's deprecated but still works
		return nil, true
	case "k":
		m.viewport.LineUp(1) //nolint:staticcheck //it's deprecated but still works
		return nil, true
	case "n": // Next month
		m.viewDate = m.viewDate.AddDate(0, 1, 0)
		m.currentDate = m.viewDate
		return m.fetchMonthData, true
	case "p": // Previous month
		m.viewDate = m.viewDate.AddDate(0, -1, 0)
		m.currentDate = m.viewDate
		return m.fetchMonthData, true
	}
	return nil, false
}
