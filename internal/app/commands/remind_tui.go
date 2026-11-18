package commands

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// remindView is which reminders the interactive list shows.
type remindView int

const (
	remindViewDay remindView = iota
	remindViewWeek
	remindViewOverdue
	remindViewAll
	remindViewCount
)

// runRemindTUI opens the TUI on the reminders tab.
func runRemindTUI(cmd *cobra.Command) error {
	if _, err := getReminderService(cmd); err != nil {
		return err
	}
	return runTUI(cmd, tabReminders, periodDaily)
}

type remindModel struct {
	ctx        context.Context //nolint:containedctx // the model outlives each Bubble Tea message
	svc        ports.ReminderResolver
	activities ports.ActivityResolver
	env        reminderFormEnv
	snooze     time.Duration

	view    remindView
	day     time.Time
	rows    []models.ReminderOccurrence
	tracked []models.Activity
	overdue int
	next    nextMemo
	// cursor is the selected row; offset the first line of the body on screen, page its height.
	cursor int
	offset int
	page   int

	form          *reminderForm
	pendingDelete *models.ReminderOccurrence
	status        string
	statusErr     bool
	err           error
	width         int
	height        int
	tabs          tabStrip
	ui            chromeStyles
}

func newRemindModel(
	ctx context.Context,
	svc ports.ReminderResolver,
	activities ports.ActivityResolver,
	env reminderFormEnv,
	snooze time.Duration,
) *remindModel {
	day, _ := timeutil.LocalDayBounds(reminderNow())
	m := &remindModel{
		ctx: ctx, svc: svc, activities: activities, env: env, snooze: snooze, day: day,
		tabs: singleTab(env.loc.Text("tui.tab.reminders")),
		ui:   newChromeStyles(env.theme),
	}
	return m
}

// remindQuery is what a reload reads: the rows of a view and its day, as of now.
type remindQuery struct {
	view remindView
	day  time.Time
	now  time.Time
}

// remindLoaded is what a reload found; a refresh sends it from the background.
type remindLoaded struct {
	query   remindQuery
	rows    []models.ReminderOccurrence
	tracked []models.Activity
	overdue int
	err     error
}

func (m *remindModel) query() remindQuery {
	return remindQuery{view: m.view, day: m.day, now: reminderNow()}
}

// reload reads the rows of the view again, keeping the selection on the same occurrence.
func (m *remindModel) reload() {
	m.apply(m.load(m.query()))
}

// load reads the rows of q. It only uses the services, so it can run in the background.
func (m *remindModel) load(q remindQuery) remindLoaded {
	overdue, err := m.svc.Overdue(m.ctx, q.now)
	if err != nil {
		return remindLoaded{query: q, err: errors.Wrap(err, "list overdue reminders")}
	}
	rows, err := m.loadRows(q, overdue)
	if err != nil {
		return remindLoaded{query: q, err: err}
	}
	rows = sortReminderRows(rows, q.now)
	return remindLoaded{query: q, rows: rows, tracked: m.loadTracked(rows), overdue: len(models.OverdueOnly(overdue, q.now))}
}

// apply shows what a reload found.
func (m *remindModel) apply(loaded remindLoaded) {
	if loaded.err != nil {
		m.err = loaded.err
		return
	}
	selected, hadSelection := m.selected()
	m.rows, m.tracked, m.overdue, m.next = loaded.rows, loaded.tracked, loaded.overdue, nextMemo{}
	m.cursor = min(m.cursor, max(len(m.rows)-1, 0))
	if hadSelection {
		m.keepSelection(selected.ID())
	}
}

// keepSelection puts the cursor back on the occurrence selected before a reload: marking it
// done or snoozing it can move it in the sort order.
func (m *remindModel) keepSelection(id string) {
	for i, row := range m.rows {
		if row.ID() == id {
			m.cursor = i
			return
		}
	}
}

