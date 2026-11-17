package commands

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// captureShell runs a command with the TUI program stubbed out and returns the shell it opened.
func captureShell(t *testing.T, run func() error) *tuiShell {
	t.Helper()
	runner := runShellProgram
	t.Cleanup(func() { runShellProgram = runner })

	var opened *tuiShell
	runShellProgram = func(shell *tuiShell) error {
		opened = shell
		return nil
	}
	require.NoError(t, run())
	require.NotNil(t, opened)
	return opened
}

// fakeScreen counts what the shell asks of a screen.
type fakeScreen struct {
	name      string
	tabs      tabStrip
	keys      []string
	sizes     []tea.WindowSizeMsg
	inits     int
	refreshes int
	open      bool
}

func (f *fakeScreen) Init() tea.Cmd { f.inits++; return nil }

func (f *fakeScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		f.keys = append(f.keys, msg.String())
	case tea.WindowSizeMsg:
		f.sizes = append(f.sizes, msg)
	}
	return f, nil
}

func (f *fakeScreen) View() string          { return f.name }
func (f *fakeScreen) setTabs(tabs tabStrip) { f.tabs = tabs }
func (f *fakeScreen) modal() bool           { return f.open }
func (f *fakeScreen) refresh() tea.Cmd      { f.refreshes++; return nil }

// allTabs are the names of the tabs with reminders available.
func allTabs() []string {
	loc := localization.MustNew(localization.LanguageEnglish)
	return []string{loc.Text("tui.tab.calendar"), loc.Text("tui.tab.reminders"), loc.Text("tui.tab.list"), loc.Text("tui.tab.analyze")}
}

func fakeShell(start shellTab) (*tuiShell, map[shellTab]*fakeScreen) {
	screens := map[shellTab]*fakeScreen{}
	specs := make([]shellTabSpec, 0, len(allTabs()))
	for i, name := range allTabs() {
		tab := shellTab(i)
		specs = append(specs, shellTabSpec{tab: tab, name: name, build: func() shellScreen {
			screen := &fakeScreen{name: name}
			screens[tab] = screen
			return screen
		}})
	}
	return newTUIShell(specs, start), screens
}

func TestShellSwitchesTabsWithDigits(t *testing.T) {
	shell, screens := fakeShell(tabList)
	require.Len(t, screens, 1, "only the start tab is built")
	assert.Equal(t, tabStrip{names: allTabs(), active: 2}, screens[tabList].tabs)

	shell.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	shell.Update(runeKey('4'))
	assert.Equal(t, allTabs()[tabAnalyze], shell.View())
	analyze := screens[tabAnalyze]
	assert.Equal(t, 1, analyze.inits, "a new screen starts")
	assert.Equal(t, []tea.WindowSizeMsg{{Width: 100, Height: 30}}, analyze.sizes, "and gets the size it missed")
	assert.Equal(t, 3, analyze.tabs.active)

	shell.Update(runeKey('3'))
	assert.Equal(t, allTabs()[tabList], shell.View())
	assert.Equal(t, 1, screens[tabList].refreshes, "a screen shown again reloads")

	shell.Update(runeKey('9'))
	shell.Update(runeKey('j'))
	assert.Equal(t, allTabs()[tabList], shell.View())
	assert.Equal(t, []string{"9", "j"}, screens[tabList].keys, "other keys go to the screen")
}

func TestShellLeavesDigitsToAnOpenForm(t *testing.T) {
	shell, screens := fakeShell(tabCalendar)
	screens[tabCalendar].open = true

	shell.Update(runeKey('2'))
	assert.Equal(t, allTabs()[tabCalendar], shell.View())
	assert.Equal(t, []string{"2"}, screens[tabCalendar].keys)

	_, cmd := shell.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd(), "ctrl+c quits from anywhere")
}

func TestShellSendsTheSizeToEveryBuiltScreen(t *testing.T) {
	shell, screens := fakeShell(tabCalendar)
	shell.Update(runeKey('2'))
	shell.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	assert.Equal(t, []tea.WindowSizeMsg{{Width: 80, Height: 24}}, screens[tabCalendar].sizes)
	assert.Equal(t, []tea.WindowSizeMsg{{Width: 80, Height: 24}}, screens[tabReminders].sizes)
}

func TestShellOpensRemindOnTheRemindersTab(t *testing.T) {
	h := newRemindHarness(t)
	cmd := NewRemindCmd()
	cmd.SetContext(h.rt.WithContext(context.Background()))
	shell := captureShell(t, func() error { return runRemindTUI(cmd) })

	model, ok := shell.current().(*remindModel)
	require.True(t, ok)
	assert.Equal(t, tabStrip{names: allTabs(), active: 1}, model.tabs)

	model.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	view := model.View()
	assert.Contains(t, view, " ≡ tock    calendar    reminders    list    analyze ")
	assert.Contains(t, view, "a add   ←→ day   tab view   1–4 tabs   q quit")
	assert.Equal(t, 20, lipgloss.Height(view))
}

