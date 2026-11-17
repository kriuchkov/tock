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

// Columns of the period rows.
const (
	periodLabelW   = 14 // "Mon 16 Nov", "September"
	periodProjectW = 22
	periodTimeW    = 8
	// periodBarMaxW keeps the bars short of the times on a wide terminal.
	periodBarMaxW = 48
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

// periodRow is a line of the period view: a project's time in a bucket, or the bucket's subtotal.
// The first line of a bucket carries its label.
type periodRow struct {
	label    string
	project  string
	duration time.Duration
	subtotal bool
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
	rows    []periodRow
	cursor  int
	offset  int
	page    int // the rows on screen at the last view
	ui      chromeStyles
	tabs    tabStrip
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
	return newListPeriodModel(service, cfg, loc, period, time.Now())
}

// newListPeriodModel sums up the week, the month or the year around anchor.
func newListPeriodModel(
	service ports.ActivityResolver,
	cfg *config.Config,
	loc *localization.Localizer,
	period listPeriod,
	anchor time.Time,
) listPeriodModel {
	m := listPeriodModel{
		service: service,
		loc:     loc,
		period:  period,
		anchor:  anchor,
		ui:      newChromeStyles(GetTheme(cfg.Theme)),
		tabs:    singleTab(loc.Text("tui.tab.list")),
	}
	m.reload()
	return m
}

func (m *listPeriodModel) Init() tea.Cmd {
	return nil
}

func (m *listPeriodModel) setTabs(tabs tabStrip) { m.tabs = tabs }

func (m *listPeriodModel) modal() bool { return false }

func (m *listPeriodModel) refresh() tea.Cmd {
	m.reload()
	return nil
}

func (m *listPeriodModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", keyCtrlC, keyEsc:
			return m, tea.Quit
		case keyLeft, "h":
			m.anchor = shiftPeriod(m.period, m.anchor, -1)
			m.reload()
		case keyRight, "l":
			m.anchor = shiftPeriod(m.period, m.anchor, 1)
			m.reload()
		default:
			m.cursor, _ = moveCursor(msg.String(), m.cursor, len(m.rows), m.page)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

func (m *listPeriodModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}

	top := m.ui.renderHeader(m.header(), m.width) + "\n" + listViewLine(m.ui, m.loc, m.period) + "\n"
	bottom := "\n" + m.footer()
	return renderScreen(top, m.body(m.height-lipgloss.Height(top)-lipgloss.Height(bottom)), bottom, m.width, m.height)
}

// body is the line of column names and the rows, scrolled to the selected one.
func (m *listPeriodModel) body(height int) string {
	if len(m.rows) == 0 {
		return " " + m.ui.muted.Render(m.loc.Text("list.period.empty"))
	}
	first := "list.period.table.date"
	if m.period == periodYearly {
		first = "list.period.table.month"
	}
	head := func(key string, w int, right bool) rowCell {
		return rowCell{text: strings.ToUpper(m.loc.Text(key)), style: m.ui.muted, width: w, right: right}
	}
	lines := []string{m.ui.renderRow("", m.ui.muted, []rowCell{
		head(first, periodLabelW, false), head("list.table.project", periodProjectW, false), {},
		head("list.table.time", periodTimeW, true),
	}, false, m.width)}

	var longest time.Duration
	for _, row := range m.rows {
		if !row.subtotal {
			longest = max(longest, row.duration)
		}
	}
	rowsH := len(m.rows)
	if height > 1 {
		rowsH = height - 1
	}
	m.offset, m.page = scrollOffset(m.offset, m.cursor, rowsH, len(m.rows)), rowsH
	for i := m.offset; i < min(len(m.rows), m.offset+rowsH); i++ {
		lines = append(lines, m.renderPeriodRow(m.rows[i], i == m.cursor, longest))
	}
	return strings.Join(lines, "\n")
}

// renderPeriodRow is a project's time with a bar against the longest one, or a muted subtotal.
func (m *listPeriodModel) renderPeriodRow(row periodRow, selected bool, longest time.Duration) string {
	label := rowCell{text: row.label, style: m.ui.text, width: periodLabelW}
	if row.subtotal {
		return m.ui.renderRow("", m.ui.muted, []rowCell{
			label, {text: m.loc.Text("list.period.subtotal"), style: m.ui.muted, width: periodProjectW}, {},
			{text: formatHM(row.duration), style: m.ui.mutedBold, width: periodTimeW, right: true},
		}, selected, m.width)
	}
	barW := min(m.width-rowGutterW-periodLabelW-periodProjectW-periodTimeW-1, periodBarMaxW)
	bar := ""
	if barW > 0 {
		bar = m.ui.info.Render(strings.Repeat(glyphBar, max(barLength(row.duration, longest, barW), 1)))
	}
	return m.ui.renderRow("", m.ui.muted, []rowCell{
		label, {text: row.project, style: m.ui.text, width: periodProjectW}, {styled: bar},
		{text: formatHM(row.duration), style: m.ui.text, width: periodTimeW, right: true},
	}, selected, m.width)
}

// header names the period; the ruler has its total.
func (m *listPeriodModel) header() screenHeader {
	total := m.ui.muted.Render(m.loc.Format("list.period.total", formatHM(m.total)))
	return screenHeader{
		tabs:    m.tabs.names,
		active:  m.tabs.active,
		title:   m.periodTitle(),
		hint:    "←→",
		summary: []rulerItem{{long: total, short: total}},
	}
}

func (m *listPeriodModel) footer() string {
	key := func(k, action string) footerKey { return footerKey{key: k, action: m.loc.Text(action)} }
	step := "tui.key.month"
	switch m.period {
	case periodWeekly:
		step = "tui.key.week"
	case periodYearly:
		step = "tui.key.year"
	case periodDaily, periodMonthly:
	}
	keys := []footerKey{key("←→", step), key("↑↓", "tui.key.row"), key(keyTab, "tui.key.view"), key("q", "tui.key.quit")}
	return m.ui.renderFooter(m.tabs.withTabs(keys, m.loc.Text("tui.key.tabs")), "", m.width)
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
	m.rows = m.buildRows()
	m.cursor, m.offset = 0, 0
}

func (m *listPeriodModel) buildRows() []periodRow {
	var rows []periodRow
	for _, bucket := range m.buckets {
		label := m.bucketLabel(bucket.date)
		for i, project := range bucket.projects {
			row := periodRow{project: project.Name, duration: project.Duration}
			if i == 0 {
				row.label = label
			}
			rows = append(rows, row)
		}
		if len(bucket.projects) > 1 {
			rows = append(rows, periodRow{duration: bucket.total, subtotal: true})
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
		return formatDayRange(m.loc, start, end.AddDate(0, 0, -1), time.Now())
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
