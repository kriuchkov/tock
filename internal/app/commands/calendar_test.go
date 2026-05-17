package commands

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func TestRunCalendarCmdInvokesProgram(t *testing.T) {
	runner := runCalendarProgram
	t.Cleanup(func() { runCalendarProgram = runner })

	called := false
	runCalendarProgram = func(model calendarModel) error {
		called = true
		assert.NotNil(t, model.service)
		assert.NotNil(t, model.config)
		assert.NotNil(t, model.timeFormat)
		assert.NotNil(t, model.loc)
		return nil
	}

	cmd := newTestCLICommand(&stubActivityResolver{})
	require.NoError(t, runCalendarCmd(cmd))
	assert.True(t, called)
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

	view := model.renderCalendar()
	assert.Contains(t, view, "April 2026")
	assert.Contains(t, view, "Mo")
	assert.Contains(t, view, "Tu")
	assert.Contains(t, view, "Use arrows to navigate")
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
	model.ready = true
	model.viewport = viewport.New(80, 20)
	model.currentDate = date

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
			wantInContent:     []string{"test", "test2", "uncolored"},
		},
		{
			name:          "no tagColors leaves theme empty and content still renders",
			activity:      models.Activity{Project: "Projekt", Tags: []string{"test"}},
			tagColors:     nil,
			wantInContent: []string{"Projekt", "test"},
		},
		{
			name:            "project color applied in breakdown total section",
			activity:        models.Activity{Project: "Projekt"},
			tagColors:       map[string]models.TagColor{"Projekt": {FG: "2"}},
			wantThemeColors: map[string]string{"Projekt": "2"},
			wantInContent:   []string{"Projekt", "Total"},
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

func TestReportModelHandleKeyMsgScrollsViewport(t *testing.T) {
	model := initialCalendarModel(
		&stubActivityResolver{},
		&config.Config{},
		timeutil.NewFormatter("24"),
		localization.MustNew(localization.LanguageEnglish),
		nil,
	)
	model.ready = true
	model.viewport = viewport.New(20, 3)
	model.viewport.SetContent("1\n2\n3\n4\n5\n6")

	_, handled := model.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	require.True(t, handled)
	assert.Positive(t, model.viewport.YOffset)
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
		)
		assert.Equal(t, 12, strings.Count(bar, "█"))
	})

	t.Run("no project breakdown uses solid fallback", func(t *testing.T) {
		bar := model.renderWeekBar(2*time.Hour, 4*time.Hour, nil)
		assert.Equal(t, 12, strings.Count(bar, "█"))
	})

	t.Run("very small duration uses single bar char", func(t *testing.T) {
		bar := model.renderWeekBar(time.Minute, 10*time.Hour, nil)
		assert.Contains(t, bar, barChar)
	})

	t.Run("zero duration returns empty", func(t *testing.T) {
		bar := model.renderWeekBar(0, 10*time.Hour, nil)
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