func TestListScreenSwitchesViewsKeepingTheDate(t *testing.T) {
	day := time.Date(2026, time.July, 15, 0, 0, 0, 0, time.Local)
	var windows []models.ActivityFilter
	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) { return nil, nil },
		getReportFn: func(_ context.Context, filter models.ActivityFilter) (*models.Report, error) {
			windows = append(windows, filter)
			return &models.Report{}, nil
		},
	}
	screen := newListScreen(service, &config.Config{}, timeutil.NewFormatter("24"),
		localization.MustNew(localization.LanguageEnglish), periodDaily)
	screen.day.selectedDate = day
	screen.Update(tea.WindowSizeMsg{Width: 120, Height: 20})

	screen.Update(tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, periodWeekly, screen.period)
	require.Len(t, windows, 1)
	assert.Equal(t, time.Date(2026, time.July, 13, 0, 0, 0, 0, time.Local), *windows[0].FromDate, "the week of the day")
	assert.Contains(t, screen.View(), "13 – 19 Jul")
	assert.Equal(t, 20, lipgloss.Height(screen.View()), "the new view got the size")

	screen.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, periodDaily, screen.period)
	assert.True(t, sameDay(day, screen.day.selectedDate))

	screen.day.pendingDelete = &models.Activity{Project: "core"}
	screen.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, periodDaily, screen.period, "the delete prompt keeps tab")
}

func TestAnalyzeTabShowsTheAnalysis(t *testing.T) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.Local).AddDate(0, 0, -1)
	end := start.Add(90 * time.Minute)
	var days []int
	service := &stubActivityResolver{
		getReportFn: func(_ context.Context, filter models.ActivityFilter) (*models.Report, error) {
			days = append(days, int(filter.ToDate.Sub(*filter.FromDate).Hours()/24+0.5))
			activities := []models.Activity{{Project: "core", StartTime: start, EndTime: &end}}
			return &models.Report{
				Activities:    activities,
				TotalDuration: end.Sub(start),
				ByProject:     map[string]models.ProjectReport{"core": {ProjectName: "core", Duration: end.Sub(start)}},
			}, nil
		},
	}
	model := newAnalyzeModel(service, timeutil.NewFormatter("24"), localization.MustNew(localization.LanguageEnglish), DarkTheme())
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model.Update(model.Init()())

	view := model.View()
	assert.Equal(t, []int{30}, days, "30 days, as tock analyze")
	for _, want := range []string{
		"last 30 days tab", "1h30m · 1 entries", "FOCUS QUALITY", "Deep Work Score", "SESSION DISTRIBUTION",
		"PROJECTS · 1h30m", "core", "BY WEEKDAY",
	} {
		assert.Contains(t, view, want)
	}
	for line := range strings.SplitSeq(view, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), 120)
	}

	cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.Nil(t, model.data, "the old window is not shown under the new title")
	model.Update(cmd())
	assert.Equal(t, []int{30, 90}, days)
	assert.Contains(t, model.View(), "last 90 days")
}

func TestListScreenReadsTheActivitiesOnce(t *testing.T) {
	day := time.Date(2026, time.July, 15, 9, 0, 0, 0, time.Local)
	reads := 0
	activities := []models.Activity{
		{Project: "core", StartTime: day},
		{Project: "ops", StartTime: day.AddDate(0, 0, 2)},
	}
	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			reads++
			return activities, nil
		},
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) { return &models.Report{}, nil },
	}
	screen := newListScreen(service, &config.Config{}, timeutil.NewFormatter("24"),
		localization.MustNew(localization.LanguageEnglish), periodDaily)
	screen.day.showDay(day)
	require.Equal(t, 1, reads)

	screen.Update(tea.KeyMsg{Type: tea.KeyRight})
	assert.True(t, sameDay(day.AddDate(0, 0, 2), screen.day.selectedDate), "the next day with activities")
	screen.Update(tea.KeyMsg{Type: tea.KeyTab})
	screen.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, 1, reads, "moving between days and views reads nothing again")

	// Switching to the tab reads them again in the background; the day view takes them
	// even while a period view is shown.
	activities = append(activities, models.Activity{Project: "new", StartTime: day.AddDate(0, 0, 2).Add(time.Hour)})
	screen.Update(tea.KeyMsg{Type: tea.KeyTab})
	cmd := screen.day.refresh()
	require.NotNil(t, cmd)
	screen.Update(cmd())
	assert.Equal(t, 2, reads)
	screen.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Len(t, screen.day.list.activities, 2)
}
