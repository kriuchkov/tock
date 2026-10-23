package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var runListProgram = func(model listModel) error {
	program := tea.NewProgram(&model, tea.WithAltScreen())
	_, err := program.Run()
	if err != nil {
		return errors.Wrap(err, "run program")
	}
	return nil
}

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

	rt := getRuntime(cmd)
	if period == periodDaily {
		model := initialListModel(rt.ActivityService, rt.TimeFormatter, getLocalizer(cmd))
		return runListProgram(model)
	}

	model := initialListPeriodModel(rt.ActivityService, rt.Config, getLocalizer(cmd), period)
	return runListPeriodProgram(model)
}

// minTableHeight keeps a list table's header, its border and at least one row visible.
const minTableHeight = 3

// Vertical layout of the daily view, used to size the table from the terminal height.
const (
	// defaultDailyTableHeight is used until the first tea.WindowSizeMsg arrives.
	defaultDailyTableHeight = 10
	// dailyViewChromeLines counts the lines View renders around the table:
	// header, blank, blank, and the help text.
	dailyViewChromeLines = 4
	// dailyViewHorizontalPadding is the width reserved around the table.
	dailyViewHorizontalPadding = 4
)

type listModel struct {
	service      ports.ActivityResolver
	timeFormat   *timeutil.Formatter // time display format (12/24 hour)
	loc          *localization.Localizer
	currentDate  time.Time
	selectedDate time.Time
	activities   []models.Activity
	table        table.Model
	// pendingDelete is the activity awaiting a delete confirmation, if any.
	pendingDelete *models.Activity
	err           error
	width         int
	height        int
}

func initialListModel(service ports.ActivityResolver, tf *timeutil.Formatter, loc *localization.Localizer) listModel {
	now := time.Now()
	m := listModel{
		service:      service,
		timeFormat:   tf,
		loc:          loc,
		currentDate:  now,
		selectedDate: now,
	}
	m.initTable()
	m.updateActivities()
	return m
}

func (m *listModel) initTable() {
	m.table = newActivityTable([]table.Column{
		{Title: m.loc.Text("list.table.key"), Width: 13},
		{Title: m.loc.Text("list.table.time"), Width: 20},
		{Title: m.loc.Text("list.table.project"), Width: 20},
		{Title: m.loc.Text("list.table.description"), Width: 40},
		{Title: m.loc.Text("list.table.duration"), Width: 10},
		{Title: m.loc.Text("list.table.tags"), Width: 15},
		{Title: m.loc.Text("list.table.notes"), Width: 30},
	}, defaultDailyTableHeight)
}

func (m *listModel) updateActivities() {
	filter := models.ActivityFilter{}
	activities, err := m.service.List(context.Background(), filter)
	if err != nil {
		m.err = errors.Wrap(err, "list activities")
		return
	}
	m.renderTable(activities)
}

func (m *listModel) navigate(dir int) {
	activities, err := m.service.List(context.Background(), models.ActivityFilter{})
	if err != nil {
		m.err = errors.Wrap(err, "list activities")
		return
	}

	m.selectedDate = adjacentActivityDay(activities, m.selectedDate, dir)
	m.renderTable(activities)
}

func (m *listModel) renderTable(activities []models.Activity) {
	m.activities = activitiesOnDay(activities, m.selectedDate)
	rows := make([]table.Row, 0, len(m.activities))
	for i, activity := range m.activities {
		rows = append(rows, table.Row{
			activityDayKey(activity, i),
			formatActivityTimeRange(m.timeFormat, activity),
			activity.Project,
			activity.Description,
			formatActivityDuration(activity),
			formatActivityTags(activity),
			truncateNotes(activity.Notes),
		})
	}
	m.table.SetRows(rows)
	// SetRows leaves the cursor at -1 after rendering an empty day; select the first row again.
	if m.table.Cursor() < 0 {
		m.table.SetCursor(0)
	}
}

const listNotesWidth = 27

func truncateNotes(notes string) string {
	notes = strings.ReplaceAll(notes, "\n", " ")
	if len(notes) > listNotesWidth {
		return notes[:listNotesWidth] + "..."
	}
	return notes
}

func (m *listModel) Init() tea.Cmd {
	return nil
}

func (m *listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.pendingDelete != nil {
			return m.handleDeleteConfirmKey(msg)
		}
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetWidth(msg.Width - dailyViewHorizontalPadding)
		m.table.SetHeight(fitTableHeight(m.height, dailyViewChromeLines, defaultDailyTableHeight))
	}
	return m, nil
}

func (m *listModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.String() {
	case "q", keyCtrlC:
		return m, tea.Quit
	case keyLeft, "h":
		m.navigate(-1)
	case keyRight, "l":
		m.navigate(1)
	case "up", "k", keyDown, "j":
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	case "x", "delete":
		m.requestDelete()
	}
	return m, nil
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
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.activities) {
		return
	}
	activity := m.activities[cursor]
	m.pendingDelete = &activity
}

// fitTableHeight returns the number of lines a table may occupy in a terminal of termHeight lines,
// leaving chromeLines for the rest of the view. fallback is used while the height is still unknown.
func fitTableHeight(termHeight, chromeLines, fallback int) int {
	if termHeight == 0 {
		return fallback
	}
	return max(termHeight-chromeLines, minTableHeight)
}

func (m *listModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}

	// Calendar Header
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		Render(m.loc.Format("list.header", formatLocalizedLongDateShortMonth(m.loc, m.selectedDate)))

	// Table
	tableView := m.table.View()
	return lipgloss.JoinVertical(lipgloss.Left, header, "", tableView, "\n"+m.footer())
}

// footer returns the delete confirmation prompt while one is pending, else the key help.
func (m *listModel) footer() string {
	if m.pendingDelete == nil {
		return m.loc.Text("list.help")
	}
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("203")).
		Render(m.loc.Format("list.delete.confirm", m.pendingDelete.Project, m.pendingDelete.Description))
}
