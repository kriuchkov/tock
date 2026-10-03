package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type listPeriod int

const (
	periodDaily listPeriod = iota
	periodWeekly
	periodMonthly
	periodYearly
)

// CLI argument spellings for each period, shared with NewListCmd's ValidArgs.
const (
	periodArgDaily   = "daily"
	periodArgWeekly  = "weekly"
	periodArgMonthly = "monthly"
	periodArgYearly  = "yearly"
)

// Vertical layout of the period view, used to size the table from the terminal height.
const (
	// defaultPeriodTableHeight is used until the first tea.WindowSizeMsg arrives.
	defaultPeriodTableHeight = 15
	// periodViewChromeLines counts the lines View renders around the table:
	// title, blank, blank, total line, and the two-line help text.
	periodViewChromeLines = 6
	// periodViewHorizontalPadding is the width reserved around the table.
	periodViewHorizontalPadding = 4
)

// parseListPeriod maps the optional CLI argument to a period; an empty argument defaults to daily.
func parseListPeriod(arg string) (listPeriod, bool) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "", periodArgDaily, "day", "d":
		return periodDaily, true
	case periodArgWeekly, "week", "w":
		return periodWeekly, true
	case periodArgMonthly, "month", "m":
		return periodMonthly, true
	case periodArgYearly, "year", "y":
		return periodYearly, true
	default:
		return periodDaily, false
	}
}

var runListPeriodProgram = func(model listPeriodModel) error {
	program := tea.NewProgram(&model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return errors.Wrap(err, "run program")
	}
	return nil
}

// periodBucket holds the per-project breakdown for one sub-unit of a period
// (a day for weekly/monthly, a month for yearly).
type periodBucket struct {
	date     time.Time
	projects []insights.ProjectDuration
	total    time.Duration
}

type listPeriodModel struct {
	service ports.ActivityResolver
	loc     *localization.Localizer
	period  listPeriod
	anchor  time.Time // A date within the currently displayed period.
	buckets []periodBucket
	total   time.Duration
	table   table.Model
	styles  Styles
	err     error
	width   int
	height  int
}

func initialListPeriodModel(
	service ports.ActivityResolver,
	cfg *config.Config,
	loc *localization.Localizer,
	period listPeriod,
) listPeriodModel {
	m := listPeriodModel{
		service: service,
		loc:     loc,
		period:  period,
		anchor:  time.Now(),
		styles:  InitStyles(GetTheme(cfg.Theme)),
	}
	m.initTable()
	m.reload()
	return m
}

func (m *listPeriodModel) Init() tea.Cmd {
	return nil
}

func (m *listPeriodModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", keyCtrlC, "esc":
			return m, tea.Quit
		case keyLeft, "h":
			m.anchor = shiftPeriod(m.period, m.anchor, -1)
			m.reload()
		case keyRight, "l":
			m.anchor = shiftPeriod(m.period, m.anchor, 1)
			m.reload()
		case "up", "k", keyDown, "j":
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetWidth(msg.Width - periodViewHorizontalPadding)
		m.table.SetHeight(fitTableHeight(m.height, periodViewChromeLines, defaultPeriodTableHeight))
	}
	return m, nil
}

func (m *listPeriodModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}

	header := m.styles.Header.Render(m.loc.Format("list.period.header", m.periodTitle()))
	help := "\n" + m.loc.Text("list.period.help")

	if len(m.buckets) == 0 {
		empty := lipgloss.NewStyle().Faint(true).Render(m.loc.Text("list.period.empty"))
		return lipgloss.JoinVertical(lipgloss.Left, header, "", empty, help)
	}

	totalLine := lipgloss.NewStyle().Bold(true).Render(m.loc.Format("list.period.total", formatDurationCompact(m.total)))
	return lipgloss.JoinVertical(lipgloss.Left, header, "", m.table.View(), "", totalLine, help)
}

func (m *listPeriodModel) initTable() {
	t := table.New(
		table.WithColumns(m.columns()),
		table.WithFocused(true),
		table.WithHeight(defaultPeriodTableHeight),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)
	m.table = t
}

func (m *listPeriodModel) columns() []table.Column {
	first := m.loc.Text("list.period.table.date")
	if m.period == periodYearly {
		first = m.loc.Text("list.period.table.month")
	}
	return []table.Column{
		{Title: first, Width: 16},
		{Title: m.loc.Text("list.table.project"), Width: 28},
		{Title: m.loc.Text("list.table.duration"), Width: 12},
	}
}

// reload fetches the activities for the current period and rebuilds the table.
func (m *listPeriodModel) reload() {
	start, end := periodRange(m.period, m.anchor)
	report, err := m.service.GetReport(context.Background(), models.ActivityFilter{FromDate: &start, ToDate: &end})
	if err != nil {
		m.err = errors.Wrap(err, "get report")
		return
	}

	m.err = nil
	m.buckets, m.total = buildPeriodBuckets(m.period, m.anchor, report.Activities, time.Now())
	m.table.SetRows(m.buildRows())
	m.table.SetCursor(0)
}

