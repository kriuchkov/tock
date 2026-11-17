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
	"github.com/kriuchkov/tock/internal/app/watching"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"
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

	model := initialWatchModel(*activity, service, GetTheme(cfg.Theme), getLocalizer(cmd))
	model.timeFormat, model.stopOnExit = rt.TimeFormatter, opts.StopOnExit
	if err = runWatchProgram(model); err != nil {
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
	activity   models.Activity
	err        error
	now        time.Time
	service    ports.ActivityResolver
	width      int
	height     int
	theme      Theme
	ui         chromeStyles
	loc        *localization.Localizer
	timeFormat *timeutil.Formatter
	paused     bool
	// stopOnExit is shown in the footer: whether quitting stops the activity.
	stopOnExit bool
}

func initialWatchModel(activity models.Activity, service ports.ActivityResolver, theme Theme, loc *localization.Localizer) watchModel {
	return watchModel{
		activity: activity,
		service:  service,
		theme:    theme,
		ui:       newChromeStyles(theme),
		loc:      loc,
		now:      time.Now(),
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
		switch msg.String() {
		case "q", keyCtrlC:
			return m, tea.Quit
		case " ":
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
	header := screenHeader{tabs: []string{m.loc.Text("tui.tab.watch")}, title: m.activity.Project, summary: m.summary()}
	top := m.ui.renderHeader(header, m.width) + "\n"

	keys := []footerKey{{key: keySpace, action: m.loc.Text("tui.key.pause")}, {key: "q", action: m.loc.Text("tui.key.quit")}}
	onExit := "watch.stop_on_exit.off"
	if m.stopOnExit {
		onExit = "watch.stop_on_exit.on"
	}
	bottom := "\n" + m.ui.renderFooter(keys, m.ui.muted.Render(m.loc.Text(onExit)), m.width)

	content := m.content()
	if bodyH := m.height - lipgloss.Height(top) - lipgloss.Height(bottom); m.width > 0 && bodyH > 0 {
		content = lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center, content)
	}
	return renderScreen(top, content, bottom, m.width, m.height)
}

// summary is the state on the ruler: "● running · since 13:30", or paused in the warning color.
func (m watchModel) summary() []rulerItem {
	state := m.ui.ok.Render(glyphRunning + " " + m.loc.Text("watch.running"))
	if m.paused {
		state = lipgloss.NewStyle().Foreground(m.theme.Warn).Render(glyphPaused + " " + m.loc.Text("watch.paused"))
	}
	format := "15:04"
	if m.timeFormat != nil {
		format = m.timeFormat.GetDisplayFormat()
	}
	since := m.ui.muted.Render(" · " + m.loc.Format("watch.since", m.activity.StartTime.Format(format)))
	return []rulerItem{{long: state + since, short: state}}
}

// content is the stopwatch in big digits, muted while paused, the description and the project with its tags.
func (m watchModel) content() string {
	duration := m.now.Sub(m.activity.StartTime)
	if m.paused {
		duration = 0
		if m.activity.EndTime != nil {
			duration = m.activity.EndTime.Sub(m.activity.StartTime)
		}
	}
	duration = duration.Round(time.Second)
	clock := fmt.Sprintf("%02d:%02d:%02d", int(duration.Hours()), int(duration.Minutes())%60, int(duration.Seconds())%60)

	digits := m.ui.bold
	if m.paused {
		digits = m.ui.mutedBold
	}
	meta := m.ui.muted.Render(m.activity.Project)
	if len(m.activity.Tags) > 0 {
		meta += m.ui.muted.Render(" · ") + m.ui.tag.Render("#"+strings.Join(m.activity.Tags, " #"))
	}
	return lipgloss.JoinVertical(lipgloss.Center,
		renderBigText(clock, digits), "", m.ui.bold.Render(m.activity.Description), meta)
}

// Font pixel constants for the big-digit renderer.
const (
	fontFull  = "███"
	fontSides = "█ █"
	fontRight = "  █"
	fontLeft  = "█  "
	fontEmpty = "   "
	fontColon = " ▪ "
)

var font = map[rune][]string{
	'0': {fontFull, fontSides, fontSides, fontSides, fontFull},
	'1': {fontRight, fontRight, fontRight, fontRight, fontRight},
	'2': {fontFull, fontRight, fontFull, fontLeft, fontFull},
	'3': {fontFull, fontRight, fontFull, fontRight, fontFull},
	'4': {fontSides, fontSides, fontFull, fontRight, fontRight},
	'5': {fontFull, fontLeft, fontFull, fontRight, fontFull},
	'6': {fontFull, fontLeft, fontFull, fontSides, fontFull},
	'7': {fontFull, fontRight, fontRight, fontRight, fontRight},
	'8': {fontFull, fontSides, fontFull, fontSides, fontFull},
	'9': {fontFull, fontSides, fontFull, fontRight, fontFull},
	':': {fontEmpty, fontColon, fontEmpty, fontColon, fontEmpty},
}

//nolint:goconst //it's more readable to have the full string literals
func renderBigText(text string, style lipgloss.Style) string {
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

	return style.Render(strings.Join(lines[:], "\n"))
}
