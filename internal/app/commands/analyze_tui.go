package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"
)

const (
	// analyzeTwoColumnMin is the width from which the projects and weekdays sit beside the analysis.
	analyzeTwoColumnMin = 100
	analyzeLeftW        = 50
	analyzeLabelW       = 20
	analyzeCountW       = 5
	analyzeTopProjects  = 8
	// analyzeChromeH is the header, the ruler, the view line and a blank line above the body,
	// and a blank line and the footer under it.
	analyzeChromeH = 6
)

// analyzeWindows are the days the analyze tab looks back over; tab switches between them.
func analyzeWindows() []int {
	return []int{7, analyzeDefaultDays, 90, 365}
}

// analyzeDataMsg is the analysis of the last days days.
type analyzeDataMsg struct {
	days     int
	report   *models.Report
	stats    insights.Stats
	weekdays [7]time.Duration // Monday first
	err      error
}

// analyzeModel is the analyze tab: the patterns tock analyze prints, next to the time per
// project and per weekday, over the last 7, 30, 90 or 365 days.
type analyzeModel struct {
	service  ports.ActivityResolver
	tf       *timeutil.Formatter
	loc      *localization.Localizer
	ui       chromeStyles
	tabs     tabStrip
	window   int
	data     *analyzeDataMsg
	viewport viewport.Model
	width    int
	height   int
}

func newAnalyzeModel(service ports.ActivityResolver, tf *timeutil.Formatter, loc *localization.Localizer, theme Theme) *analyzeModel {
	window := 0
	for i, days := range analyzeWindows() {
		if days == analyzeDefaultDays {
			window = i
		}
	}
	return &analyzeModel{
		service:  service,
		tf:       tf,
		loc:      loc,
		ui:       newChromeStyles(theme),
		tabs:     singleTab(loc.Text("tui.tab.analyze")),
		window:   window,
		viewport: viewport.New(0, 0),
	}
}

func (m *analyzeModel) days() int {
	return analyzeWindows()[m.window]
}

func (m *analyzeModel) Init() tea.Cmd { return m.load() }

func (m *analyzeModel) setTabs(tabs tabStrip) { m.tabs = tabs }

func (m *analyzeModel) modal() bool { return false }

func (m *analyzeModel) refresh() tea.Cmd { return m.load() }

// load reads and analyzes the activities of the window shown.
func (m *analyzeModel) load() tea.Cmd {
	days, service := m.days(), m.service
	return func() tea.Msg {
		now := time.Now()
		report, err := analysisReport(context.Background(), service, days, now)
		if err != nil {
			return analyzeDataMsg{days: days, err: err}
		}
		msg := analyzeDataMsg{days: days, report: report}
		if len(report.Activities) > 0 {
			msg.stats = insights.AnalyzeActivities(report.Activities)
			msg.weekdays = weekdayTotals(report.Activities, now.AddDate(0, 0, -days), now)
		}
		return msg
	}
}

// weekdayTotals is the time tracked on each weekday from one day to another, Monday first.
func weekdayTotals(activities []models.Activity, from, to time.Time) [7]time.Duration {
	var totals [7]time.Duration
	daily := insights.BuildDailyReports(activities, to)
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if report, ok := daily[insights.DateKey(day)]; ok && report != nil {
			totals[(int(day.Weekday())+6)%7] += report.TotalDuration
		}
	}
	return totals
}

func (m *analyzeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.viewport.Width, m.viewport.Height = msg.Width, max(msg.Height-analyzeChromeH, 1)
		m.updateContent()
	case analyzeDataMsg:
		if msg.days == m.days() {
			m.data = &msg
			m.updateContent()
		}
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *analyzeModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	count := len(analyzeWindows())
	switch msg.String() {
	case "q", keyEsc:
		return tea.Quit
	case keyTab:
		m.window = (m.window + 1) % count
	case keyShiftTab:
		m.window = (m.window + count - 1) % count
	default:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return cmd
	}
	m.data = nil
	m.updateContent()
	return m.load()
}

