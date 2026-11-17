package commands

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func TestRunCalendarCmdOpensTheCalendarTab(t *testing.T) {
	cmd := newTestCLICommand(&stubActivityResolver{})
	shell := captureShell(t, func() error { return runCalendarCmd(cmd) })

	model, ok := shell.current().(*calendarModel)
	require.True(t, ok)
	assert.NotNil(t, model.service)
	assert.NotNil(t, model.config)
	assert.NotNil(t, model.timeFormat)
	assert.NotNil(t, model.loc)
	tabs := allTabs()
	assert.Equal(t, []string{tabs[tabCalendar], tabs[tabList], tabs[tabAnalyze]}, model.tabs.names, "no reminders tab without reminders")
}

func TestReportModelUpdateViewportContentLocalizedEmptyState(t *testing.T) {
	loc := localization.MustNew(localization.LanguageEnglish)
	model := initialCalendarModel(&stubActivityResolver{}, &config.Config{}, timeutil.NewFormatter("24"), loc, nil)
	model.width = 100
	model.height = 30
	model.ready = true
	model.viewport = viewport.New(40, 20)
	model.currentDate = time.Date(2026, time.April, 4, 0, 0, 0, 0, time.Local)
	model.updateViewportContent()

	content := model.viewport.View()
	assert.Contains(t, content, "Saturday, 04 April 2026")
	assert.Contains(t, content, "No events")
}

func TestReportModelFetchMonthDataBuildsReportWindow(t *testing.T) {
	var gotFilter models.ActivityFilter
	service := &stubActivityResolver{
		getReportFn: func(_ context.Context, filter models.ActivityFilter) (*models.Report, error) {
			gotFilter = filter
			return &models.Report{}, nil
		},
	}
	model := initialCalendarModel(
		service,
		&config.Config{},
		timeutil.NewFormatter("24"),
		localization.MustNew(localization.LanguageEnglish),
		nil,
	)
	model.viewDate = time.Date(2026, time.April, 4, 0, 0, 0, 0, time.Local)

	msg := model.fetchMonthData()
	monthData, ok := msg.(monthDataMsg)
	require.True(t, ok)
	require.NotNil(t, gotFilter.FromDate)
	require.NotNil(t, gotFilter.ToDate)
	assert.Equal(t, time.Date(2026, time.March, 18, 0, 0, 0, 0, time.Local), *gotFilter.FromDate)
	assert.Equal(t, time.Date(2026, time.May, 15, 0, 0, 0, 0, time.Local), *gotFilter.ToDate)
	assert.Empty(t, monthData.monthReports)
	assert.Empty(t, monthData.dailyReports)
}

func TestReportModelRenderCalendarLocalizedLabels(t *testing.T) {
	loc := localization.MustNew(localization.LanguageEnglish)
	model := initialCalendarModel(&stubActivityResolver{}, &config.Config{}, timeutil.NewFormatter("24"), loc, nil)
	model.currentDate = time.Date(2026, time.April, 4, 0, 0, 0, 0, time.Local)
	model.viewDate = model.currentDate
	model.monthReports[4] = &models.Report{TotalDuration: time.Hour}
	model.Update(tea.WindowSizeMsg{Width: 160, Height: 40})

	view := model.View()
	assert.Contains(t, view, "APRIL 2026")
	assert.Contains(t, view, " Mo  Tu  We  Th  Fr  Sa  Su")
	assert.Contains(t, view, "4 tracked", "the legend samples a tracked day of the month")
	assert.Contains(t, view, "n/p month")
}

func TestReportModelHandleKeyMsgChangesMonth(t *testing.T) {
	model := initialCalendarModel(
		&stubActivityResolver{},
		&config.Config{},
		timeutil.NewFormatter("24"),
		localization.MustNew(localization.LanguageEnglish),
		nil,
	)
	model.currentDate = time.Date(2026, time.March, 31, 0, 0, 0, 0, time.Local)
	model.viewDate = model.currentDate

	cmd, handled := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRight})
	require.True(t, handled)
	require.NotNil(t, cmd)
	assert.Equal(t, time.Date(2026, time.April, 1, 0, 0, 0, 0, time.Local), model.currentDate)
	assert.Equal(t, model.currentDate, model.viewDate)
}

