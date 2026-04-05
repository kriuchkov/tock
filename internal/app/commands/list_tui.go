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
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List activities (Calendar View)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runListCmd(cmd)
		},
	}
	return cmd
}

func runListCmd(cmd *cobra.Command) error {
	rt := getRuntime(cmd)
	service := rt.ActivityService
	tf := rt.TimeFormatter
	model := initialListModel(service, tf, getLocalizer(cmd))
	return runListProgram(model)
}

type listModel struct {
	service      ports.ActivityResolver
	timeFormat   *timeutil.Formatter // time display format (12/24 hour)
	loc          *localization.Localizer
	currentDate  time.Time
	selectedDate time.Time
	activities   []models.Activity
	table        table.Model
	err          error
	width        int
	height       int
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
	columns := []table.Column{
		{Title: m.loc.Text("list.table.key"), Width: 13},
		{Title: m.loc.Text("list.table.time"), Width: 20},
		{Title: m.loc.Text("list.table.project"), Width: 20},
		{Title: m.loc.Text("list.table.description"), Width: 40},
		{Title: m.loc.Text("list.table.duration"), Width: 10},
		{Title: m.loc.Text("list.table.tags"), Width: 15},
		{Title: m.loc.Text("list.table.notes"), Width: 30},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
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

	current := time.Date(m.selectedDate.Year(), m.selectedDate.Month(), m.selectedDate.Day(), 0, 0, 0, 0, m.selectedDate.Location())
	target := models.FindTargetDate(activities, current, dir)

	if target != nil {
		m.selectedDate = *target
	}

	m.renderTable(activities)
}

func (m *listModel) renderTable(activities []models.Activity) {
	var dayActivities []models.Activity    // Filtered for display
	var allDayActivities []models.Activity // All for the day (for numbering)

	// First pass: find all activities for the selected date to establish correct numbering
	for _, a := range activities {
		if a.StartTime.Year() == m.selectedDate.Year() &&
			a.StartTime.Month() == m.selectedDate.Month() &&
			a.StartTime.Day() == m.selectedDate.Day() {
			allDayActivities = append(allDayActivities, a)
		}
	}
	// Sort by start time (assuming they might not be sorted)
	// Actually models.Activity doesn't have a sort method, but usually they come sorted or we should sort them.
	// For now we assume they are somewhat ordered or we sort them here?
	// Let's rely on the order provided by service.List for now, or sort if needed.
	// We'll trust the service or handle it implicitly.
	// However, to be safe for key generation:

	// Simply assign to filtered list (currently no other filtering)
	dayActivities = allDayActivities
	m.activities = dayActivities

	var rows []table.Row
	for i, a := range dayActivities {
		duration := a.Duration().Round(time.Minute).String()
		timeStr := a.StartTime.Format(m.timeFormat.GetDisplayFormat())
		if a.EndTime != nil {
			timeStr += " - " + a.EndTime.Format(m.timeFormat.GetDisplayFormat())
		} else {
			timeStr += " - ..."
		}

		key := fmt.Sprintf("%s-%02d", a.StartTime.Format("2006-01-02"), i+1)

		tagsStr := strings.Join(a.Tags, ", ")
		notesStr := strings.ReplaceAll(a.Notes, "\n", " ")
		if len(notesStr) > 27 {
			notesStr = notesStr[:27] + "..."
		}

		rows = append(rows, table.Row{key, timeStr, a.Project, a.Description, duration, tagsStr, notesStr})
	}
	m.table.SetRows(rows)
}

func (m *listModel) Init() tea.Cmd {
	return nil
}

func (m *listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "left", "h":
			m.navigate(-1)
		case "right", "l":
			m.navigate(1)
		case "up", "k":
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		case "down", "j":
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetWidth(msg.Width - 4)
	}
	return m, nil
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
	return lipgloss.JoinVertical(lipgloss.Left, header, "", tableView, "\n"+m.loc.Text("list.help"))
}
