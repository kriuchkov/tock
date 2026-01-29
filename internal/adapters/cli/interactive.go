package cli

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-faster/errors"
	"github.com/samber/lo"

	ce "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
)

func SelectActivityMetadata(activities []models.Activity, project, description string, theme Theme) (string, string, error) {
	if project == "" {
		var err error
		project, err = selectProject(activities, theme)
		if err != nil {
			return "", "", err
		}
	}

	if description == "" {
		var err error
		description, err = selectTask(activities, project, theme)
		if err != nil {
			return "", "", err
		}
	}
	return project, description, nil
}

func selectProject(activities []models.Activity, theme Theme) (string, error) {
	projects := getUniqueProjects(activities)
	const newOption = "➕ Create New Project"
	options := append([]string{newOption}, projects...)

	selection, err := RunInteractiveList(options, "Select Project", theme)
	if err != nil {
		return "", errors.Wrap(err, "select project")
	}

	if selection == newOption {
		var project string
		project, err = RunInteractiveInput("New Project Name", "Project", theme)
		if err != nil {
			return "", errors.Wrap(err, "input project name")
		}
		return project, nil
	}
	return selection, nil
}

func selectTask(activities []models.Activity, project string, theme Theme) (string, error) {
	descriptions := getDescriptionsForProject(activities, project)

	const newOption = "➕ Create New Task"
	options := append([]string{newOption}, descriptions...)

	selection, err := RunInteractiveList(options, fmt.Sprintf("Select Task for %s", project), theme)
	if err != nil {
		return "", errors.Wrap(err, "select task")
	}

	if selection == newOption {
		var description string
		description, err = RunInteractiveInput("New Task Description", "Task", theme)
		if err != nil {
			return "", errors.Wrap(err, "input task description")
		}
		return description, nil
	}

	return selection, nil
}

func getUniqueProjects(activities []models.Activity) []string {
	projects := lo.Map(activities, func(a models.Activity, _ int) string {
		return a.Project
	})
	projects = lo.Uniq(projects)
	sort.Strings(projects)
	return projects
}

func getDescriptionsForProject(activities []models.Activity, project string) []string {
	descriptions := lo.FilterMap(activities, func(a models.Activity, _ int) (string, bool) {
		return a.Description, a.Project == project
	})
	descriptions = lo.Uniq(descriptions)
	sort.Strings(descriptions)
	return descriptions
}

func RunInteractiveInput(header, placeholder string, theme Theme) (string, error) {
	m := simpleInputModel{
		prompt:      header,
		placeholder: placeholder,
		theme:       theme,
	}

	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	final := finalModel.(simpleInputModel) //nolint:errcheck // safe cast
	if final.canceled {
		return "", ce.ErrCancelled
	}

	if strings.TrimSpace(final.value) == "" {
		return "", errors.New("empty input")
	}

	return strings.TrimSpace(final.value), nil
}

type simpleInputModel struct {
	prompt      string
	placeholder string
	value       string
	canceled    bool
	theme       Theme
}

func (m simpleInputModel) Init() tea.Cmd { return nil }

func (m simpleInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		//nolint:exhaustive	// bubbletea keyMsg.Type is not exhaustive
		switch keyMsg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.canceled = true
			return m, tea.Quit
		case tea.KeyEnter:
			return m, tea.Quit
		case tea.KeyBackspace, tea.KeyDelete:
			if len(m.value) > 0 {
				m.value = m.value[:len(m.value)-1]
			}
		case tea.KeyRunes:
			m.value += string(keyMsg.Runes)
		case tea.KeySpace:
			m.value += " "
		}
	}
	return m, nil
}

func (m simpleInputModel) View() string {
	t := m.theme
	s := strings.Builder{}
	s.WriteString("\n")
	s.WriteString(lipgloss.NewStyle().Bold(true).Foreground(t.Highlight).Render(m.prompt))
	s.WriteString("\n\n")

	val := m.value
	if val == "" {
		val = lipgloss.NewStyle().Foreground(t.Faint).Render(m.placeholder)
	}
	s.WriteString(val)
	// Cursor
	s.WriteString(lipgloss.NewStyle().Blink(true).Render("█"))

	s.WriteString("\n\n")
	s.WriteString(lipgloss.NewStyle().Foreground(t.Faint).Render("(esc to cancel)"))
	s.WriteString("\n")
	return s.String()
}

func RunInteractiveList(items []string, title string, theme Theme) (string, error) {
	m := &simpleListModel{
		title:    title,
		all:      items,
		filtered: items,
		theme:    theme,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return "", errors.Wrap(err, "run interactive list")
	}

	final := finalModel.(*simpleListModel) //nolint:errcheck // safe cast
	if final.canceled {
		return "", ce.ErrCancelled
	}

	if final.selected == "" {
		return "", errors.New("no selection")
	}
	return final.selected, nil
}

type simpleListModel struct {
	title    string
	all      []string
	filtered []string
	filter   string
	cursor   int
	selected string
	canceled bool
	theme    Theme
}

func (m *simpleListModel) Init() tea.Cmd { return nil }

func (m *simpleListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		//nolint:exhaustive	// bubbletea keyMsg.Type is not exhaustive
		switch keyMsg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.canceled = true
			return m, tea.Quit
		case tea.KeyEnter, tea.KeyTab:
			if len(m.filtered) > 0 {
				m.selected = m.filtered[m.cursor]
				return m, tea.Quit
			}
			return m, nil
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		case tea.KeyBackspace, tea.KeyDelete:
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.applyFilter()
			}
		case tea.KeyRunes:
			m.filter += string(keyMsg.Runes)
			m.applyFilter()
		case tea.KeySpace:
			m.filter += " "
			m.applyFilter()
		}
	}
	return m, nil
}

func (m *simpleListModel) applyFilter() {
	if m.filter == "" {
		m.filtered = m.all
	} else {
		lowFilter := strings.ToLower(m.filter)
		m.filtered = lo.Filter(m.all, func(item string, _ int) bool {
			return strings.Contains(strings.ToLower(item), lowFilter)
		})
	}
	m.cursor = 0
}

func (m *simpleListModel) View() string {
	t := m.theme
	s := strings.Builder{}
	s.WriteString(lipgloss.NewStyle().Bold(true).Foreground(t.Highlight).Render(m.title))
	s.WriteString("\n")

	// Filter input
	s.WriteString("Filter: " + m.filter)
	s.WriteString(lipgloss.NewStyle().Blink(true).Render("█"))
	s.WriteString("\n\n")

	// List
	// Show window of 10 items around cursor
	start := 0
	end := len(m.filtered)

	height := 15
	if end > height {
		start, end = m.calculateWindow(height, end)
	}

	for i := start; i < end; i++ {
		cursor := "  "
		style := lipgloss.NewStyle()
		if m.cursor == i {
			cursor = "> "
			style = style.Foreground(t.Text).Background(t.Primary).Bold(true)
		}
		s.WriteString(cursor + style.Render(m.filtered[i]) + "\n")
	}

	if len(m.filtered) == 0 {
		s.WriteString("  (no matches)\n")
	}

	return lipgloss.NewStyle().Margin(1, 2).Render(s.String())
}

func (m *simpleListModel) calculateWindow(height, end int) (int, int) {
	start := 0
	if m.cursor > height/2 {
		start = m.cursor - height/2
	}
	if start+height < end {
		end = start + height
	} else {
		start = max(end-height, 0)
	}
	return start, end
}
