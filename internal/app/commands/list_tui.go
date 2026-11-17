package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func NewListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:       "list [period]",
		Aliases:   []string{"ls"},
		Short:     "List activities: daily calendar view, or a weekly/monthly/yearly summary",
		ValidArgs: []string{periodArgDaily, periodArgWeekly, periodArgMonthly, periodArgYearly},
		Args:      cobra.MaximumNArgs(1),
		RunE:      runListCmd,
	}
	return cmd
}

func runListCmd(cmd *cobra.Command, args []string) error {
	arg := ""
	if len(args) > 0 {
		arg = args[0]
	}

	period, ok := parseListPeriod(arg)
	if !ok {
		return errors.New(text(cmd, "list.error.invalid_period", arg))
	}

	return runTUI(cmd, tabList, period)
}

type listModel struct {
	service      ports.ActivityResolver
	timeFormat   *timeutil.Formatter // time display format (12/24 hour)
	loc          *localization.Localizer
	currentDate  time.Time
	selectedDate time.Time
	// all is every activity, read once and again after a change, for moving between days.
	all  []models.Activity
	list activityList
	// pendingDelete is the activity awaiting a delete confirmation, if any.
	pendingDelete *models.Activity
	// edit is the form of the activity being edited, if any.
	edit      *editForm
	formErr   string
	status    string
	statusErr bool
	err       error
	width     int
	height    int
	tabs      tabStrip
	ui        chromeStyles
}

func initialListModel(
	service ports.ActivityResolver,
	tf *timeutil.Formatter,
	loc *localization.Localizer,
	theme Theme,
) listModel {
	return newListModel(service, tf, loc, theme, time.Now())
}

// newListModel lists the activities of the day of date.
func newListModel(
	service ports.ActivityResolver,
	tf *timeutil.Formatter,
	loc *localization.Localizer,
	theme Theme,
	date time.Time,
) listModel {
	ui := newChromeStyles(theme)
	m := listModel{
		service:      service,
		timeFormat:   tf,
		loc:          loc,
		currentDate:  time.Now(),
		selectedDate: date,
		list:         newActivityList(ui, loc, tf),
		tabs:         singleTab(loc.Text("tui.tab.list")),
		ui:           ui,
	}
	m.updateActivities()
	return m
}

// listLoaded is every activity, read in the background by a refresh.
type listLoaded struct {
	activities []models.Activity
	err        error
}

// updateActivities reads every activity again and shows the selected day.
func (m *listModel) updateActivities() {
	m.setAll(loadAllActivities(m.service))
}

func loadAllActivities(service ports.ActivityResolver) listLoaded {
	activities, err := service.List(context.Background(), models.ActivityFilter{})
	return listLoaded{activities: activities, err: err}
}

func (m *listModel) setAll(loaded listLoaded) {
	if loaded.err != nil {
		m.err = errors.Wrap(loaded.err, "list activities")
		return
	}
	m.all = loaded.activities
	m.list.set(activitiesOnDay(m.all, m.selectedDate))
}

// showDay shows the activities of day.
func (m *listModel) showDay(day time.Time) {
	m.selectedDate = day
	m.list.set(activitiesOnDay(m.all, day))
}

func (m *listModel) navigate(dir int) {
	m.showDay(adjacentActivityDay(m.all, m.selectedDate, dir))
}

func (m *listModel) Init() tea.Cmd {
	return nil
}

func (m *listModel) setTabs(tabs tabStrip) { m.tabs = tabs }

// modal reports whether the delete prompt or the edit form is open.
func (m *listModel) modal() bool { return m.pendingDelete != nil || m.edit != nil }

// refresh reads the activities in the background, so switching to the tab does not wait on them.
func (m *listModel) refresh() tea.Cmd {
	service := m.service
	return func() tea.Msg { return loadAllActivities(service) }
}

func (m *listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case m.pendingDelete != nil:
			return m.handleDeleteConfirmKey(msg)
		case m.edit != nil:
			m.updateEdit(msg)
			return m, nil
		}
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case listLoaded:
		m.setAll(msg)
	}
	return m, nil
}

func (m *listModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch msg.String() {
	case "q", keyCtrlC:
		return m, tea.Quit
	case keyLeft, "h":
		m.navigate(-1)
	case keyRight, "l":
		m.navigate(1)
	case "x", "delete":
		m.requestDelete()
	case "e", "enter":
		if activity, ok := m.list.selected(); ok {
			form := newEditForm(activity, m.timeFormat)
			m.edit, m.formErr = &form, ""
		}
	case "c":
		m.continueSelected()
	default:
		m.list.move(msg.String())
	}
	return m, nil
}

