package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/core/models"
)

// Breakpoints of the calendar layout. From calendarWideMin columns it has three columns (A):
// the month with its totals, the day, and Upcoming with the last days and top projects.
// Narrower, the right column goes and the totals move into the ruler (B); then Upcoming
// leaves the month column, and below calendarMonthMin only the day is left, the month
// opening over it on m.
const (
	calendarWideMin     = 140
	calendarInlineMin   = 120 // B with notes and tags in the day rows instead of the focus box
	calendarUpcomingMin = 100 // B with Upcoming under the month
	calendarMonthMin    = 70
	calendarExtrasMinH  = 30 // BY PROJECT and the last days need this many lines
	calendarFocusMinH   = 24

	calendarMonthColW = 30
	calendarSideColW  = 42
	// calendarChromeH is the header, the ruler, the blank line under them and the footer.
	calendarChromeH = 4
)

// calendarLayout is where the calendar puts what, worked out from the terminal size.
type calendarLayout struct {
	width  int
	height int
	monthW int // the month column, 0 when only the day fits
	sideW  int // the right column of layout A, 0 otherwise
	dayW   int
	bodyH  int // the height of the columns
	// upcomingLeft puts Upcoming under the month (layout B).
	upcomingLeft bool
	// inline shows notes and tags in the day rows; otherwise they are in the focus box.
	inline bool
	focus  bool
	// extras are BY PROJECT under the day and the last days and top projects of layout A.
	extras bool
}

func newCalendarLayout(width, height int) calendarLayout {
	l := calendarLayout{
		width:  width,
		height: height,
		focus:  height >= calendarFocusMinH,
		extras: height >= calendarExtrasMinH,
	}
	switch {
	case width >= calendarWideMin:
		l.monthW, l.sideW = calendarMonthColW, calendarSideColW
	case width >= calendarMonthMin:
		l.monthW = calendarMonthColW
		l.upcomingLeft = width >= calendarUpcomingMin
		if width >= calendarInlineMin {
			l.inline, l.focus = true, false
		}
	}

	l.dayW = width
	if l.monthW > 0 {
		l.dayW -= l.monthW + len([]rune(columnRule))
	}
	if l.sideW > 0 {
		l.dayW -= l.sideW + len([]rune(columnRule))
	}
	l.dayW = max(l.dayW, 1)

	bottom := 1 // the blank line above the footer
	if l.focus {
		bottom = focusBoxH
	}
	l.bodyH = max(height-calendarChromeH-bottom, 1)
	return l
}

// dayOnly is the layout of narrow terminals: the day without the month next to it.
func (l calendarLayout) dayOnly() bool {
	return l.monthW == 0
}

func (m *calendarModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}
	if !m.ready {
		return m.loc.Format("calendar.initializing")
	}
	if m.rem.form != nil {
		return renderScreen(m.renderHeader()+"\n", m.rem.form.view(m.rem.env, reminderNow(), m.width), "", m.width, m.height)
	}

	parts := []string{m.renderHeader(), "", m.renderBody()}
	if m.layout.focus {
		parts = append(parts, m.ui.renderFocusBox(m.focusLines(), m.layout.width))
	} else {
		parts = append(parts, "")
	}
	parts = append(parts, m.renderFooterLine())
	return strings.Join(parts, "\n")
}

// renderBody draws the columns: the month, the day and, in layout A, the right column.
func (m *calendarModel) renderBody() string {
	l := m.layout
	if l.dayOnly() {
		if m.monthOpen {
			return fitBlock(indentLines(m.monthBlock()), l.width, l.bodyH)
		}
		return fitBlock(m.viewport.View(), l.dayW, l.bodyH)
	}

	columns := []string{
		fitBlock(m.renderMonthColumn(), l.monthW, l.bodyH),
		m.ui.columnRules(l.bodyH),
		fitBlock(m.viewport.View(), l.dayW, l.bodyH),
	}
	if l.sideW > 0 {
		columns = append(columns, m.ui.columnRules(l.bodyH), fitBlock(m.renderSideColumn(), l.sideW, l.bodyH))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, columns...)
}

// renderMonthColumn is the month with its legend, then the month totals (A) or Upcoming (B).
func (m *calendarModel) renderMonthColumn() string {
	l := m.layout
	lines := m.monthBlock()
	switch {
	case l.sideW > 0:
		lines = append(lines, "")
		lines = append(lines, m.monthStatsLines(l.monthW-1)...)
	case l.upcomingLeft:
		if upcoming := m.upcomingLines(l.monthW-1, l.bodyH-len(lines)-1); len(upcoming) > 0 {
			lines = append(lines, "")
			lines = append(lines, upcoming...)
		}
	}
	return indentLines(lines)
}

// renderSideColumn is the right column of layout A: Upcoming, the last days and the top projects.
func (m *calendarModel) renderSideColumn() string {
	l := m.layout
	budget := l.bodyH
	if l.extras {
		budget = max(l.bodyH-calendarLastDays-calendarTopProjects-4, calendarUpcomingMinLines)
	}
	var lines []string
	if upcoming := m.upcomingLines(l.sideW, budget); len(upcoming) > 0 {
		lines = append(lines, upcoming...)
		lines = append(lines, "")
	}
	if l.extras {
		lines = append(lines, m.lastDaysLines(l.sideW)...)
		lines = append(lines, "")
		lines = append(lines, m.topProjectLines(l.sideW)...)
	}
	return strings.Join(lines, "\n")
}

// monthBlock is the month grid with its legend.
func (m *calendarModel) monthBlock() []string {
	lines := m.monthLines()
	if legend := m.legendLines(); len(legend) > 0 {
		lines = append(lines, "")
		lines = append(lines, legend...)
	}
	return lines
}

