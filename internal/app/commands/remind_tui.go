package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
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

const (
	// remindViewBlankLines are the blank lines around the table plus one spare line.
	remindViewBlankLines   = 3
	remindDetailsNextCount = 4
	remindPink             = lipgloss.Color("212")
	remindStatusOK         = lipgloss.Color("120")
)

var runRemindProgram = func(model *remindModel) error {
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return errors.Wrap(err, "run program")
	}
	return nil
}

func runRemindTUI(cmd *cobra.Command) error {
	svc, err := getReminderService(cmd)
	if err != nil {
		return err
	}
	rt := getRuntime(cmd)
	model := newRemindModel(cmd.Context(), svc, rt.ActivityService,
		newReminderFormEnv(rt.Localizer, rt.TimeFormatter, GetTheme(rt.Config.Theme)), defaultSnooze(cmd))
	model.reload()
	return runRemindProgram(model)
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
	table   table.Model

	form          *reminderForm
	pendingDelete *models.ReminderOccurrence
	status        string
	statusErr     bool
	err           error
	width         int
	height        int
}

func newRemindModel(
	ctx context.Context,
	svc ports.ReminderResolver,
	activities ports.ActivityResolver,
	env reminderFormEnv,
	snooze time.Duration,
) *remindModel {
	day, _ := timeutil.LocalDayBounds(reminderNow())
	m := &remindModel{ctx: ctx, svc: svc, activities: activities, env: env, snooze: snooze, day: day}
	loc := env.loc
	m.table = newActivityTable([]table.Column{
		{Title: loc.Text("remind.table.id"), Width: 4},
		{Title: loc.Text("remind.table.due"), Width: 18},
		{Title: loc.Text("remind.table.title"), Width: 24},
		{Title: loc.Text("remind.table.repeat"), Width: 22},
		{Title: loc.Text("remind.table.project"), Width: 12},
		{Title: loc.Text("remind.table.tags"), Width: 14},
		{Title: loc.Text("remind.table.status"), Width: 20},
	}, defaultDailyTableHeight)
	return m
}

func (m *remindModel) reload() {
	now := reminderNow()
	selected, hadSelection := m.selected()

	overdue, err := m.svc.Overdue(m.ctx, now)
	if err != nil {
		m.err = errors.Wrap(err, "list overdue reminders")
		return
	}
	m.overdue = len(overdueOnly(overdue, now))

	m.rows, err = m.loadRows(overdue, now)
	if err != nil {
		m.err = err
		return
	}
	m.tracked = m.loadTracked()
	m.renderTable(now)
	if hadSelection {
		m.keepSelection(occurrenceID(selected))
	}
}

// keepSelection puts the cursor back on the occurrence selected before a reload: marking it
// done or snoozing it can move it in the sort order.
func (m *remindModel) keepSelection(id string) {
	for i, row := range m.rows {
		if occurrenceID(row) == id {
			m.table.SetCursor(i)
			return
		}
	}
}