func (m *analyzeModel) View() string {
	labels := make([]string, 0, len(analyzeWindows()))
	for _, days := range analyzeWindows() {
		labels = append(labels, m.loc.Format("analyze.tui.days", days))
	}
	top := m.ui.renderHeader(m.header(), m.width) + "\n" +
		m.ui.renderViewLine(m.loc.Text("tui.view"), labels, m.window, keyTab) + "\n"
	return renderScreen(top, m.viewport.View(), "\n"+m.footer(), m.width, m.height)
}

func (m *analyzeModel) header() screenHeader {
	h := screenHeader{
		tabs:   m.tabs.names,
		active: m.tabs.active,
		title:  m.loc.Format("analyze.tui.title", m.days()),
		hint:   keyTab,
	}
	if m.data != nil && m.data.report != nil && len(m.data.report.Activities) > 0 {
		total := formatHM(m.data.report.TotalDuration)
		h.summary = []rulerItem{{
			long:  m.ui.muted.Render(m.loc.Format("analyze.tui.summary", total, len(m.data.report.Activities))),
			short: m.ui.muted.Render(total),
		}}
	}
	return h
}

func (m *analyzeModel) footer() string {
	key := func(k, action string) footerKey { return footerKey{key: k, action: m.loc.Text(action)} }
	keys := []footerKey{key(keyTab, "tui.key.period")}
	if m.viewport.TotalLineCount() > m.viewport.Height {
		keys = append(keys, key("↑↓", "tui.key.scroll"))
	}
	keys = append(keys, key("q", "tui.key.quit"))
	return m.ui.renderFooter(m.tabs.withTabs(keys, m.loc.Text("tui.key.tabs")), "", m.width)
}

// updateContent lays the analysis out for the width: two columns from analyzeTwoColumnMin, else one.
func (m *analyzeModel) updateContent() {
	m.viewport.SetContent(m.content())
}

func (m *analyzeModel) content() string {
	switch {
	case m.data == nil:
		return ""
	case m.data.err != nil:
		return " " + m.ui.danger.Render(fmt.Sprintf("Error: %v", m.data.err))
	case m.data.report == nil || len(m.data.report.Activities) == 0:
		return " " + m.ui.muted.Render(m.loc.Text("analyze.empty"))
	}

	if m.width < analyzeTwoColumnMin {
		width := max(m.width-2, 1)
		lines := m.analysisLines(width)
		lines = append(lines, "")
		lines = append(lines, m.projectLines(width)...)
		lines = append(lines, "")
		lines = append(lines, m.weekdayLines(width)...)
		return indentLines(lines)
	}

	left := m.analysisLines(analyzeLeftW - 1)
	rightW := m.width - analyzeLeftW - len([]rune(columnRule))
	right := m.projectLines(rightW - 1)
	right = append(right, "")
	right = append(right, m.weekdayLines(rightW-1)...)
	height := max(len(left), len(right))
	return lipgloss.JoinHorizontal(lipgloss.Top,
		fitBlock(indentLines(left), analyzeLeftW, height),
		m.ui.columnRules(height),
		fitBlock(strings.Join(right, "\n"), rightW, height),
	)
}

