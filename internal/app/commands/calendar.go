package commands

import (
	"context"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/config"
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
			return runCalendarCmd(cmd)
		},
	}
	return cmd
}

func runCalendarCmd(cmd *cobra.Command) error {
	return runTUI(cmd, tabCalendar, periodDaily)
}

type calendarModel struct {
	service      ports.ActivityResolver
	config       *config.Config
	timeFormat   *timeutil.Formatter    // time display format (12/24 hour)
	currentDate  time.Time              // The date currently selected
	viewDate     time.Time              // The month currently being viewed
	monthReports map[int]*models.Report // Cache for daily reports in the month (day -> report)
	dailyReports map[string]*models.Report
	running      []models.Activity // activities running when the month was loaded
	viewport     viewport.Model    // the day column
	ready        bool
	width        int
	height       int
	layout       calendarLayout
	sel          daySelection
	monthOpen    bool // the month over the day, on terminals too narrow for both
	err          error
	tabs         tabStrip
	ui           chromeStyles
	theme        Theme
	loc          *localization.Localizer
	rem          calendarReminders
}

func initialCalendarModel(
	service ports.ActivityResolver,
	cfg *config.Config,
	tf *timeutil.Formatter,
	loc *localization.Localizer,
	tagColors map[string]models.TagColor,
) calendarModel {
	now := time.Now()
	theme := GetTheme(cfg.Theme)
	for tag, tc := range tagColors {
		if theme.TagColors == nil {
			theme.TagColors = make(map[string]TagColorStyle, len(tagColors))
		}
		ts := TagColorStyle{}
		if tc.FG != "" {
			ts.FG = lipgloss.Color(tc.FG)
		}
		if tc.BG != "" {
			ts.BG = lipgloss.Color(tc.BG)
		}
		theme.TagColors[tag] = ts
	}
	return calendarModel{
		service:      service,
		config:       cfg,
		timeFormat:   tf,
		currentDate:  now,
		viewDate:     now,
		monthReports: make(map[int]*models.Report),
		dailyReports: make(map[string]*models.Report),
		tabs:         singleTab(loc.Text("tui.tab.calendar")),
		ui:           newChromeStyles(theme),
		theme:        theme,
		loc:          loc,
	}
}

func (m *calendarModel) setTabs(tabs tabStrip) { m.tabs = tabs }

// modal reports whether the reminder form or the delete prompt is open.
func (m *calendarModel) modal() bool {
	return m.rem.form != nil || m.rem.pendingDelete != nil
}

func (m *calendarModel) refresh() tea.Cmd { return m.fetchMonthData }

// calendarTickMsg redraws the calendar each minute, so running time stays current.
type calendarTickMsg struct{}

func calendarTick() tea.Cmd {
	return tea.Every(time.Minute, func(time.Time) tea.Msg { return calendarTickMsg{} })
}

func (m *calendarModel) Init() tea.Cmd {
	return tea.Batch(m.fetchMonthData, calendarTick())
}

func (m *calendarModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if cmd, handled := m.handleKeyMsg(msg); handled {
			return m, cmd
		}

	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)

	case monthDataMsg:
		m.monthReports = msg.monthReports
		m.dailyReports = msg.dailyReports
		m.running = msg.running
		m.rem.data, m.rem.next = msg.reminders, nextMemo{}
		m.updateViewportContent()

	case calendarTickMsg:
		m.updateViewportContent()
		return m, calendarTick()

	case errMsg:
		m.err = msg.err
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// resize lays the calendar out anew for the terminal size.
func (m *calendarModel) resize(width, height int) {
	m.width, m.height = width, height
	m.layout = newCalendarLayout(width, height)
	if !m.ready {
		m.viewport = viewport.New(m.layout.dayW, m.layout.bodyH)
		m.ready = true
	} else {
		m.viewport.Width = m.layout.dayW
		m.viewport.Height = m.layout.bodyH
	}
	if !m.layout.dayOnly() {
		m.monthOpen = false
	}
	m.updateViewportContent()
}

type tagColorScope string

const (
	tagColorScopeCalendar   tagColorScope = "calendar"
	tagColorScopeWeekly     tagColorScope = "weekly_activity"
	tagColorScopeTopProject tagColorScope = "top_projects"
)

func (m *calendarModel) useTockTagColorsFor(scope tagColorScope) bool {
	tw := m.config.Timewarrior
	if tw.UseTockTagColors {
		return true
	}
	switch scope {
	case tagColorScopeCalendar:
		return tw.UseTockTagColorsCalendar
	case tagColorScopeWeekly:
		return tw.UseTockTagColorsWeeklyActivity
	case tagColorScopeTopProject:
		return tw.UseTockTagColorsTopProjects
	default:
		return false
	}
}

