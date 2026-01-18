package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const (
	defaultPrimaryColor   = "#7D56F4"
	defaultSecondaryColor = "#FF5555"
	defaultTextColor      = "252"
	defaultSubTextColor   = "240"
	defaultFaintColor     = "#555555"
)

func NewWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Display a full-screen stopwatch for the current activity",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service := getService(cmd)
			cfg := getConfig(cmd)

			ctx := context.Background()

			isRunning := true
			filter := dto.ActivityFilter{
				IsRunning: &isRunning,
			}

			activities, err := service.List(ctx, filter)
			if err != nil {
				return fmt.Errorf("list activities: %w", err)
			}

			if len(activities) == 0 {
				fmt.Println("No currently running activities.")
				return nil
			}

			activity := activities[0]
			p := tea.NewProgram(initialWatchModel(activity, service, cfg.Theme))
			if _, err = p.Run(); err != nil {
				return fmt.Errorf("run program: %w", err)
			}

			return nil
		},
	}
}

type tickMsg time.Time

type watchModel struct {
	activity models.Activity
	err      error
	now      time.Time
	service  ports.ActivityResolver
	help     help.Model
	keys     keyMap
	width    int
	height   int
	theme    config.ThemeConfig
	paused   bool
}

type keyMap struct {
	Quit  key.Binding
	Pause key.Binding
}

func initialWatchModel(activity models.Activity, service ports.ActivityResolver, theme config.ThemeConfig) watchModel {
	return watchModel{
		activity: activity,
		service:  service,
		theme:    theme,
		now:      time.Now(),
		help:     help.New(),
		keys: keyMap{
			Quit: key.NewBinding(
				key.WithKeys("q", "ctrl+c"),
				key.WithHelp("q", "quit"),
			),
			Pause: key.NewBinding(
				key.WithKeys("space", " "),
				key.WithHelp("space", "pause/resume"),
			),
		},
	}
}

func (m watchModel) Init() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Pause):
			if m.paused {
				// Resume (Start new)
				req := dto.StartActivityRequest{
					Project:     m.activity.Project,
					Description: m.activity.Description,
					StartTime:   time.Now(),
				}

				newAct, err := m.service.Start(context.Background(), req)
				if err != nil {
					m.err = err
					return m, nil
				}

				m.activity = *newAct
				m.paused = false
			} else {
				// Pause (Stop)
				req := dto.StopActivityRequest{
					EndTime: time.Now(),
				}

				stoppedAct, err := m.service.Stop(context.Background(), req)
				if err != nil {
					m.paused = true
				} else if stoppedAct != nil {
					m.activity = *stoppedAct
					m.paused = true
				}
			}
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		m.now = time.Time(msg)
		return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	case error:
		m.err = msg
		return m, tea.Quit
	}
	return m, nil
}

func (m watchModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	var duration time.Duration
	if m.paused {
		if m.activity.EndTime != nil {
			duration = m.activity.EndTime.Sub(m.activity.StartTime)
		} else {
			duration = 0
		}
	} else {
		duration = m.now.Sub(m.activity.StartTime)
	}

	duration = duration.Round(time.Second)

	primary := m.theme.Primary
	if primary == "" {
		primary = defaultPrimaryColor
	}

	if m.paused {
		if m.theme.Faint != "" {
			primary = m.theme.Faint
		} else {
			primary = defaultFaintColor
		}
	}

	text := m.theme.Text
	if text == "" {
		text = defaultTextColor
	}
	subText := m.theme.SubText
	if subText == "" {
		subText = defaultSubTextColor
	}

	var (
		projectStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(subText)).
				Align(lipgloss.Center)

		descStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(text)).
				Bold(true).
				Align(lipgloss.Center).
				MarginTop(1)

		helpStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(subText)).
				Align(lipgloss.Center).
				MarginTop(2)
	)

	h := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	s := int(duration.Seconds()) % 60
	timeStr := fmt.Sprintf("%02d:%02d:%02d", h, minutes, s)

	bigTime := renderBigText(timeStr, primary)

	status := ""
	if m.paused {
		status = "PAUSED"
	}

	secondary := m.theme.Secondary
	if secondary == "" {
		secondary = defaultSecondaryColor
	}

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(secondary)).
		Bold(true).
		Align(lipgloss.Center).
		MarginTop(1)

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		bigTime,
		statusStyle.Render(status),
		descStyle.Render(m.activity.Description),
		projectStyle.Render(m.activity.Project),
		helpStyle.Render(m.help.ShortHelpView([]key.Binding{m.keys.Quit, m.keys.Pause})),
	)

	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}
	return "\n" + content + "\n"
}

var font = map[rune][]string{
	'0': {"###", "# #", "# #", "# #", "###"},
	'1': {"  #", "  #", "  #", "  #", "  #"},
	'2': {"###", "  #", "###", "#  ", "###"},
	'3': {"###", "  #", "###", "  #", "###"},
	'4': {"# #", "# #", "###", "  #", "  #"},
	'5': {"###", "#  ", "###", "  #", "###"},
	'6': {"###", "#  ", "###", "# #", "###"},
	'7': {"###", "  #", "  #", "  #", "  #"},
	'8': {"###", "# #", "###", "# #", "###"},
	'9': {"###", "# #", "###", "  #", "###"},
	':': {"   ", " # ", "   ", " # ", "   "},
}

func renderBigText(text string, color string) string {
	var lines [5]string
	for _, char := range text {
		matrix, ok := font[char]
		if !ok {
			matrix = []string{"   ", "   ", "   ", "   ", "   "}
		}
		for i, line := range matrix {
			lines[i] += line + "  "
		}
	}

	s := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true)
	return s.Render(fmt.Sprintf("%s\n%s\n%s\n%s\n%s", lines[0], lines[1], lines[2], lines[3], lines[4]))
}
