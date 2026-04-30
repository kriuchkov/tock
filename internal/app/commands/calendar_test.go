package commands

import (
	"context"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