func (m *remindModel) loadRows(q remindQuery, overdue []models.ReminderOccurrence) ([]models.ReminderOccurrence, error) {
	now := q.now
	switch q.view {
	case remindViewOverdue:
		return models.OverdueOnly(overdue, now), nil
	case remindViewAll:
		reminders, err := m.svc.List(m.ctx)
		if err != nil {
			return nil, errors.Wrap(err, "list reminders")
		}
		rows := make([]models.ReminderOccurrence, 0, len(reminders))
		for _, reminder := range reminders {
			// A finished reminder has no current occurrence; it shows its last one, usually done.
			occurrence, ok := reminder.Current(now)
			if !ok {
				occurrence, ok = reminder.Last(now)
			}
			if ok {
				rows = append(rows, occurrence)
			}
		}
		return rows, nil
	case remindViewDay, remindViewWeek, remindViewCount:
	}

	from := q.day
	to := from.AddDate(0, 0, 1)
	if q.view == remindViewWeek {
		to = from.AddDate(0, 0, defaultReminderListDays)
	}
	occurrences, err := m.svc.Occurrences(m.ctx, from, to, now)
	if err != nil {
		return nil, errors.Wrap(err, "list reminders")
	}

	today, _ := timeutil.LocalDayBounds(now)
	if !today.Before(from) && today.Before(to) {
		return models.MergeOccurrences(overdue, occurrences), nil
	}
	return occurrences, nil
}

// loadTracked reads the activities started from the listed occurrences, for "tracked 15m".
func (m *remindModel) loadTracked(rows []models.ReminderOccurrence) []models.Activity {
	var from, to time.Time
	for _, row := range rows {
		start := row.State.ActivityStart
		if start == nil {
			continue
		}
		if from.IsZero() || start.Before(from) {
			from = *start
		}
		if start.After(to) {
			to = *start
		}
	}
	if from.IsZero() || m.activities == nil {
		return nil
	}

	from, to = from.Truncate(time.Minute), to.Truncate(time.Minute).Add(time.Minute)
	activities, err := m.activities.List(m.ctx, models.ActivityFilter{FromDate: &from, ToDate: &to})
	if err != nil {
		return nil
	}
	return activities
}

// sortReminderRows puts the overdue occurrences first, then the others by when they are due.
func sortReminderRows(rows []models.ReminderOccurrence, now time.Time) []models.ReminderOccurrence {
	sorted := slices.Clone(rows)
	slices.SortStableFunc(sorted, func(a, b models.ReminderOccurrence) int {
		aOverdue, bOverdue := a.Status(now) == models.ReminderOverdue, b.Status(now) == models.ReminderOverdue
		switch {
		case aOverdue != bOverdue:
			if aOverdue {
				return -1
			}
			return 1
		case !sameDay(a.Due, b.Due):
			return a.Due.Compare(b.Due)
		}
		return 0
	})
	return sorted
}

func (m *remindModel) selected() (models.ReminderOccurrence, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return models.ReminderOccurrence{}, false
	}
	return m.rows[m.cursor], true
}

func (m *remindModel) Init() tea.Cmd { return nil }

func (m *remindModel) setTabs(tabs tabStrip) { m.tabs = tabs }

// modal reports whether the form or the delete prompt is open.
func (m *remindModel) modal() bool { return m.form != nil || m.pendingDelete != nil }

// refresh reloads in the background, so switching to the tab does not wait on the reminders.
func (m *remindModel) refresh() tea.Cmd {
	q := m.query()
	return func() tea.Msg { return m.load(q) }
}

func (m *remindModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case remindLoaded:
		// A refresh for a view or day since left is dropped.
		if msg.query.view == m.view && msg.query.day.Equal(m.day) {
			m.apply(msg)
		}
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		switch {
		case m.form != nil:
			m.updateForm(msg)
		case m.pendingDelete != nil:
			m.confirmDelete(msg)
		default:
			return m, m.handleKey(msg)
		}
	}
	return m, nil
}