// updateEdit passes a key to the edit form and saves or closes it.
func (m *listModel) updateEdit(msg tea.KeyMsg) {
	switch m.edit.update(msg) {
	case formCancel:
		m.edit = nil
	case formSave:
		updated, errText := saveEditForm(context.Background(), m.service, m.timeFormat, m.loc, *m.edit)
		if updated == nil {
			m.formErr = errText
			return
		}
		m.edit = nil
		m.setStatus(m.loc.Format("edit.saved", updated.Project, updated.Description), false)
		m.selectedDate = updated.StartTime
		m.updateActivities()
	case formContinue:
	}
}

// continueSelected starts the selected activity again, as `tock continue` does.
func (m *listModel) continueSelected() {
	activity, ok := m.list.selected()
	if !ok {
		return
	}
	started, err := m.service.Start(context.Background(), models.StartActivityRequest{
		Description: activity.Description,
		Project:     activity.Project,
		StartTime:   time.Now(),
	})
	if err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.setStatus(m.loc.Format("list.started", started.Project, started.Description), false)
	m.selectedDate = started.StartTime
	m.updateActivities()
	m.list.cursor = len(m.list.activities) - 1
}

func (m *listModel) setStatus(status string, isErr bool) {
	m.status, m.statusErr = status, isErr
}

// handleDeleteConfirmKey deletes the pending activity on "y"; any other key cancels.
func (m *listModel) handleDeleteConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	activity := *m.pendingDelete
	m.pendingDelete = nil

	switch msg.String() {
	case keyCtrlC:
		return m, tea.Quit
	case "y", "Y":
		if err := m.service.Remove(context.Background(), activity); err != nil {
			m.err = errors.Wrap(err, "remove activity")
			return m, nil
		}
		m.updateActivities()
	}
	return m, nil
}

func (m *listModel) requestDelete() {
	if activity, ok := m.list.selected(); ok {
		m.pendingDelete = &activity
	}
}

func (m *listModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}
	if m.edit != nil {
		header := m.header()
		header.title = activityDayKey(m.edit.original, m.list.cursor)
		header.hint, header.summary = "", m.edit.summary(m.ui, m.loc)
		top := m.ui.renderHeader(header, m.width) + "\n"
		bottom := "\n" + m.ui.renderFooter(editFormKeys(m.loc, "tui.key.back"), m.ui.renderStatus(m.formErr, true), m.width)
		return renderScreen(top, strings.Join(m.edit.lines(m.ui, m.loc, m.width), "\n"), bottom, m.width, m.height)
	}
	top := m.ui.renderHeader(m.header(), m.width) + "\n" + listViewLine(m.ui, m.loc, periodDaily) + "\n"
	bottom := "\n" + m.footer()
	bodyH := m.height - lipgloss.Height(top) - lipgloss.Height(bottom)
	return renderScreen(top, m.list.view(m.width, bodyH, m.loc.Text("list.empty")), bottom, m.width, m.height)
}

// header names the day; the ruler counts its activities and their time.
func (m *listModel) header() screenHeader {
	summary := m.ui.muted.Render(fmt.Sprintf("%d · %s", len(m.list.activities), formatHM(m.list.total())))
	return screenHeader{
		tabs:    m.tabs.names,
		active:  m.tabs.active,
		title:   formatLocalizedLongDateShortMonth(m.loc, m.selectedDate),
		hint:    "←→",
		summary: []rulerItem{{long: summary, short: summary}},
	}
}

// footer returns the delete confirmation prompt while one is pending, else the keys.
func (m *listModel) footer() string {
	if m.pendingDelete != nil {
		return m.ui.renderPrompt(m.loc.Format("list.delete.confirm", m.pendingDelete.Project, m.pendingDelete.Description), m.width)
	}
	key := func(k, action string) footerKey { return footerKey{key: k, action: m.loc.Text(action)} }
	keys := []footerKey{key("←→", "tui.key.day"), key("↑↓", "tui.key.entry")}
	if len(m.list.activities) > 0 {
		keys = append(keys, key("e", "tui.key.edit"), key("c", "tui.key.continue"), key("x", "tui.key.delete"))
	}
	keys = append(keys, key(keyTab, "tui.key.view"), key("q", "tui.key.quit"))
	status := m.ui.renderStatus(m.status, m.statusErr)
	return m.ui.renderFooter(m.tabs.withTabs(keys, m.loc.Text("tui.key.tabs")), status, m.width)
}

// listViewLine names the views of the list tab, the shown one marked.
func listViewLine(ui chromeStyles, loc *localization.Localizer, period listPeriod) string {
	labels := []string{
		loc.Text("list.view.day"), loc.Text("list.view.week"), loc.Text("list.view.month"), loc.Text("list.view.year"),
	}
	return ui.renderViewLine(loc.Text("tui.view"), labels, int(period), keyTab)
}
