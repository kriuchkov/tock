package cli

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"

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
			m := initialReportModel(service)
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
	currentDate  time.Time           // The date currently selected
	viewDate     time.Time           // The month currently being viewed
	monthReports map[int]*dto.Report // Cache for daily reports in the month (day -> report)
	viewport     viewport.Model
	ready        bool
	width        int
	height       int
	err          error
}

func initialReportModel(service ports.ActivityResolver) reportModel {
	now := time.Now()
	return reportModel{
		service:      service,
		currentDate:  now,
		viewDate:     now,
		monthReports: make(map[int]*dto.Report),
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

		detailsWidth := msg.Width - 36
		if msg.Width > 120 {
			detailsWidth = msg.Width - 36 - 44
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

	calendarView := m.renderCalendar()
	detailsView := m.renderDetails()

	if m.width > 120 {
		sidebarView := m.renderSidebar()
		return lipgloss.JoinHorizontal(lipgloss.Top, calendarView, detailsView, sidebarView)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, calendarView, detailsView)
}

// Styles.
var (
	colorRed   = lipgloss.Color("196")
	colorBlue  = lipgloss.Color("63")
	colorGrey  = lipgloss.Color("240")
	colorLight = lipgloss.Color("255")
	colorSub   = lipgloss.Color("248")
	colorDot   = lipgloss.Color("214")

	styleWrapper = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorGrey).
			Padding(0, 1).
			MarginRight(1)

	styleHeader = lipgloss.NewStyle().
			Foreground(colorLight).
			Bold(true).
			Align(lipgloss.Center).
			Width(28)

	styleWeekday = lipgloss.NewStyle().
			Foreground(colorSub).
			Width(4).
			Align(lipgloss.Center)

	styleDay = lipgloss.NewStyle().
			Width(4).
			Align(lipgloss.Center)

	styleToday = styleDay.
			Foreground(lipgloss.Color("255")).
			Background(colorRed).
			Bold(true)

	styleSelected = styleDay.
			Foreground(lipgloss.Color("255")).
			Background(colorBlue)

	styleDetailsHeader = lipgloss.NewStyle().
				Foreground(colorLight).
				Bold(true).
				Border(lipgloss.NormalBorder(), false, false, true, false).
				BorderForeground(colorGrey).
				Width(100)

	styleTime = lipgloss.NewStyle().
			Foreground(colorSub).
			Width(12)

	styleProject = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)

	styleDesc = lipgloss.NewStyle().
			Foreground(colorLight)

	styleDuration = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")) // Orange/Gold

	styleSidebar = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorGrey).
			Padding(0, 1).
			Width(40)
)

func (m *reportModel) renderCalendar() string {
	var b strings.Builder
	now := time.Now()

	// Month Header
	header := fmt.Sprintf("%s %d", m.viewDate.Month(), m.viewDate.Year())
	b.WriteString(styleHeader.Render(header) + "\n\n")

	// Weekday headers
	weekdays := []string{"Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"}
	for _, w := range weekdays {
		b.WriteString(styleWeekday.Render(w))
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
			cellStyle = styleToday
		case isSelected:
			cellStyle = styleSelected
		default:
			cellStyle = styleDay
			if hasActivity {
				cellStyle = cellStyle.Foreground(colorDot).Bold(true)
			} else {
				cellStyle = cellStyle.Foreground(colorSub)
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
			Foreground(colorSub).
			Render("Use arrows to navigate:\n - 'j'/'k' to scroll details\n - 'n'/'p' for next/prev month\n - 'q' to quit"),
	)

	return styleWrapper.Render(b.String())
}

func (m *reportModel) renderDetails() string {
	detailsWidth := m.width - 36
	if m.width > 120 {
		detailsWidth = m.width - 36 - 44
	}

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(colorGrey).
		Padding(0, 1).
		Width(detailsWidth).
		Height(m.height - 2).
		Render(m.viewport.View())
}

func (m *reportModel) updateViewportContent() {
	day := m.currentDate.Day()
	report, ok := m.monthReports[day]

	var b strings.Builder

	// Header
	dateStr := m.currentDate.Format("Monday, 02 January 2006")
	b.WriteString(styleDetailsHeader.Render(dateStr) + "\n\n")

	hasEvents := ok && report != nil && report.TotalDuration > 0

	if !hasEvents {
		b.WriteString(lipgloss.NewStyle().Foreground(colorSub).Render("No events"))
		m.viewport.SetContent(b.String())
		return
	}
	// Flatten and sort activities
	var activities []models.Activity
	for _, pr := range report.ByProject {
		activities = append(activities, pr.Activities...)
	}
	sort.Slice(activities, func(i, j int) bool {
		return activities[i].StartTime.Before(activities[j].StartTime)
	})

	for i, act := range activities {
		isLast := i == len(activities)-1
		start := act.StartTime.Format("15:04")

		// Timeline styles
		dot := "●"
		line := "│"

		// Colors
		dotStyle := lipgloss.NewStyle().Foreground(colorBlue)
		lineStyle := lipgloss.NewStyle().Foreground(colorGrey)

		// Content
		durStr := act.Duration().Round(time.Minute).String()
		if act.EndTime != nil {
			durStr += fmt.Sprintf(" • %s", act.EndTime.Format("15:04"))
		}

		// Row 1: Time | Dot | Project
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
			styleTime.Width(6).Align(lipgloss.Right).Render(start),
			"  ",
			dotStyle.Render(dot),
			"  ",
			styleProject.Render(act.Project),
		) + "\n")

		// Row 2:      | Line | Description
		if act.Description != "" {
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
				lipgloss.NewStyle().Width(6).Render(""),
				"  ",
				lineStyle.Render(line),
				"  ",
				styleDesc.Render(act.Description),
			) + "\n")
		}

		// Row 3:      | Line | Duration
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(6).Render(""),
			"  ",
			lineStyle.Render(line),
			"  ",
			styleDuration.Render(durStr),
		) + "\n")

		// Spacer
		if !isLast {
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
				lipgloss.NewStyle().Width(6).Render(""),
				"  ",
				lineStyle.Render(line),
			) + "\n")
		} else {
			b.WriteString("\n")
		}
	}

	totalDur := report.TotalDuration.Round(time.Minute)
	b.WriteString(lipgloss.NewStyle().Foreground(colorSub).Render(fmt.Sprintf("Total: %s", totalDur)))
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
		b.WriteString(fmt.Sprintf("- %s: %s\n",
			styleProject.Render(s.Name),
			styleDuration.Render(s.Duration.Round(time.Minute).String()),
		))
	}

	m.viewport.SetContent(b.String())
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
		if act.StartTime.Month() != month {
			continue
		}

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