// indentLines joins lines one column in from the screen edge.
func indentLines(lines []string) string {
	return " " + strings.Join(lines, "\n ")
}

func (m *calendarModel) renderHeader() string {
	h := screenHeader{
		tabs:    m.tabs.names,
		active:  m.tabs.active,
		title:   formatLocalizedMonthYear(m.loc, m.viewDate),
		hint:    "n/p",
		summary: m.rulerSummary(),
	}
	if m.layout.dayOnly() {
		day := m.currentDate
		h.title = fmt.Sprintf("%s %d %s",
			localizedWeekdayAbbr(m.loc, day.Weekday()), day.Day(), localizedMonthShortName(m.loc, day.Month()))
		h.hint = "←→"
	}
	return m.ui.renderHeader(h, m.layout.width)
}

// rulerSummary is what the ruler shows from anywhere: overdue reminders, the running
// activity and the totals.
func (m *calendarModel) rulerSummary() []rulerItem {
	var items []rulerItem
	if overdue := len(models.OverdueOnly(m.rem.data.overdue, reminderNow())); m.remindersEnabled() && overdue > 0 {
		items = append(items, m.ui.overdueItem(m.loc.Format("tui.summary.overdue", overdue), overdue))
	}
	if activity, ok := m.runningActivity(); ok {
		spent := formatHM(activity.Duration())
		items = append(items, rulerItem{
			long:  m.ui.ok.Render(glyphRunning) + m.ui.muted.Render(" "+activity.Project+" "+spent),
			short: m.ui.ok.Render(glyphRunning) + m.ui.muted.Render(" "+spent),
		})
	}
	if totals := m.totalsSummary(); totals.long != "" {
		items = append(items, totals)
	}
	return items
}

// totalsSummary is today's and the month's time; without the month totals column it adds
// the average, the streak and the week target.
func (m *calendarModel) totalsSummary() rulerItem {
	stats := m.monthStats()
	var parts []string
	if today, ok := m.reportForDate(time.Now()); ok && today != nil {
		parts = append(parts, m.loc.Format("calendar.summary.today", formatHM(today.TotalDuration)))
	}
	short := strings.Join(parts, "")
	parts = append(parts, m.loc.Format("calendar.summary.month", formatHM(stats.TotalDuration)))
	if short == "" {
		short = parts[0]
	}
	if m.layout.sideW == 0 {
		parts = append(parts,
			m.loc.Format("calendar.summary.avg", formatHM(stats.AvgDuration)),
			m.loc.Format("calendar.summary.streak", stats.LongestStreak),
		)
		if target := m.config.WeeklyTarget; target > 0 {
			week := insights.BuildWeeklyActivityData(m.dailyReports, m.currentDate).CurrentWeekTotal
			parts = append(parts, m.loc.Format("calendar.summary.week", formatHM(week), formatHM(target)))
		}
	}
	return rulerItem{long: m.ui.muted.Render(strings.Join(parts, " · ")), short: m.ui.muted.Render(short)}
}

// runningActivity is the latest activity still running, if the loaded days have one.
func (m *calendarModel) runningActivity() (models.Activity, bool) {
	if len(m.running) == 0 {
		return models.Activity{}, false
	}
	latest := m.running[0]
	for _, activity := range m.running[1:] {
		if activity.StartTime.After(latest.StartTime) {
			latest = activity
		}
	}
	return latest, true
}

func (m *calendarModel) renderFooterLine() string {
	width := m.layout.width
	if m.rem.pendingDelete != nil {
		return m.ui.renderPrompt(m.reminderDeletePrompt(), width)
	}
	return m.ui.renderFooter(m.footerKeys(), m.ui.renderStatus(m.rem.status, m.rem.statusErr), width)
}

// footerKeys are the keys that work in the current state, most useful first.
func (m *calendarModel) footerKeys() []footerKey {
	key := func(k, action string) footerKey { return footerKey{key: k, action: m.loc.Text(action)} }
	switch {
	case m.monthOpen:
		return []footerKey{
			key("←→", "tui.key.day"), key("↑↓", "tui.key.week"),
			key("n/p", "tui.key.month"), key("m/esc", "tui.key.close"),
		}
	case m.sel.section == sectionReminders:
		return []footerKey{
			key("j/k", "tui.key.row"), key(keySpace, "tui.key.done"), key("s", "tui.key.start"),
			{key: "z", action: m.loc.Format("tui.key.snooze", formatNotifyValue(m.rem.snooze))},
			key("e", "tui.key.edit"), key("x", "tui.key.delete"),
			key(keyTab, "tui.key.section"), key("esc", "tui.key.back"),
		}
	case m.sel.section == sectionTracked:
		return []footerKey{
			key("j/k", "tui.key.row"), key(keyTab, "tui.key.section"),
			key("←→", "tui.key.day"), key("esc", "tui.key.back"),
		}
	}

	keys := []footerKey{key("←→", "tui.key.day"), key("↑↓", "tui.key.week")}
	if m.layout.dayOnly() {
		keys = append(keys, key("m", "tui.key.month"))
	} else {
		keys = append(keys, key("n/p", "tui.key.month"))
	}
	keys = append(keys, key("j/k", "tui.key.select"), key(keyTab, "tui.key.section"))
	if m.remindersEnabled() {
		keys = append(keys, key("a", "tui.key.add_reminder"))
	}
	keys = append(keys, key("q", "tui.key.quit"))
	return m.tabs.withTabs(keys, m.loc.Text("tui.key.tabs"))
}