func (m *remindModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	m.status = ""
	switch msg.String() {
	case "q", keyEsc:
		return tea.Quit
	case keyTab:
		m.view = (m.view + 1) % remindViewCount
		m.reload()
	case keyShiftTab:
		m.view = (m.view + remindViewCount - 1) % remindViewCount
		m.reload()
	case keyLeft, "h":
		m.shiftDay(-1)
	case keyRight, "l":
		m.shiftDay(1)
	case "a":
		day := m.day
		if m.view != remindViewDay {
			day, _ = timeutil.LocalDayBounds(reminderNow())
		}
		m.form = newReminderFormFromRequest(m.env.tf, models.AddReminderRequest{Due: day, AllDay: true})
	case "e", keyEnter:
		if occurrence, ok := m.selected(); ok {
			m.form = newReminderFormFromReminder(m.env.tf, occurrence.Reminder)
		}
	case " ", keySpace:
		m.toggleDone()
	case "s":
		m.track()
	case "z":
		m.snoozeSelected()
	case "x", keyDel:
		if occurrence, ok := m.selected(); ok {
			m.pendingDelete = &occurrence
		}
	default:
		m.cursor, _ = moveCursor(msg.String(), m.cursor, len(m.rows), m.page)
	}
	return nil
}

func (m *remindModel) shiftDay(dir int) {
	switch m.view {
	case remindViewDay:
		m.day = m.day.AddDate(0, 0, dir)
	case remindViewWeek:
		m.day = m.day.AddDate(0, 0, dir*defaultReminderListDays)
	case remindViewOverdue, remindViewAll, remindViewCount:
		return
	}
	m.reload()
}

func (m *remindModel) toggleDone() {
	occurrence, ok := m.selected()
	if !ok {
		return
	}
	done := occurrence.State.DoneAt == nil
	if _, err := m.svc.SetDone(m.ctx, occurrence.Ref(), done, reminderNow()); err != nil {
		m.setError(err)
		return
	}
	key := "remind.tui.done"
	if !done {
		key = "remind.tui.undone"
	}
	m.setStatus(m.env.loc.Format(key, occurrence.Reminder.Title))
	m.reload()
}

func (m *remindModel) track() {
	occurrence, ok := m.selected()
	if !ok {
		return
	}
	activity, err := m.svc.Track(m.ctx, occurrence.Ref(), models.TrackReminderRequest{StartTime: reminderNow()})
	if err != nil {
		m.setError(err)
		return
	}
	m.setStatus(m.env.loc.Format("remind.tui.started", activity.Project, activity.Description))
	m.reload()
}

func (m *remindModel) snoozeSelected() {
	occurrence, ok := m.selected()
	if !ok {
		return
	}
	now := reminderNow()
	until := now.Add(m.snooze)
	if _, err := m.svc.Snooze(m.ctx, occurrence.Ref(), until); err != nil {
		m.setError(err)
		return
	}
	m.setStatus(m.env.loc.Format("remind.tui.snoozed", occurrence.Reminder.Title, until.Format(m.env.tf.GetDisplayFormat())))
	m.reload()
}

// confirmDelete removes the reminder on "y", only the selected occurrence on "o"; any other key cancels.
func (m *remindModel) confirmDelete(msg tea.KeyMsg) {
	occurrence := *m.pendingDelete
	m.pendingDelete = nil

	var err error
	switch msg.String() {
	case "y", "Y":
		if err = m.svc.Remove(m.ctx, occurrence.Reminder.ID); err == nil {
			m.setStatus(m.env.loc.Format("remind.tui.removed", occurrence.Reminder.Title))
		}
	case "o", "O":
		if !occurrence.Reminder.IsRepeating() {
			return
		}
		if err = m.svc.Skip(m.ctx, occurrence.Ref()); err == nil {
			when := formatReminderDue(m.env.loc, m.env.tf, occurrence.Due, occurrence.Reminder.AllDay, reminderNow())
			m.setStatus(m.env.loc.Format("remind.tui.skipped", when, occurrence.Reminder.Title))
		}
	default:
		return
	}
	if err != nil {
		m.setError(err)
		return
	}
	m.reload()
}

