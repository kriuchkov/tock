package cli

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func NewListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List activities (Calendar View)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service := getService(cmd)
			m := initialModel(service)
			p := tea.NewProgram(&m)
			if _, err := p.Run(); err != nil {
				return errors.Wrap(err, "run program")
			}
			return nil
		},
	}
	return cmd
}

type model struct {
	service      ports.ActivityResolver
	currentDate  time.Time
	selectedDate time.Time
	activities   []models.Activity
	table        table.Model
	err          error
	width        int
	height       int
}

func initialModel(service ports.ActivityResolver) model {
	now := time.Now()
	m := model{
		service:      service,
		currentDate:  now,
		selectedDate: now,
	}
	m.initTable()
	m.updateActivities()
	return m
}

func (m *model) initTable() {
	columns := []table.Column{
		{Title: "No.", Width: 4},
		{Title: "Time", Width: 15},
		{Title: "Project", Width: 20},
		{Title: "Description", Width: 40},
		{Title: "Duration", Width: 10},
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

func (m *model) updateActivities() {
	filter := dto.ActivityFilter{}
	activities, err := m.service.List(context.Background(), filter)
	if err != nil {
		m.err = errors.Wrap(err, "list activities")
		return
	}
	m.renderTable(activities)
}

func (m *model) navigate(dir int) {
	activities, err := m.service.List(context.Background(), dto.ActivityFilter{})
	if err != nil {
		m.err = errors.Wrap(err, "list activities")
		return
	}

	current := time.Date(m.selectedDate.Year(), m.selectedDate.Month(), m.selectedDate.Day(), 0, 0, 0, 0, m.selectedDate.Location())
	target := m.findTargetDate(activities, current, dir)

	if target != nil {
		m.selectedDate = *target
	}

	m.renderTable(activities)
}

func (m *model) findTargetDate(activities []models.Activity, current time.Time, dir int) *time.Time {
	var target *time.Time

	for _, a := range activities {
		date := time.Date(a.StartTime.Year(), a.StartTime.Month(), a.StartTime.Day(), 0, 0, 0, 0, a.StartTime.Location())
		if dir < 0 { //nolint:nestif // simple logic
			if !date.Before(current) {
				continue
			}
			if target == nil || date.After(*target) {
				d := date
				target = &d
			}
		} else {
			if !date.After(current) {
				continue
			}
			if target == nil || date.Before(*target) {
				d := date
				target = &d
			}
		}
	}
	return target
}

func (m *model) renderTable(activities []models.Activity) {
	var dayActivities []models.Activity
	for _, a := range activities {
		if a.StartTime.Year() == m.selectedDate.Year() &&
			a.StartTime.Month() == m.selectedDate.Month() &&
			a.StartTime.Day() == m.selectedDate.Day() {
			dayActivities = append(dayActivities, a)
		}
	}
	m.activities = dayActivities

	var rows []table.Row
	for _, a := range dayActivities {
		duration := a.Duration().Round(time.Minute).String()
		timeStr := a.StartTime.Format("15:04")
		if a.EndTime != nil {
			timeStr += " - " + a.EndTime.Format("15:04")
		} else {
			timeStr += " - ..."
		}

		rows = append(rows, table.Row{strconv.Itoa(len(rows)), timeStr, a.Project, a.Description, duration})
	}
	m.table.SetRows(rows)
}

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m *model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}

	// Calendar Header
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		Render(fmt.Sprintf("<< %s >>", m.selectedDate.Format("Monday, 02 Jan 2006")))

	// Table
	tableView := m.table.View()
	return lipgloss.JoinVertical(lipgloss.Left, header, "", tableView, "\nPress 'q' to quit, left/right to change date")
}