// makeModelWithActivity returns a ready calendarModel with a single activity
// on 2026-05-10 and the given tagColors wired in.
func makeModelWithActivity(t *testing.T, act models.Activity, tagColors map[string]models.TagColor) calendarModel {
	t.Helper()
	date := time.Date(2026, time.May, 10, 0, 0, 0, 0, time.Local)
	end := date.Add(time.Hour)
	act.StartTime = date
	act.EndTime = &end

	loc := localization.MustNew(localization.LanguageEnglish)
	model := initialCalendarModel(&stubActivityResolver{}, &config.Config{}, timeutil.NewFormatter("24"), loc, tagColors)
	model.currentDate, model.viewDate = date, date
	model.Update(tea.WindowSizeMsg{Width: 130, Height: 40}) // layout B: notes and tags in the rows

	key := date.Format("2006-01-02")
	model.dailyReports = map[string]*models.Report{
		key: {
			Activities:    []models.Activity{act},
			TotalDuration: time.Hour,
			ByProject:     map[string]models.ProjectReport{act.Project: {Duration: time.Hour}},
		},
	}
	model.updateViewportContent()
	return model
}

func TestTagColors(t *testing.T) {
	tests := []struct {
		name              string
		activity          models.Activity
		tagColors         map[string]models.TagColor
		wantThemeColors   map[string]string // tag → expected FG lipgloss.Color string
		wantAbsentInTheme []string          // tags that must NOT be in TagColors
		wantInContent     []string          // substrings that must appear in viewport
	}{
		{
			name:            "tagColors populated in theme",
			activity:        models.Activity{Project: "Projekt"},
			tagColors:       map[string]models.TagColor{"Projekt": {FG: "2"}, "test": {FG: "5"}},
			wantThemeColors: map[string]string{"Projekt": "2", "test": "5"},
		},
		{
			name:          "project and tags rendered without tagColors",
			activity:      models.Activity{Project: "Projekt", Tags: []string{"test"}},
			tagColors:     nil,
			wantInContent: []string{"Projekt", "test"},
		},
		{
			name:            "project color taken from tagColors",
			activity:        models.Activity{Project: "Projekt"},
			tagColors:       map[string]models.TagColor{"Projekt": {FG: "2"}},
			wantThemeColors: map[string]string{"Projekt": "2"},
			wantInContent:   []string{"Projekt"},
		},
		{
			name:              "tag colors taken from tagColors; uncolored tag absent from theme",
			activity:          models.Activity{Project: "Projekt", Tags: []string{"test", "test2", "uncolored"}},
			tagColors:         map[string]models.TagColor{"test": {FG: "5"}, "test2": {FG: "3"}},
			wantThemeColors:   map[string]string{"test": "5", "test2": "3"},
			wantAbsentInTheme: []string{"uncolored"},
			wantInContent:     []string{"#test", "#test2"},
		},
		{
			name:          "no tagColors leaves theme empty and content still renders",
			activity:      models.Activity{Project: "Projekt", Tags: []string{"test"}},
			tagColors:     nil,
			wantInContent: []string{"Projekt", "test"},
		},
		{
			name:            "project color applied in the by-project section",
			activity:        models.Activity{Project: "Projekt"},
			tagColors:       map[string]models.TagColor{"Projekt": {FG: "2"}},
			wantThemeColors: map[string]string{"Projekt": "2"},
			wantInContent:   []string{"Projekt", "BY PROJECT", "TRACKED · 1 · 1h"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := makeModelWithActivity(t, tt.activity, tt.tagColors)

			if tt.tagColors == nil {
				assert.Empty(t, model.theme.TagColors)
			}
			for tag, want := range tt.wantThemeColors {
				assert.Equal(t, want, string(model.theme.TagColors[tag].FG), "TagColors[%q].FG", tag)
			}
			for _, tag := range tt.wantAbsentInTheme {
				_, present := model.theme.TagColors[tag]
				assert.False(t, present, "TagColors must not contain %q", tag)
			}

			content := model.viewport.View()
			for _, sub := range tt.wantInContent {
				assert.Contains(t, content, sub)
			}
		})
	}
}