func (m *calendarModel) effectiveTagColor(name string, scope tagColorScope) (TagColorStyle, bool) {
	if m.useTockTagColorsFor(scope) {
		if c, ok := m.config.Theme.TagColors[name]; ok && c != "" {
			return TagColorStyle{FG: lipgloss.Color(c)}, true
		}
		return TagColorStyle{}, false
	}
	ts, ok := m.theme.TagColors[name]
	return ts, ok
}

// tagColorStyle returns a lipgloss style with fg/bg applied from TagColors when present.
func (m *calendarModel) tagColorStyle(base lipgloss.Style, name string, scope tagColorScope) lipgloss.Style {
	ts, ok := m.effectiveTagColor(name, scope)
	if !ok {
		return base
	}
	if ts.FG != "" {
		base = base.Foreground(ts.FG)
	}
	if ts.BG != "" {
		base = base.Background(ts.BG)
	}
	return base
}

// tagBarStyle returns a lipgloss style suitable for a solid bar (█ chars).
// BG is preferred as the foreground color (it's the "accent" of the pair);
// FG is used only when BG is absent; falls back to base if neither is set.
func (m *calendarModel) tagBarStyle(base lipgloss.Style, name string, scope tagColorScope) lipgloss.Style {
	ts, ok := m.effectiveTagColor(name, scope)
	if !ok {
		return base
	}
	if ts.BG != "" {
		return base.Foreground(ts.BG)
	}
	if ts.FG != "" {
		return base.Foreground(ts.FG)
	}
	return base
}

// Messages and Commands

type monthDataMsg struct {
	monthReports map[int]*models.Report
	dailyReports map[string]*models.Report
	running      []models.Activity
	reminders    reminderData
}

type errMsg struct{ err error }

func (m *calendarModel) fetchMonthData() tea.Msg {
	// Calculate start and end of the month
	year, month, _ := m.viewDate.Date()
	startOfMonth := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	endOfMonth := startOfMonth.AddDate(0, 1, 0)
	fetchStart := startOfMonth.AddDate(0, 0, -14)
	fetchEnd := endOfMonth.AddDate(0, 0, 14)

	filter := models.ActivityFilter{
		FromDate: &fetchStart,
		ToDate:   &fetchEnd,
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

	reminders, err := m.fetchReminders(context.Background(), fetchStart, fetchEnd)
	if err != nil {
		return errMsg{err}
	}

	var running []models.Activity
	for _, activity := range report.Activities {
		if activity.EndTime == nil {
			running = append(running, activity)
		}
	}

	data := insights.BuildMonthData(report.Activities, year, month, time.Now())
	return monthDataMsg{monthReports: data.MonthReports, dailyReports: data.DailyReports, running: running, reminders: reminders}
}

func (m *calendarModel) reportForDate(date time.Time) (*models.Report, bool) {
	report, ok := m.dailyReports[insights.DateKey(date)]
	return report, ok
}

// handleKeyMsg routes a key to the reminder form or prompt, the selected row, then the calendar.
func (m *calendarModel) handleKeyMsg(msg tea.KeyMsg) (tea.Cmd, bool) {
	if cmd, handled := m.handleReminderKey(msg); handled {
		return cmd, true
	}
	if m.handleSelectionKey(msg) {
		return nil, true
	}
	return m.handleNavigationKey(msg)
}

// handleNavigationKey moves through days, weeks and months, and quits.
func (m *calendarModel) handleNavigationKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "q", keyCtrlC, keyEsc:
		return tea.Quit, true
	case keyLeft, "h":
		return m.moveDate(m.currentDate.AddDate(0, 0, -1)), true
	case keyRight, "l":
		return m.moveDate(m.currentDate.AddDate(0, 0, 1)), true
	case "up":
		return m.moveDate(m.currentDate.AddDate(0, 0, -7)), true
	case keyDown:
		return m.moveDate(m.currentDate.AddDate(0, 0, 7)), true
	case "n": // Next month
		m.viewDate = m.viewDate.AddDate(0, 1, 0)
		m.currentDate = m.viewDate
		m.sel = daySelection{}
		return m.fetchMonthData, true
	case "p": // Previous month
		m.viewDate = m.viewDate.AddDate(0, -1, 0)
		m.currentDate = m.viewDate
		m.sel = daySelection{}
		return m.fetchMonthData, true
	}
	return nil, false
}

// moveDate selects another day, loading its month when it is not on screen.
func (m *calendarModel) moveDate(date time.Time) tea.Cmd {
	m.currentDate = date
	m.sel = daySelection{}
	if date.Year() != m.viewDate.Year() || date.Month() != m.viewDate.Month() {
		m.viewDate = date
		return m.fetchMonthData
	}
	m.updateViewportContent()
	return nil
}