// analysisLines are the sections of tock analyze: focus, rhythm, switching and session lengths.
func (m *analyzeModel) analysisLines(width int) []string {
	stats := m.data.stats
	score := lipgloss.NewStyle().Foreground(deepWorkColor(m.ui.theme, stats.DeepWorkScore))
	barW := max(width-1, calendarBarMinW)
	filled := barLength(int(stats.DeepWorkScore), 100, barW)

	format := m.tf.GetDisplayFormat()
	peak := time.Date(2000, 1, 1, stats.PeakHour, 0, 0, 0, time.Local)
	lines := []string{
		m.ui.renderSection(m.loc.Text("analyze.section.focus")),
		m.row("analyze.label.deep_work", score.Bold(true).Render(fmt.Sprintf("%.1f%%", stats.DeepWorkScore))),
		score.Render(strings.Repeat(glyphBar, filled)) + m.ui.faint.Render(strings.Repeat(glyphTrack, barW-filled)),
		m.row("analyze.label.avg_session", m.ui.bold.Render(formatHM(stats.AvgSessionDuration))),
		"",
		m.ui.renderSection(m.loc.Text("analyze.section.chronotype")),
		m.row("analyze.label.type", m.ui.bold.Render(withoutTrailingSymbols(stats.Chronotype))),
		m.row("analyze.label.peak_hour", m.ui.bold.Render(peak.Format(format)+"–"+peak.Add(time.Hour).Format(format))),
		m.row("analyze.label.best_day", m.ui.bold.Render(stats.MostProductiveDay)),
		"",
		m.ui.renderSection(m.loc.Text("analyze.section.switching")),
		m.row("analyze.label.avg_switches", m.ui.bold.Render(fmt.Sprintf("%.1f", stats.AvgSwitchesPerDay))),
		m.row("analyze.label.verdict", m.ui.muted.Render(withoutTrailingSymbols(m.loc.Text(switchVerdictKey(stats.AvgSwitchesPerDay))))),
		"",
		m.ui.renderSection(m.loc.Text("analyze.section.distribution")),
	}

	most := 0
	for _, count := range stats.FocusDistribution {
		most = max(most, count)
	}
	countBarW := max(width-analyzeLabelW-analyzeCountW, calendarBarMinW)
	for _, key := range focusDistributionKeys() {
		count := stats.FocusDistribution[key]
		lines = append(lines, m.ui.renderBarLine(barLine{
			label:      localizeFocusDistributionLabel(m.loc, key),
			labelStyle: m.ui.muted,
			labelW:     analyzeLabelW,
			length:     barLength(count, most, countBarW),
			nonzero:    count > 0,
			barStyle:   lipgloss.NewStyle().Foreground(focusDistributionColor(m.ui.theme, key)),
			barW:       countBarW,
			value:      strconv.Itoa(count),
			valueW:     analyzeCountW,
		}))
	}
	return lines
}

// row is a label of tock analyze, without its colon, and a value.
func (m *analyzeModel) row(key, value string) string {
	label := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m.loc.Text(key)), ":"))
	return m.ui.muted.Render(padText(label, analyzeLabelW, false)) + value
}

// projectLines are the projects of the window by time, longest first.
func (m *analyzeModel) projectLines(width int) []string {
	projects := reportProjects(m.data.report)
	lines := []string{m.ui.renderSection(m.loc.Text("analyze.tui.projects"), formatHM(m.data.report.TotalDuration))}
	if len(projects) == 0 {
		return lines
	}
	barW := max(width-calendarProjectW-calendarDurationW-1, calendarBarMinW)
	for _, project := range projects[:min(len(projects), analyzeTopProjects)] {
		lines = append(lines, m.durationBar(project.Name, project.Duration, projects[0].Duration, calendarProjectW, barW))
	}
	if more := len(projects) - analyzeTopProjects; more > 0 {
		lines = append(lines, m.ui.muted.Render(m.loc.Format("analyze.tui.more", more)))
	}
	return lines
}

// weekdayLines are the time tracked on each weekday of the window, Monday first.
func (m *analyzeModel) weekdayLines(width int) []string {
	lines := []string{m.ui.renderSection(m.loc.Text("analyze.tui.weekdays"))}
	longest := time.Duration(0)
	for _, total := range m.data.weekdays {
		longest = max(longest, total)
	}
	barW := max(width-calendarDayLabelW-calendarDurationW-1, calendarBarMinW)
	for i, total := range m.data.weekdays {
		weekday := time.Weekday((i + 1) % 7)
		lines = append(lines, m.durationBar(localizedWeekdayShort(m.loc, weekday), total, longest, calendarDayLabelW, barW))
	}
	return lines
}

func (m *analyzeModel) durationBar(label string, value, longest time.Duration, labelW, barW int) string {
	text := "—"
	if value > 0 {
		text = formatHM(value)
	}
	return m.ui.renderBarLine(barLine{
		label:      label,
		labelStyle: m.ui.text,
		labelW:     labelW,
		length:     barLength(value, longest, barW),
		nonzero:    value > 0,
		barStyle:   m.ui.info,
		barW:       barW,
		value:      text,
		valueW:     calendarDurationW + 1,
	})
}

// withoutTrailingSymbols drops the emoji at the end of a label: their width differs between
// terminals, which would shift the column rule beside them.
func withoutTrailingSymbols(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.Is(unicode.So, r) || unicode.Is(unicode.Mn, r)
	})
}