func TestCalendarSelectsRowsAndScrollsToThem(t *testing.T) {
	var acts []models.Activity
	for i := range 8 {
		start := time.Date(2026, time.May, 10, 8+i, 0, 0, 0, time.Local)
		end := start.Add(30 * time.Minute)
		acts = append(acts, models.Activity{Project: "P", Description: "task", StartTime: start, EndTime: &end})
	}
	model := makeModelWithActivity(t, acts[0], nil)
	model.dailyReports[insights.DateKey(acts[0].StartTime)].Activities = acts
	model.Update(tea.WindowSizeMsg{Width: 130, Height: 12})

	press := func(r rune) { model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}) }
	press('j')
	assert.Equal(t, daySelection{section: sectionTracked}, model.sel, "j selects the first row")
	for range 7 {
		press('j')
	}
	assert.Equal(t, daySelection{section: sectionTracked, index: 7}, model.sel)
	assert.Positive(t, model.viewport.YOffset, "the selected row is scrolled into view")
	assert.Contains(t, model.viewport.View(), "15:00–15:30")

	model.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 6, model.sel.index, "the arrows move a selection")
	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, daySelection{}, model.sel)

	cmd, handled := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyUp})
	require.True(t, handled)
	assert.Nil(t, cmd)
	assert.Equal(t, 3, model.currentDate.Day(), "without a selection the arrows move by a week")
}

func TestCalendarLayoutBreakpoints(t *testing.T) {
	tests := []struct {
		width, height                 int
		monthW, sideW                 int
		upcoming, inline, focus, more bool
	}{
		{width: 160, height: 36, monthW: calendarMonthColW, sideW: calendarSideColW, focus: true, more: true},
		{width: 125, height: 36, monthW: calendarMonthColW, upcoming: true, inline: true, more: true},
		{width: 105, height: 36, monthW: calendarMonthColW, upcoming: true, focus: true, more: true},
		{width: 85, height: 26, monthW: calendarMonthColW, focus: true},
		{width: 60, height: 20},
	}
	for _, tt := range tests {
		l := newCalendarLayout(tt.width, tt.height)
		name := fmt.Sprintf("%dx%d", tt.width, tt.height)
		assert.Equal(t, tt.monthW, l.monthW, name)
		assert.Equal(t, tt.sideW, l.sideW, name)
		assert.Equal(t, tt.upcoming, l.upcomingLeft, name)
		assert.Equal(t, tt.inline, l.inline, name)
		assert.Equal(t, tt.focus, l.focus, name)
		assert.Equal(t, tt.more, l.extras, name)

		used := l.dayW
		if l.monthW > 0 {
			used += l.monthW + 3
		}
		if l.sideW > 0 {
			used += l.sideW + 3
		}
		assert.Equal(t, tt.width, used, name)
	}
}

func TestCalendarViewFitsTheTerminal(t *testing.T) {
	act := models.Activity{
		Project:     "A project with a long name",
		Description: strings.Repeat("long description ", 10),
		Notes:       strings.Repeat("notes ", 30),
		Tags:        []string{"backend", "api", "review"},
	}
	for _, size := range [][2]int{{200, 50}, {160, 36}, {125, 34}, {105, 30}, {85, 26}, {60, 30}, {60, 20}} {
		model := makeModelWithActivity(t, act, nil)
		model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})

		lines := strings.Split(model.View(), "\n")
		assert.Len(t, lines, size[1], "%dx%d", size[0], size[1])
		for i, line := range lines {
			assert.LessOrEqual(t, lipgloss.Width(line), size[0], "%dx%d line %d", size[0], size[1], i)
		}
	}
}

func TestCalendarMonthOpensOverTheDayOnNarrowTerminals(t *testing.T) {
	model := makeModelWithActivity(t, models.Activity{Project: "P"}, nil)
	model.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	assert.NotContains(t, model.View(), "MAY 2026")

	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	require.True(t, model.monthOpen)
	assert.Contains(t, model.View(), "MAY 2026")

	model.Update(tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, 11, model.currentDate.Day(), "days move with the month open")
	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, model.monthOpen)
}