func (m *remindModel) updateForm(msg tea.KeyMsg) {
	switch m.form.update(msg) {
	case formCancel:
		m.form = nil
	case formSave:
		saved, err := saveReminderForm(m.ctx, m.svc, m.form, m.env.tf, reminderNow())
		if err != nil {
			m.form.err = reminderErrorText(m.env.loc, err)
			return
		}
		m.form = nil
		m.setStatus(m.env.loc.Format("remind.tui.saved", reminderLabel(*saved)))
		m.reload()
	case formContinue:
	}
}

func (m *remindModel) setStatus(status string) {
	m.status, m.statusErr = status, false
}

func (m *remindModel) setError(err error) {
	m.status, m.statusErr = reminderErrorText(m.env.loc, err), true
}

func (m *remindModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}
	now := reminderNow()
	header := m.ui.renderHeader(m.header(now), m.width)
	if m.form != nil {
		return renderScreen(header+"\n", m.form.view(m.env, now, m.width), "", m.width, m.height)
	}

	labels := []string{
		m.env.loc.Text("remind.tui.view.day"),
		m.env.loc.Text("remind.tui.view.week"),
		m.env.loc.Format("remind.tui.view.overdue", m.overdue),
		m.env.loc.Text("remind.tui.view.all"),
	}
	top := header + "\n" + m.ui.renderViewLine(m.env.loc.Text("tui.view"), labels, int(m.view), keyTab) + "\n"
	bottom := "\n" + m.footer()
	if m.height >= calendarFocusMinH {
		bottom = m.ui.renderFocusBox(m.focusLines(now), m.width) + "\n" + m.footer()
	}
	bodyH := m.height - lipgloss.Height(top) - lipgloss.Height(bottom)
	return renderScreen(top, m.body(now, bodyH), bottom, m.width, m.height)
}

// body is the rows under their sections, scrolled so the selected one is on screen.
func (m *remindModel) body(now time.Time, height int) string {
	if len(m.rows) == 0 {
		return " " + m.ui.muted.Render(m.env.loc.Text("remind.list.empty"))
	}
	lines, selectedLine := m.bodyLines(now)
	if height <= 0 {
		return strings.Join(lines, "\n")
	}
	if m.cursor == 0 {
		// The first row keeps its section title in sight.
		selectedLine = 0
	}
	m.offset, m.page = scrollOffset(m.offset, selectedLine, height, len(lines)), height
	return strings.Join(lines[m.offset:min(len(lines), m.offset+height)], "\n")
}

// bodyLines are the sections of the rows: the overdue ones, then one per day; selectedLine is
// the line of the selected row.
func (m *remindModel) bodyLines(now time.Time) ([]string, int) {
	rows := newReminderRows(m.ui, m.env.loc, m.env.tf)
	var lines []string
	selectedLine := 0
	for start := 0; start < len(m.rows); {
		overdue := m.rows[start].Status(now) == models.ReminderOverdue
		end := start + 1
		for end < len(m.rows) && m.sameSection(m.rows[start], m.rows[end], overdue, now) {
			end++
		}
		if start > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, " "+m.sectionTitle(m.rows[start], overdue, end-start, now))

		day := m.rows[start].Due
		if overdue {
			day = now
		}
		opts := reminderRowOpts{day: day, notes: true, notify: true}
		for i := start; i < end; i++ {
			if i == m.cursor {
				selectedLine = len(lines)
			}
			lines = append(lines, rows.row(m.rows[i], trackedDuration(m.rows[i], m.tracked), now, i == m.cursor, m.width, opts))
		}
		start = end
	}
	return lines, selectedLine
}

func (m *remindModel) sameSection(first, next models.ReminderOccurrence, overdue bool, now time.Time) bool {
	if overdue {
		return next.Status(now) == models.ReminderOverdue
	}
	return sameDay(first.Due, next.Due)
}