func (m *listPeriodModel) buildRows() []table.Row {
	var rows []table.Row
	for _, bucket := range m.buckets {
		label := m.bucketLabel(bucket.date)
		for i, project := range bucket.projects {
			cell := ""
			if i == 0 {
				cell = label
			}
			rows = append(rows, table.Row{cell, project.Name, formatDurationCompact(project.Duration)})
		}
		if len(bucket.projects) > 1 {
			rows = append(rows, table.Row{"", m.loc.Text("list.period.subtotal"), formatDurationCompact(bucket.total)})
		}
	}
	return rows
}

func (m *listPeriodModel) bucketLabel(date time.Time) string {
	if m.period == periodYearly {
		return localizedMonthName(m.loc, date.Month())
	}
	return fmt.Sprintf("%s %02d %s",
		localizedWeekdayShort(m.loc, date.Weekday()), date.Day(), localizedMonthShortName(m.loc, date.Month()))
}

func (m *listPeriodModel) periodTitle() string {
	switch m.period {
	case periodWeekly:
		start, end := periodRange(m.period, m.anchor)
		last := end.AddDate(0, 0, -1)
		return m.loc.Format("list.period.week_of", fmt.Sprintf("%02d %s – %02d %s %d",
			start.Day(), localizedMonthShortName(m.loc, start.Month()),
			last.Day(), localizedMonthShortName(m.loc, last.Month()), last.Year()))
	case periodYearly:
		return strconv.Itoa(m.anchor.Year())
	case periodDaily, periodMonthly:
		return formatLocalizedMonthYear(m.loc, m.anchor)
	default:
		return formatLocalizedMonthYear(m.loc, m.anchor)
	}
}

// periodRange returns the half-open window [start, end) covering the period around anchor.
func periodRange(period listPeriod, anchor time.Time) (time.Time, time.Time) {
	switch period {
	case periodWeekly:
		start := startOfWeek(anchor)
		return start, start.AddDate(0, 0, 7)
	case periodYearly:
		start := time.Date(anchor.Year(), time.January, 1, 0, 0, 0, 0, time.Local)
		return start, start.AddDate(1, 0, 0)
	case periodDaily, periodMonthly:
		start := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.Local)
		return start, start.AddDate(0, 1, 0)
	default:
		start := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.Local)
		return start, start.AddDate(0, 1, 0)
	}
}

func startOfWeek(date time.Time) time.Time {
	weekday := int(date.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -(weekday - 1))
}

func buildPeriodBuckets(
	period listPeriod,
	anchor time.Time,
	activities []models.Activity,
	now time.Time,
) ([]periodBucket, time.Duration) {
	if period == periodYearly {
		return buildYearlyBuckets(anchor, activities, now)
	}
	return buildDailyBuckets(period, anchor, activities, now)
}

func buildDailyBuckets(
	period listPeriod,
	anchor time.Time,
	activities []models.Activity,
	now time.Time,
) ([]periodBucket, time.Duration) {
	daily := insights.BuildDailyReports(activities, now)
	start, end := periodRange(period, anchor)

	var (
		buckets []periodBucket
		total   time.Duration
	)
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		report, ok := daily[insights.DateKey(day)]
		if !ok || report.TotalDuration == 0 {
			continue
		}
		buckets = append(buckets, periodBucket{
			date:     day,
			projects: insights.SortedProjectDurations(report),
			total:    report.TotalDuration,
		})
		total += report.TotalDuration
	}
	return buckets, total
}

func buildYearlyBuckets(anchor time.Time, activities []models.Activity, now time.Time) ([]periodBucket, time.Duration) {
	monthly := insights.BuildMonthlyReports(activities, anchor.Year(), now)

	var (
		buckets []periodBucket
		total   time.Duration
	)
	for month := time.January; month <= time.December; month++ {
		report, ok := monthly[month]
		if !ok || report.TotalDuration == 0 {
			continue
		}
		buckets = append(buckets, periodBucket{
			date:     time.Date(anchor.Year(), month, 1, 0, 0, 0, 0, time.Local),
			projects: insights.SortedProjectDurations(report),
			total:    report.TotalDuration,
		})
		total += report.TotalDuration
	}
	return buckets, total
}

func shiftPeriod(period listPeriod, anchor time.Time, direction int) time.Time {
	switch period {
	case periodWeekly:
		return startOfWeek(anchor).AddDate(0, 0, 7*direction)
	case periodYearly:
		return time.Date(anchor.Year()+direction, time.January, 1, 0, 0, 0, 0, time.Local)
	case periodDaily, periodMonthly:
		return time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.Local).AddDate(0, direction, 0)
	default:
		return time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.Local).AddDate(0, direction, 0)
	}
}