func TestRenderWeekBar(t *testing.T) {
	model := initialCalendarModel(
		&stubActivityResolver{},
		&config.Config{},
		timeutil.NewFormatter("24"),
		localization.MustNew(localization.LanguageEnglish),
		map[string]models.TagColor{"Alpha": {FG: "2"}, "Beta": {FG: "4"}},
	)

	t.Run("segmented width matches scaled day width", func(t *testing.T) {
		bar := model.renderWeekBar(
			2*time.Hour,
			4*time.Hour,
			[]insights.ProjectDuration{
				{Name: "Alpha", Duration: 90 * time.Minute},
				{Name: "Beta", Duration: 30 * time.Minute},
			},
			25,
		)
		assert.Equal(t, 12, strings.Count(bar, glyphBar))
	})

	t.Run("no project breakdown uses solid fallback", func(t *testing.T) {
		bar := model.renderWeekBar(2*time.Hour, 4*time.Hour, nil, 25)
		assert.Equal(t, 12, strings.Count(bar, glyphBar))
	})

	t.Run("very small duration uses single bar char", func(t *testing.T) {
		bar := model.renderWeekBar(time.Minute, 10*time.Hour, nil, 25)
		assert.Contains(t, bar, barChar)
	})

	t.Run("zero duration returns empty", func(t *testing.T) {
		bar := model.renderWeekBar(0, 10*time.Hour, nil, 25)
		assert.Empty(t, bar)
	})
}

func TestEffectiveTagColor_ScopeSelectiveToggle(t *testing.T) {
	cfg := &config.Config{
		Theme: config.ThemeConfig{
			TagColors: map[string]string{"Work": "2"},
		},
		Timewarrior: config.TimewarriorConfig{
			UseTockTagColorsWeeklyActivity: true,
		},
	}

	model := initialCalendarModel(
		&stubActivityResolver{},
		cfg,
		timeutil.NewFormatter("24"),
		localization.MustNew(localization.LanguageEnglish),
		map[string]models.TagColor{"Work": {FG: "196", BG: "0"}},
	)

	weekly, ok := model.effectiveTagColor("Work", tagColorScopeWeekly)
	require.True(t, ok)
	assert.Equal(t, "2", string(weekly.FG))
	assert.Empty(t, string(weekly.BG))

	calendar, ok := model.effectiveTagColor("Work", tagColorScopeCalendar)
	require.True(t, ok)
	assert.Equal(t, "196", string(calendar.FG))
	assert.Equal(t, "0", string(calendar.BG))
}

func TestEffectiveTagColor_GlobalToggleAppliesToAllScopes(t *testing.T) {
	cfg := &config.Config{
		Theme: config.ThemeConfig{
			TagColors: map[string]string{"Work": "3"},
		},
		Timewarrior: config.TimewarriorConfig{
			UseTockTagColors: true,
		},
	}

	model := initialCalendarModel(
		&stubActivityResolver{},
		cfg,
		timeutil.NewFormatter("24"),
		localization.MustNew(localization.LanguageEnglish),
		map[string]models.TagColor{"Work": {FG: "196", BG: "0"}},
	)

	for _, scope := range []tagColorScope{tagColorScopeCalendar, tagColorScopeWeekly, tagColorScopeTopProject} {
		ts, ok := model.effectiveTagColor("Work", scope)
		require.True(t, ok)
		assert.Equal(t, "3", string(ts.FG))
		assert.Empty(t, string(ts.BG))
	}
}

func TestCalendarByProjectUsesTheSpentFormat(t *testing.T) {
	model := makeModelWithActivity(t, models.Activity{Project: "Projekt"}, nil)
	model.config.Calendar.TimeSpentFormat = "decimal"
	model.updateViewportContent()
	view := model.viewport.View()
	_, byProject, found := strings.Cut(view, "BY PROJECT")
	require.True(t, found, view)
	assert.Regexp(t, `Projekt\s+\S+\s+1\.00`, byProject, "calendar.time_spent_format, as on master")
	assert.NotContains(t, byProject, "1h")
}
