package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/app/watching"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

type watchOptions struct {
	StopOnExit bool
}

var currentActivityTime = time.Now

var runWatchProgram = func(model watchModel) error {
	program := tea.NewProgram(&model, tea.WithAltScreen())
	_, err := program.Run()
	if err != nil {
		return errors.Wrap(err, "run program")
	}
	return nil
}

func NewWatchCmd() *cobra.Command {
	var opts watchOptions

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Display a full-screen stopwatch for the current activity",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWatchCmd(cmd, &opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.StopOnExit, "stop", "s", false, defaultText("watch.flag.stop"))
	return cmd
}

func runWatchCmd(cmd *cobra.Command, opts *watchOptions) error {
	rt := getRuntime(cmd)
	service := rt.ActivityService
	cfg := rt.Config
	out := cmd.OutOrStdout()

	activity, err := watching.FindCurrentActivity(cmd.Context(), service)
	if err != nil {
		if errors.Is(err, coreErrors.ErrNoActiveActivity) {
			fmt.Fprintln(out, text(cmd, "current.empty"))
			return nil
		}
		return errors.Wrap(err, "load current activity")
	}

	if err = runWatchProgram(initialWatchModel(*activity, service, GetTheme(cfg.Theme), getLocalizer(cmd))); err != nil {
		return err
	}

	if !opts.StopOnExit {
		return nil
	}

	stopped, err := watching.StopOnExit(cmd.Context(), service, currentActivityTime())
	if err != nil {
		return err
	}
	if stopped == nil {
		return nil
	}
	_, err = fmt.Fprintf(out, text(cmd, "message.activity_stopped_short"), stopped.Project, stopped.Description)
	return err
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
	theme    Theme
	loc      *localization.Localizer
	paused   bool
}

type keyMap struct {
	Quit  key.Binding
	Pause key.Binding
}

func initialWatchModel(activity models.Activity, service ports.ActivityResolver, theme Theme, loc *localization.Localizer) watchModel {
	return watchModel{
		activity: activity,
		service:  service,
		theme:    theme,
		loc:      loc,
		now:      time.Now(),
		help:     help.New(),
		keys: keyMap{
			Quit: key.NewBinding(
				key.WithKeys("q", "ctrl+c"),
				key.WithHelp("q", loc.Format("watch.key.quit")),
			),
			Pause: key.NewBinding(
				key.WithKeys("space", " "),
				key.WithHelp("space", loc.Format("watch.key.pause")),
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
			updated, paused, err := watching.TogglePause(context.Background(), m.service, m.activity, m.paused, currentActivityTime())
			if err != nil {
				m.err = err
				return m, nil
			}
			m.activity = updated
			m.paused = paused
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
	if m.paused {
		primary = m.theme.Faint
	}

	var (
		projectStyle = lipgloss.NewStyle().
				Foreground(m.theme.SubText).
				Align(lipgloss.Center)

		descStyle = lipgloss.NewStyle().
				Foreground(m.theme.Text).
				Bold(true).
				Align(lipgloss.Center).
				MarginTop(1)

		helpStyle = lipgloss.NewStyle().
				Foreground(m.theme.SubText).
				Align(lipgloss.Center).
				MarginTop(2)
	)

	h := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	s := int(duration.Seconds()) % 60
	timeStr := fmt.Sprintf("%02d:%02d:%02d", h, minutes, s)

	bigTime := renderBigText(timeStr, string(primary))

	status := ""
	if m.paused {
		status = m.loc.Format("watch.status.paused")
	}

	statusStyle := lipgloss.NewStyle().
		Foreground(m.theme.Secondary).
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