// sectionTitle is "OVERDUE · 2", "TODAY · MON 16 NOV · 3" or "WED 18 NOV · 1".
func (m *remindModel) sectionTitle(first models.ReminderOccurrence, overdue bool, count int, now time.Time) string {
	loc := m.env.loc
	if overdue {
		return m.ui.renderSection(loc.Text("remind.tui.section.overdue"), strconv.Itoa(count))
	}
	day := strings.ToUpper(formatReminderDay(loc, first.Due, now))
	switch {
	case sameDay(first.Due, now):
		return m.ui.renderSection(loc.Text("calendar.upcoming.today"), day, strconv.Itoa(count))
	case sameDay(first.Due, now.AddDate(0, 0, 1)):
		return m.ui.renderSection(loc.Text("calendar.upcoming.tomorrow"), day, strconv.Itoa(count))
	}
	return m.ui.renderSection(day, strconv.Itoa(count))
}

// focusLines are the details of the selected reminder.
func (m *remindModel) focusLines(now time.Time) []string {
	label := m.ui.mutedBold.Render(m.env.loc.Text("calendar.focus") + "  ")
	occurrence, ok := m.selected()
	if !ok {
		return []string{label + m.ui.muted.Render(m.env.loc.Text("remind.tui.focus.hint"))}
	}
	rows := newReminderRows(m.ui, m.env.loc, m.env.tf)
	rows.next = &m.next
	return rows.focus(label, occurrence, now)
}

// header names what the view shows: the day, the week, or all reminders; the ruler counts
// the overdue ones.
func (m *remindModel) header(now time.Time) screenHeader {
	loc := m.env.loc
	h := screenHeader{tabs: m.tabs.names, active: m.tabs.active, title: loc.Text("remind.tui.title")}
	switch m.view {
	case remindViewDay:
		h.title, h.hint = formatReminderDay(loc, m.day, now), "←→"
	case remindViewWeek:
		h.title, h.hint = formatDayRange(loc, m.day, m.day.AddDate(0, 0, defaultReminderListDays-1), now), "←→"
	case remindViewOverdue, remindViewAll, remindViewCount:
	}
	if m.overdue > 0 {
		h.summary = append(h.summary, m.ui.overdueItem(loc.Format("tui.summary.overdue", m.overdue), m.overdue))
	}
	if len(m.rows) > 0 {
		done := 0
		for _, row := range m.rows {
			if row.Status(now) == models.ReminderDone {
				done++
			}
		}
		count := m.ui.muted.Render(loc.Format("remind.tui.summary.done", done, len(m.rows)))
		h.summary = append(h.summary, rulerItem{long: count, short: count})
	}
	return h
}

func (m *remindModel) footer() string {
	loc := m.env.loc
	if m.pendingDelete != nil {
		occurrence := m.pendingDelete
		key := "remind.tui.delete_confirm"
		if occurrence.Reminder.IsRepeating() {
			key = "remind.tui.delete_confirm_repeating"
		}
		return m.ui.renderPrompt(loc.Format(key, occurrence.Reminder.ID, occurrence.Reminder.Title), m.width)
	}
	return m.ui.renderFooter(m.footerKeys(), m.ui.renderStatus(m.status, m.statusErr), m.width)
}

// footerKeys are the keys of the selected reminder first, then those of the view.
func (m *remindModel) footerKeys() []footerKey {
	loc := m.env.loc
	key := func(k, action string) footerKey { return footerKey{key: k, action: loc.Text(action)} }
	var keys []footerKey
	if _, ok := m.selected(); ok {
		keys = append(keys,
			key(keySpace, "tui.key.done"), key("s", "tui.key.start"),
			footerKey{key: "z", action: loc.Format("tui.key.snooze", formatNotifyValue(m.snooze))},
			key("e", "tui.key.edit"), key("x", "tui.key.delete"),
		)
	}
	keys = append(keys, key("a", "tui.key.add"))
	switch m.view {
	case remindViewDay:
		keys = append(keys, key("←→", "tui.key.day"))
	case remindViewWeek:
		keys = append(keys, key("←→", "tui.key.week"))
	case remindViewOverdue, remindViewAll, remindViewCount:
	}
	keys = append(keys, key(keyTab, "tui.key.view"), key("q", "tui.key.quit"))
	return m.tabs.withTabs(keys, loc.Text("tui.key.tabs"))
}
