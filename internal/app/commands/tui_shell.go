package commands

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/go-faster/errors"
	"github.com/spf13/cobra"
)

// shellTab is a tab of the tock TUI.
type shellTab int

const (
	tabCalendar shellTab = iota
	tabReminders
	tabList
	tabAnalyze
)

// shellScreen is the screen on a tab. It draws the whole terminal, the header with the
// tabs the shell gives it included.
type shellScreen interface {
	tea.Model
	setTabs(tabs tabStrip)
	// modal reports whether a form or a prompt is open: it then gets every key, digits too.
	modal() bool
	// refresh reloads what the other tabs may have changed when the screen's tab is shown again.
	refresh() tea.Cmd
}

// shellTabSpec is a tab of the shell: its name and how to build its screen.
type shellTabSpec struct {
	tab   shellTab
	name  string
	build func() shellScreen
}

// tuiShell is the tock TUI: the screens on their tabs, switched with 1–4. A screen is built
// when its tab is first shown and keeps its state when another tab is.
type tuiShell struct {
	specs   []shellTabSpec
	screens []shellScreen
	active  int
	size    *tea.WindowSizeMsg
}

var runShellProgram = func(shell *tuiShell) error {
	program := tea.NewProgram(shell, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return errors.Wrap(err, "run program")
	}
	return nil
}

// runTUI opens the tock TUI on the start tab; the list tab opens on period.
func runTUI(cmd *cobra.Command, start shellTab, period listPeriod) error {
	return runShellProgram(newAppShell(cmd, start, period))
}

// newAppShell builds the tabs of the tock TUI from the runtime. The reminders tab is left out
// when reminders are unavailable.
func newAppShell(cmd *cobra.Command, start shellTab, period listPeriod) *tuiShell {
	rt := getRuntime(cmd)
	loc := getLocalizer(cmd)
	theme := GetTheme(rt.Config.Theme)
	snooze := defaultSnooze(cmd)

	specs := []shellTabSpec{{
		tab:  tabCalendar,
		name: loc.Text("tui.tab.calendar"),
		build: func() shellScreen {
			model := initialCalendarModel(rt.ActivityService, rt.Config, rt.TimeFormatter, loc, rt.TagColors)
			if rt.ReminderService != nil {
				model.withReminders(rt.ReminderService, snooze)
			}
			return &model
		},
	}}
	if rt.ReminderService != nil {
		specs = append(specs, shellTabSpec{
			tab:  tabReminders,
			name: loc.Text("tui.tab.reminders"),
			build: func() shellScreen {
				model := newRemindModel(cmd.Context(), rt.ReminderService, rt.ActivityService,
					newReminderFormEnv(loc, rt.TimeFormatter, theme), snooze)
				model.reload()
				return model
			},
		})
	}
	specs = append(specs,
		shellTabSpec{
			tab:   tabList,
			name:  loc.Text("tui.tab.list"),
			build: func() shellScreen { return newListScreen(rt.ActivityService, rt.Config, rt.TimeFormatter, loc, period) },
		},
		shellTabSpec{
			tab:   tabAnalyze,
			name:  loc.Text("tui.tab.analyze"),
			build: func() shellScreen { return newAnalyzeModel(rt.ActivityService, rt.TimeFormatter, loc, theme) },
		},
	)
	return newTUIShell(specs, start)
}

// newTUIShell builds the screen of the start tab, or of the first one when start is not there.
func newTUIShell(specs []shellTabSpec, start shellTab) *tuiShell {
	s := &tuiShell{specs: specs, screens: make([]shellScreen, len(specs))}
	for i, spec := range specs {
		if spec.tab == start {
			s.active = i
		}
	}
	s.screen(s.active)
	return s
}

// screen is the screen at a position of the tabs, built when first asked for.
func (s *tuiShell) screen(i int) (shellScreen, bool) {
	if s.screens[i] != nil {
		return s.screens[i], false
	}
	names := make([]string, len(s.specs))
	for j, spec := range s.specs {
		names[j] = spec.name
	}
	screen := s.specs[i].build()
	screen.setTabs(tabStrip{names: names, active: i})
	s.screens[i] = screen
	return screen, true
}

func (s *tuiShell) current() shellScreen {
	return s.screens[s.active]
}

func (s *tuiShell) Init() tea.Cmd {
	return s.current().Init()
}

func (s *tuiShell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return s, tea.Quit
		}
		if i, ok := s.tabKey(msg); ok {
			return s, s.open(i)
		}
		_, cmd := s.current().Update(msg)
		return s, cmd
	case tea.WindowSizeMsg:
		s.size = &msg
	}

	// The size and the answers to the screens' commands go to every screen built so far;
	// a screen leaves the messages of the others alone.
	var cmds []tea.Cmd
	for _, screen := range s.screens {
		if screen != nil {
			_, cmd := screen.Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	return s, tea.Batch(cmds...)
}

// tabKey is the tab a digit key switches to; a screen with a form or a prompt open keeps its keys.
func (s *tuiShell) tabKey(msg tea.KeyMsg) (int, bool) {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 || s.current().modal() {
		return 0, false
	}
	i := int(msg.Runes[0] - '1')
	return i, i >= 0 && i < len(s.specs)
}

// open shows the tab at position i: a screen shown before reloads, a new one starts.
func (s *tuiShell) open(i int) tea.Cmd {
	if i == s.active {
		return nil
	}
	s.active = i
	screen, built := s.screen(i)
	if !built {
		return screen.refresh()
	}
	var sized tea.Cmd
	if s.size != nil {
		_, sized = screen.Update(*s.size)
	}
	return tea.Batch(screen.Init(), sized)
}

func (s *tuiShell) View() string {
	return s.current().View()
}