func (m *remindModel) loadRows(overdue []models.ReminderOccurrence, now time.Time) ([]models.ReminderOccurrence, error) {
	switch m.view {
	case remindViewOverdue:
		return overdueOnly(overdue, now), nil
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

	from := m.day
	to := from.AddDate(0, 0, 1)
	if m.view == remindViewWeek {
		to = from.AddDate(0, 0, defaultReminderListDays)
	}
	occurrences, err := m.svc.Occurrences(m.ctx, from, to, now)
	if err != nil {
		return nil, errors.Wrap(err, "list reminders")
	}

	today, _ := timeutil.LocalDayBounds(now)
	if !today.Before(from) && today.Before(to) {
		return mergeOccurrences(overdue, occurrences), nil
	}
	return occurrences, nil
}

// loadTracked reads the activities started from the listed occurrences, for "tracked 15m".
func (m *remindModel) loadTracked() []models.Activity {
	var from, to time.Time
	for _, row := range m.rows {
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

func (m *remindModel) renderTable(now time.Time) {
	loc, tf := m.env.loc, m.env.tf
	rows := make([]table.Row, 0, len(m.rows))
	for _, occurrence := range m.rows {
		reminder := occurrence.Reminder
		due := formatReminderDue(loc, tf, occurrence.Due, reminder.AllDay, now)
		if m.view == remindViewDay && sameDay(occurrence.Due, m.day) {
			due = formatReminderClock(loc, tf, occurrence.Due, reminder.AllDay)
		}
		rows = append(rows, table.Row{
			strconv.Itoa(reminder.ID),
			due,
			reminder.Title,
			orDash(formatRepeat(loc, reminder)),
			reminder.Project,
			strings.Join(reminder.Tags, ", "),
			formatReminderStatusLine(loc, tf, occurrence, now, trackedDuration(occurrence, m.tracked)),
		})
	}
	m.table.SetRows(rows)
	if cursor := m.table.Cursor(); cursor < 0 || cursor >= len(rows) {
		m.table.SetCursor(max(len(rows)-1, 0))
	}
}

func (m *remindModel) selected() (models.ReminderOccurrence, bool) {
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.rows) {
		return models.ReminderOccurrence{}, false
	}
	return m.rows[cursor], true
}

func (m *remindModel) Init() tea.Cmd { return nil }

func (m *remindModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.table.SetWidth(msg.Width - dailyViewHorizontalPadding)
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
	case "tab":
		m.view = (m.view + 1) % remindViewCount
		m.reload()
	case "shift+tab":
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
	case " ", "space":
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
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return cmd
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
	if m.form != nil {
		return m.form.view(m.env, now, m.width)
	}

	header, footer := m.renderHeader(), m.footer()
	var details string
	if occurrence, ok := m.selected(); ok {
		details = m.renderDetails(occurrence, now)
	}
	m.fitTable(header, details, footer)

	body := m.table.View()
	if len(m.rows) == 0 {
		body = lipgloss.NewStyle().Foreground(m.env.theme.SubText).Render(m.env.loc.Text("remind.list.empty"))
	}

	sections := []string{header, "", body, ""}
	if details != "" {
		sections = append(sections, details, "")
	}
	sections = append(sections, footer)
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// fitTable gives the table the lines the rest of the view leaves free. The details box and the
// footer grow with notes and a status line, so the chrome is measured rather than assumed;
// one line is kept spare.
func (m *remindModel) fitTable(header, details, footer string) {
	if m.height <= 0 {
		return
	}
	chrome := lipgloss.Height(header) + lipgloss.Height(footer) + remindViewBlankLines
	if details != "" {
		chrome += lipgloss.Height(details) + 1
	}
	m.table.SetHeight(fitTableHeight(m.height, chrome, defaultDailyTableHeight))
}

func (m *remindModel) renderHeader() string {
	loc := m.env.loc
	pink := lipgloss.NewStyle().Bold(true).Foreground(remindPink)
	faint := lipgloss.NewStyle().Foreground(m.env.theme.SubText)

	var title string
	switch m.view {
	case remindViewDay:
		title = loc.Format("list.header", formatLocalizedLongDateShortMonth(loc, m.day))
	case remindViewWeek:
		title = loc.Format("list.header", loc.Format("list.period.week_of", formatReminderDate(loc, m.day)))
	case remindViewOverdue, remindViewAll, remindViewCount:
		title = loc.Text("remind.tui.title")
	}

	labels := [remindViewCount]string{
		loc.Text("remind.tui.view.day"),
		loc.Text("remind.tui.view.week"),
		loc.Format("remind.tui.view.overdue", m.overdue),
		loc.Text("remind.tui.view.all"),
	}
	tabs := make([]string, 0, len(labels))
	for i, label := range labels {
		if remindView(i) == m.view {
			tabs = append(tabs, pink.Underline(true).Render(label))
		} else {
			tabs = append(tabs, faint.Render(label))
		}
	}
	return pink.Render(title) + "   " + strings.Join(tabs, faint.Render("  ·  "))
}

func (m *remindModel) renderDetails(occurrence models.ReminderOccurrence, now time.Time) string {
	loc, tf := m.env.loc, m.env.tf
	reminder := occurrence.Reminder
	label := lipgloss.NewStyle().Foreground(m.env.theme.SubText).Width(reminderFormLabelWidth)
	sub := lipgloss.NewStyle().Foreground(m.env.theme.SubText)

	lines := []string{lipgloss.NewStyle().Bold(true).Render(reminder.Title) + "  " + sub.Render(fmt.Sprintf("#%d", reminder.ID))}

	meta := m.env.styles.Project.Render(orDash(reminder.Project))
	if len(reminder.Tags) > 0 {
		meta += "   " + sub.Render(loc.Text("remind.form.tags")) + "  " +
			lipgloss.NewStyle().Foreground(m.env.theme.Tag).Render(strings.Join(reminder.Tags, ", "))
	}
	lines = append(lines, label.Render(loc.Text("remind.form.project"))+meta)

	repeat := orDash(formatRepeat(loc, reminder) + formatRepeatBounds(loc, reminder.Repeat))
	lines = append(lines, label.Render(loc.Text("remind.form.repeat"))+repeat)

	if reminder.Notify > 0 {
		lines = append(
			lines,
			label.Render(loc.Text("remind.form.notify"))+loc.Format("remind.notify_before", formatNotifyValue(reminder.Notify)),
		)
	}

	upcoming := reminder.Occurrences(occurrence.Due, occurrence.Due.AddDate(0, 0, reminderFormPreviewDays), now)
	next := make([]string, 0, remindDetailsNextCount)
	for i, upcomingOccurrence := range upcoming {
		if i == remindDetailsNextCount {
			break
		}
		when := formatReminderDue(loc, tf, upcomingOccurrence.Due, reminder.AllDay, now)
		if i == 0 {
			next = append(next, m.env.styles.Duration.Render(when))
		} else {
			next = append(next, sub.Render(when))
		}
	}
	if len(next) > 0 {
		lines = append(lines, label.Render(loc.Text("remind.form.next"))+strings.Join(next, sub.Render("  ·  ")))
	}

	if reminder.Notes != "" {
		lines = append(lines, label.Render(loc.Text("remind.form.notes"))+strings.ReplaceAll(reminder.Notes, "\n", " "))
	}
	return m.env.styles.Wrapper.Render(strings.Join(lines, "\n"))
}

func (m *remindModel) footer() string {
	loc := m.env.loc
	if m.pendingDelete != nil {
		occurrence := m.pendingDelete
		key := "remind.tui.delete_confirm"
		if occurrence.Reminder.IsRepeating() {
			key = "remind.tui.delete_confirm_repeating"
		}
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203")).
			Render(loc.Format(key, occurrence.Reminder.ID, occurrence.Reminder.Title))
	}

	help := loc.Text("remind.tui.help")
	if m.status == "" {
		return help
	}
	color := remindStatusOK
	if m.statusErr {
		color = m.env.theme.Secondary
	}
	return help + "\n" + lipgloss.NewStyle().Foreground(color).Render(m.status)
}
